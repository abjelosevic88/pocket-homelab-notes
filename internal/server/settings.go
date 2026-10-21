package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/notesmd"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

func chi_token(r *http.Request) string { return chi.URLParam(r, "token") } //nolint:revive

func urlPathEscape(s string) string { return url.PathEscape(s) }

// Settings are the box title (admins change it) plus the caller's own preferences.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r, st))
}

// publicSettings adds what the UI needs to know about the environment.
func (s *Server) publicSettings(r *http.Request, st store.Settings) map[string]any {
	ctx, uid := r.Context(), me(r)
	return map[string]any{
		"title": st.Title, "setup_complete": st.SetupComplete,
		"editor": st.Editor, "file_suffix": st.FileSuffix, "default_folder": st.DefaultFolder, "title_from_first_line": st.TitleFromFirstLine,
		"sort_by": st.SortBy, "spellcheck": st.SpellCheck, "date_format": st.DateFormat,
		"daily_folder": st.DailyFolder, "daily_format": st.DailyFormat, "daily_template": st.DailyTemplate, "capture_format": st.CaptureFormat,
		"templates_folder": st.TemplatesFolder, "attachments_folder": st.AttachmentsFolder,
		"trash_days": st.TrashDays, "versions_keep": st.VersionsKeep, "sync_minutes": st.SyncMinutes,
		"storage_kind": st.StorageKind,
		"notify_kind":  st.NotifyKind, "notify_url": st.NotifyURL, "notify_token_set": st.NotifyToken != "",
		"notify_env": s.cfg.NotifyURL != "" && s.isOwner(ctx, uid),
		"core":       s.core.Configured(),
	}
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cur, err := s.store.GetSettings(ctx, me(r))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in struct {
		store.Settings
		NotifyToken *string `json:"notify_token"`
	}
	in.Settings = cur
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st := in.Settings
	if strings.TrimSpace(st.Title) == "" {
		st.Title = store.DefaultTitle
	}
	if st.Editor != "markdown" {
		st.Editor = "rich"
	}
	if st.FileSuffix != ".txt" {
		st.FileSuffix = ".md"
	}
	if st.SortBy != "title" && st.SortBy != "created" {
		st.SortBy = "modified"
	}
	if st.DateFormat != "dmy-dot" && st.DateFormat != "long" {
		st.DateFormat = "iso"
	}
	st.DefaultFolder = notesmd.SafeFolder(st.DefaultFolder)
	st.DailyFolder = notesmd.SafeFolder(st.DailyFolder)
	st.TemplatesFolder = notesmd.SafeFolder(st.TemplatesFolder)
	st.AttachmentsFolder = notesmd.SafeFolder(st.AttachmentsFolder)
	if strings.TrimSpace(st.DailyFormat) == "" || time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC).Format(st.DailyFormat) == st.DailyFormat {
		st.DailyFormat = "2006-01-02"
	}
	if !strings.Contains(st.CaptureFormat, "{text}") {
		st.CaptureFormat = "- {time} {text}"
	}
	st.TrashDays = clampInt(st.TrashDays, 0, 3650)
	st.VersionsKeep = clampInt(st.VersionsKeep, 10, 10000)
	st.SyncMinutes = clampInt(st.SyncMinutes, 1, 1440)
	if st.NotifyKind != "ntfy" && st.NotifyKind != "webhook" {
		st.NotifyKind = "apprise"
	}
	st.NotifyURL = strings.TrimSpace(st.NotifyURL)
	st.NotifyToken = ""
	if in.NotifyToken != nil {
		st.NotifyToken = strings.TrimSpace(*in.NotifyToken)
		if st.NotifyToken == "" {
			_ = s.store.SetUserSetting(ctx, me(r), "notify_token", "")
		}
	}
	st.SetupComplete = cur.SetupComplete
	if u := userFrom(ctx); u == nil || u.Role != "admin" {
		st.Title = cur.Title // the title is the box's: only admins change it
	}
	if err := s.store.SaveSettings(ctx, me(r), st); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if st.Title != cur.Title {
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	st, _ = s.store.GetSettings(ctx, me(r))
	writeJSON(w, http.StatusOK, s.publicSettings(r, st))
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind, URL, Token string
	}
	_ = decode(r, &in)
	st, _ := s.store.GetSettings(r.Context(), me(r))
	kind, u, tok := st.NotifyKind, st.NotifyURL, st.NotifyToken
	if in.URL != "" {
		kind, u, tok = in.Kind, in.URL, in.Token
		if tok == "" {
			tok = st.NotifyToken
		}
	}
	if u == "" && s.cfg.NotifyURL != "" && s.isOwner(r.Context(), me(r)) {
		kind, u = s.cfg.NotifyKind, s.cfg.NotifyURL
	}
	if err := notify.Send(r.Context(), kind, u, tok, "Pocket Notes", "Notifications work. You hear from Pocket Notes here when your notes folder stops answering.", s.cfg.BaseURL); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	counts := s.store.UserCounts(r.Context(), me(r))
	if u := userFrom(r.Context()); u != nil && u.Role == "admin" {
		if n, err := s.store.CountUsers(r.Context()); err == nil {
			counts["users"] = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": counts, "storage_roots": s.cfg.StorageRoots,
	})
}

