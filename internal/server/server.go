// Package server wires the HTTP API, the sync with each person's notes
// folder, auth, Nextcloud Notes compatibility and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/config"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger
	webFS fs.FS
	http  *http.Client // integrations, bookmarks

	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64

	libs   map[int64]*library
	libsMu sync.Mutex
	// filesToks keeps each person's Pocket Files link token (uid →
	// *linkTokens), shared by their library and a storage test.
	filesToks sync.Map

	core    *core.Client
	coreMu  sync.Mutex
	coreOK  bool
	coreErr string

	oidc   *oidcProvider // keyed on the issuer; rebuilt by provider() when it changes
	oidcMu sync.Mutex
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	ua := "PocketNotes/" + Version + " (+https://github.com/abjelosevic88/pocket-homelab-notes)"
	s := &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), libs: map[int64]*library{}}
	s.http = &http.Client{Timeout: 30 * time.Second}
	s.core = core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: ua, ServiceCache: time.Minute, ServiceStale: 15 * time.Minute})
	s.oidc = &oidcProvider{issuerURL: cfg.OIDCIssuer}
	return s
}

// isOwner reports whether uid is the account the box was set up with. Keys
// given to the whole box through the environment are the owner's.
func (s *Server) isOwner(ctx context.Context, uid int64) bool {
	return uid > 0 && uid == s.store.OwnerID(ctx)
}