// ---------- storage ----------

// storageInput is a storage choice as the UI sends it.
type storageInput struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
	Folder   string `json:"folder"`
	ViaCore  bool   `json:"via_core"`
	// Mode, when the place changes: "move" copies the notes there first,
	// "switch" starts from what is there (the old files stay where they were).
	Mode string `json:"mode"`
}

func (in storageInput) apply(cur store.Settings) (store.Settings, error) {
	st := cur
	st.StorageKind = in.Kind
	st.StoragePath, st.StorageURL, st.StorageUser, st.StorageFolder, st.StorageViaCore = "", "", "", "", false
	switch in.Kind {
	case "builtin":
	case "folder":
		p := filepath.Clean(strings.TrimSpace(in.Path))
		if !filepath.IsAbs(p) {
			return st, errors.New("the folder must be an absolute path")
		}
		st.StoragePath = p
	case "webdav":
		u := strings.TrimRight(strings.TrimSpace(in.URL), "/")
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return st, errors.New("the WebDAV address must start with http:// or https://")
		}
		st.StorageURL, st.StorageUser = u, strings.TrimSpace(in.User)
	case "nextcloud":
		st.StorageViaCore = in.ViaCore
		f, err := storage.Clean(strings.TrimSpace(in.Folder))
		if err != nil {
			return st, err
		}
		st.StorageFolder = f
		if !in.ViaCore {
			u := strings.TrimRight(strings.TrimSpace(in.URL), "/")
			u = strings.TrimSuffix(u, "/index.php")
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				return st, errors.New("the Nextcloud address must start with http:// or https://")
			}
			st.StorageURL, st.StorageUser = u, strings.TrimSpace(in.User)
			if st.StorageUser == "" {
				return st, errors.New("your Nextcloud username is needed")
			}
		}
	case "files":
		f, err := storage.Clean(strings.TrimSpace(in.Folder))
		if err != nil {
			return st, err
		}
		if f == "" {
			return st, errors.New("choose a folder in your Pocket Files (e.g. Notes)")
		}
		st.StorageFolder, st.StorageViaCore = f, true
	default:
		return st, errors.New("unknown storage kind")
	}
	if in.Password != "" {
		st.StoragePassword = in.Password
	} else if cur.StorageKind != in.Kind || (in.Kind == "nextcloud" && cur.StorageURL != st.StorageURL) || (in.Kind == "webdav" && cur.StorageURL != st.StorageURL) {
		st.StoragePassword = "" // a password belongs to the place it was given for
	}
	return st, nil
}

// samePlace reports whether two settings point at the same folder.
func samePlace(a, b store.Settings) bool {
	if a.StorageKind != b.StorageKind {
		return false
	}
	switch a.StorageKind {
	case "folder":
		return a.StoragePath == b.StoragePath
	case "webdav":
		return a.StorageURL == b.StorageURL
	case "nextcloud":
		return a.StorageFolder == b.StorageFolder && (a.StorageViaCore == b.StorageViaCore || a.StorageURL == b.StorageURL)
	case "files":
		return a.StorageFolder == b.StorageFolder
	}
	return true
}