// Bootstrap seeds the first admin (if configured), demo data and starts the workers.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.cfg.DataDir, "notes"), 0o755); err != nil {
		return fmt.Errorf("create notes dir: %w", err)
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		s.seedUser(ctx, u.ID)
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	go s.syncLoop(ctx)
	go s.housekeeping(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// housekeeping empties old trash and trims version history once an hour.
func (s *Server) housekeeping(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		keep := 0
		for _, uid := range s.store.UserIDs(ctx) {
			st, err := s.store.GetSettings(ctx, uid)
			if err != nil {
				continue
			}
			_ = s.store.PurgeTrash(ctx, uid, st.TrashDays)
			if st.VersionsKeep > keep {
				keep = st.VersionsKeep
			}
		}
		if keep > 0 {
			_ = s.store.PruneVersions(ctx, keep)
		}
		_ = s.store.PurgeSessions(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5, "application/json", "text/html", "text/css", "application/javascript", "text/plain", "text/markdown"))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	// Read-only shared notes (no account needed; the token is the key).
	r.Route("/api/public/share/{token}", func(r chi.Router) {
		r.Get("/", s.handlePublicShare)
		r.Get("/attachment", s.handlePublicShareAttachment)
	})

	// Nextcloud Notes' own API, so apps written for it (the Pocket Homelab app,
	// Nextcloud Notes for Android and iOS) can use this server as it is.
	s.nextcloudRoutes(r)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(180 * time.Second))
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Post("/client-error", s.handleClientError)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/changes", s.handleChanges)

			r.Get("/notes", s.handleListNotes)
			r.Post("/notes", s.handleCreateNote)
			r.Get("/notes/{id}", s.handleGetNote)
			r.Put("/notes/{id}", s.handleUpdateNote)
			r.Delete("/notes/{id}", s.handleDeleteNote)
			r.Post("/notes/{id}/append", s.handleAppendNote)
			r.Post("/notes/{id}/tasks", s.handleToggleTask)
			r.Post("/notes/{id}/duplicate", s.handleDuplicateNote)
			r.Get("/notes/{id}/backlinks", s.handleBacklinks)
			r.Get("/notes/{id}/versions", s.handleListVersions)
			r.Get("/notes/{id}/versions/{vid}", s.handleGetVersion)
			r.Post("/notes/{id}/versions/{vid}/restore", s.handleRestoreVersion)
			r.Get("/notes/{id}/attachment", s.handleGetAttachment)
			r.Post("/notes/{id}/attachments", s.handleUploadAttachment)
			r.Get("/notes/{id}/shares", s.handleNoteShares)
			r.Post("/notes/{id}/shares", s.handleCreateShare)
			r.Get("/notes/{id}/download", s.handleDownloadNote)

			r.Get("/resolve", s.handleResolve)
			r.Get("/mentions", s.handleMentions)
			r.Get("/search", s.handleSearch)
			r.Get("/suggest", s.handleSuggest)
			r.Get("/folders", s.handleFolders)
			r.Post("/folders", s.handleCreateFolder)
			r.Put("/folders", s.handleRenameFolder)
			r.Delete("/folders", s.handleDeleteFolder)
			r.Get("/tags", s.handleTags)
			r.Get("/tasks", s.handleTasks)
			r.Get("/daily", s.handleGetDaily)
			r.Post("/daily", s.handleCreateDaily)
			r.Get("/calendar", s.handleCalendar)
			r.Post("/capture", s.handleCapture)
			r.Get("/templates", s.handleTemplates)
			r.Get("/templates/{id}", s.handleTemplate)
			r.Get("/today", s.handleToday)
			r.Get("/today/agenda", s.handleTodayAgenda)
			r.Get("/today/habits", s.handleTodayHabits)
			r.Put("/today/habits/{id}", s.handleTodayHabit)

			r.Get("/trash", s.handleListTrash)
			r.Get("/trash/{id}", s.handleGetTrash)
			r.Post("/trash/{id}/restore", s.handleRestoreTrash)
			r.Delete("/trash/{id}", s.handleDeleteTrash)
			r.Delete("/trash", s.handleEmptyTrash)

			r.Get("/shares", s.handleListShares)
			r.Delete("/shares/{id}", s.handleDeleteShare)

			r.Get("/storage", s.handleGetStorage)
			r.Put("/storage", s.handlePutStorage)
			r.Post("/storage/test", s.handleTestStorage)
			r.Post("/sync", s.handleSyncNow)

			s.toolRoutes(r)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)
			r.Post("/settings/notify/test", s.handleTestNotify)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.zip", s.handleExportZip)
			r.Post("/import", s.handleImport)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	var ce *conflictError
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "already there")
		return
	case errors.As(err, &ce):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "This note was changed somewhere else meanwhile.", "conflict": true, "note": ce.Note})
		return
	case errors.Is(err, storage.ErrAuth):
		writeErr(w, http.StatusBadGateway, "the notes storage refused the sign-in: check the storage settings")
		return
	case errors.Is(err, storage.ErrLocked):
		writeErr(w, http.StatusLocked, "the note's file is locked by another app; try again in a moment")
		return
	case errors.Is(err, storage.ErrInvalidPath):
		writeErr(w, http.StatusBadRequest, "that path is not allowed")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20))
	return dec.Decode(v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func me(r *http.Request) int64 { return actorID(r.Context()) }

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// ---------- Pocket Core ----------

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "notes", Name: "Pocket Notes",
		Description: "Markdown notes kept as plain files where you choose (this server, a folder, WebDAV, your Nextcloud or your Pocket Files), with full-text search, tags, links between notes, tasks, daily notes, version history, trash, shared links, a rich editor with homelab tools, and Nextcloud Notes' own API for its apps.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "NOTES",
		Publishes: []string{"notes.created", "notes.updated", "notes.deleted"},
		Offers:    linkOffers,
		Uses:      linkUses,
		Services:  serviceUses,
	}
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.cfg.BaseURL + "/api/v1/auth/oidc/callback"}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// publish persists an event locally; the SDK retries network delivery. The payload
// names whose note it was (user_id, user_email).

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// handleClientError writes a crash of the web app into the server's log, so
// a blank page on someone's browser leaves a trace here.
func (s *Server) handleClientError(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message, Stack, Component, URL, Where, Agent string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	who := ""
	if u, err := s.resolveUser(r); err == nil && u != nil {
		who = u.Email
	}
	s.log.Warn("web app crashed", "user", who, "where", in.Where, "url", in.URL, "message", in.Message, "stack", in.Stack, "component", in.Component, "agent", in.Agent)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_notes_info Build info.\n# TYPE pocket_notes_info gauge\npocket_notes_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_notes_uptime_seconds Process uptime.\n# TYPE pocket_notes_uptime_seconds gauge\npocket_notes_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_notes_http_requests_total HTTP requests served.\n# TYPE pocket_notes_http_requests_total counter\npocket_notes_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_notes_http_errors_total HTTP 5xx responses.\n# TYPE pocket_notes_http_errors_total counter\npocket_notes_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_notes_rows Rows per table.\n# TYPE pocket_notes_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_notes_rows{table=%q} %d\n", k, v)
	}
	failing := 0
	s.libsMu.Lock()
	for _, l := range s.libs {
		l.stateMu.Lock()
		if l.failures > 0 {
			failing++
		}
		l.stateMu.Unlock()
	}
	s.libsMu.Unlock()
	fmt.Fprintf(w, "# HELP pocket_notes_sync_failing People whose notes folder does not answer.\n# TYPE pocket_notes_sync_failing gauge\npocket_notes_sync_failing %d\n", failing)
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/remote.php/") || strings.HasPrefix(r.URL.Path, "/ocs/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}