func (s *Server) handleGetStorage(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	out := map[string]any{
		"kind": st.StorageKind, "path": st.StoragePath, "url": st.StorageURL, "user": st.StorageUser, "folder": st.StorageFolder,
		"via_core": st.StorageViaCore, "password_set": st.StoragePassword != "", "roots": s.cfg.StorageRoots,
		"core": s.core.Configured(),
	}
	if l, err := s.lib(ctx, uid); err == nil {
		out["status"] = l.status()
	} else {
		out["status"] = map[string]any{"error": err.Error()}
	}
	if s.core.Configured() {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if cred, err := s.core.ServiceForFresh(cctx, s.userEmail(ctx, uid), "nextcloud"); err == nil {
			out["core_nextcloud"] = map[string]any{"url": firstNonEmpty(cred.PublicURL, cred.URL), "user": cred.Fields["USERNAME"]}
		} else {
			out["core_nextcloud_error"] = err.Error()
		}
		// Pocket Files is offered when Core lets Notes in for this person;
		// otherwise the reason (the link off in Core → Links, Files missing).
		if on, why := s.core.LinkOn(cctx, s.userEmail(ctx, uid), "files", useScopes("files")...); on {
			pf := map[string]any{"on": true}
			if r, err := s.core.Resolve(cctx, "files"); err == nil {
				pf["url"] = firstNonEmpty(r.PublicURL, r.URL)
			}
			out["core_files"] = pf
		} else {
			out["core_files"] = map[string]any{"on": false, "reason": linkOffReason(why)}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// probe builds a backend from a proposed choice and looks at what is there.
func (s *Server) probe(ctx context.Context, uid int64, st store.Settings) (storage.Backend, map[string]any, error) {
	b, err := s.backendFor(ctx, uid, st)
	if err != nil {
		return nil, nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := b.Test(cctx); err != nil {
		return nil, nil, err
	}
	entries, err := b.List(cctx)
	if err != nil {
		return nil, nil, err
	}
	notes, folders, files := 0, 0, 0
	for _, e := range entries {
		switch {
		case e.IsDir:
			folders++
		case isNoteFile(e.Path):
			notes++
		default:
			files++
		}
	}
	return b, map[string]any{"ok": true, "where": b.Describe(), "notes": notes, "folders": folders, "files": files}, nil
}

// linkOffReason says in words why Core does not let Notes into an app.
func linkOffReason(state string) string {
	switch state {
	case "off_admin":
		return "the link is switched off for the whole box (an admin turns it on in Pocket Core → Links)"
	case "off_person":
		return "you switched the link off in Pocket Core → Links"
	case "target_missing":
		return "Pocket Files is not registered with Pocket Core"
	case "not_offered", "not_declared":
		return "Pocket Core does not know this link yet"
	case "not_in_audience":
		return "you do not have access to Pocket Files"
	case "":
		return "Pocket Core did not say why"
	}
	return state
}

func storageErr(err error) string {
	switch {
	case errors.Is(err, storage.ErrAuth):
		return "The sign-in was refused. Check the username and the (app) password."
	case errors.Is(err, storage.ErrNotFound):
		return "That address does not exist. Check the address and the folder."
	}
	return err.Error()
}

func (s *Server) handleTestStorage(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in storageInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	cur, _ := s.store.GetSettings(ctx, uid)
	st, err := in.apply(cur)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, res, err := s.probe(ctx, uid, st)
	if err != nil {
		writeErr(w, http.StatusBadRequest, storageErr(err))
		return
	}
	res["same_place"] = samePlace(cur, st)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePutStorage(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in storageInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	cur, _ := s.store.GetSettings(ctx, uid)
	st, err := in.apply(cur)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	target, res, err := s.probe(ctx, uid, st)
	if err != nil {
		writeErr(w, http.StatusBadRequest, storageErr(err))
		return
	}
	l, lerr := s.lib(ctx, uid)
	moved := !samePlace(cur, st)
	if moved {
		switch in.Mode {
		case "move":
			if lerr != nil {
				writeErr(w, http.StatusBadRequest, "the notes cannot be copied: their current storage does not answer ("+lerr.Error()+")")
				return
			}
			l.mu.Lock()
			copied, renamed, err := storage.Copy(ctx, l.backend, target, nil)
			l.mu.Unlock()
			if err != nil {
				writeErr(w, http.StatusBadGateway, "copying the notes failed: "+storageErr(err))
				return
			}
			res["copied"], res["renamed"] = copied, renamed
			s.log.Info("notes copied to a new storage", "user", uid, "to", target.Describe(), "files", copied, "kept_beside", renamed)
		case "switch":
			if err := s.store.DeleteAllNotes(ctx, uid); err != nil {
				s.fail(w, err, "switch storage")
				return
			}
		default:
			writeErr(w, http.StatusBadRequest, "say whether to copy the notes there (move) or to start from what is there (switch)")
			return
		}
	}
	if err := s.store.SaveStorage(ctx, uid, st); err != nil {
		s.fail(w, err, "save storage")
		return
	}
	if st.StoragePassword == "" && cur.StoragePassword != "" {
		_ = s.store.SetUserSetting(ctx, uid, "storage_password", "")
	}
	stats, err := s.syncUser(ctx, uid, true)
	if err != nil {
		res["sync_error"] = err.Error()
	}
	res["sync"] = stats
	go s.reportModes(context.Background())
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSyncNow(w http.ResponseWriter, r *http.Request) {
	stats, err := s.syncUser(r.Context(), me(r), true)
	if err != nil {
		writeErr(w, http.StatusBadGateway, storageErr(err))
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ---------- backup / export / import ----------

// handleBackup downloads the whole database — every account — so only an
// admin, as the operator of the box, may take it.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if u := userFrom(r.Context()); u == nil || u.Role != "admin" {
		writeErr(w, http.StatusForbidden, "only an admin can download the database; export your notes instead")
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-notes-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	defer os.Remove(dst)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	http.ServeFile(w, r, dst)
}

// handleExportZip downloads the person's whole notes folder (notes and the
// files with them) as a zip, read from wherever it lives.
func (s *Server) handleExportZip(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	l, err := s.lib(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	l.mu.Lock()
	entries, err := l.backend.List(ctx)
	l.mu.Unlock()
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-notes-`+time.Now().Format("2006-01-02")+`.zip"`)
	zw := zip.NewWriter(w)
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		data, _, err := l.backend.Read(ctx, e.Path)
		if err != nil {
			s.log.Warn("export: skipped a file", "path", e.Path, "err", err)
			continue
		}
		hdr := &zip.FileHeader{Name: e.Path, Method: zip.Deflate, Modified: e.Modified}
		f, err := zw.CreateHeader(hdr)
		if err != nil {
			return
		}
		_, _ = f.Write(data)
	}
	_ = zw.Close()
}

// handleImport takes Markdown or text files, or a zip of a notes folder
// (folders and pictures kept), into the person's notes.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "send the files as multipart form data (field \"file\")")
		return
	}
	base := notesmd.SafeFolder(r.FormValue("category"))
	l, err := s.lib(ctx, uid)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	notes, files, skipped := 0, 0, 0
	var problems []string
	put := func(rel string, data []byte) {
		rel = strings.TrimLeft(strings.ReplaceAll(rel, "\\", "/"), "/")
		parts := strings.Split(rel, "/")
		for i := range parts {
			if i < len(parts)-1 {
				parts[i] = notesmd.SafeName(parts[i])
			}
		}
		rel = path.Join(base, strings.Join(parts, "/"))
		clean, err := storage.Clean(rel)
		if err != nil || clean == "" || storage.Hidden(clean) && !strings.Contains(clean, ".attachments.") {
			skipped++
			return
		}
		if isNoteFile(clean) {
			cat, title := splitPath(clean)
			content := string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
			if _, err := s.createNote(ctx, uid, NoteInput{Title: &title, Category: &cat, Content: &content}, "import"); err != nil {
				problems = append(problems, clean+": "+err.Error())
				return
			}
			notes++
			return
		}
		l.mu.Lock()
		_, err = l.backend.Create(ctx, clean, data)
		l.mu.Unlock()
		if errors.Is(err, storage.ErrExists) {
			skipped++
			return
		}
		if err != nil {
			problems = append(problems, clean+": "+err.Error())
			return
		}
		files++
	}
	for _, fh := range r.MultipartForm.File["file"] {
		f, err := fh.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		if strings.EqualFold(path.Ext(fh.Filename), ".zip") {
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				problems = append(problems, fh.Filename+": not a readable zip")
				continue
			}
			for _, zf := range zr.File {
				if zf.FileInfo().IsDir() || strings.HasPrefix(zf.Name, "__MACOSX/") || path.Base(zf.Name) == ".DS_Store" {
					continue
				}
				rc, err := zf.Open()
				if err != nil {
					continue
				}
				b, err := io.ReadAll(io.LimitReader(rc, 200<<20))
				rc.Close()
				if err == nil {
					put(zf.Name, b)
				}
			}
			continue
		}
		put(fh.Filename, data)
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes, "files": files, "skipped": skipped, "problems": problems})
}
