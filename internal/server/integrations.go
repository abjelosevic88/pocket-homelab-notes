package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// The homelab around a note: the services and Pocket apps the editor's tools
// read from (and, for a task or a transaction, write to). Each person reaches
// them through Pocket Core (their own account there: a service connection
// or an app link) or directly, with an address and key typed in here.

// serviceUses are the third-party services the tools use, by Core catalog type.
var serviceUses = []core.ServiceUse{
	{Type: "nextcloud", Reason: "Your notes folder, files to attach or link, and lines sent to Tasks or Deck"},
	{Type: "immich", Reason: "Photos from your library, put into a note"},
	{Type: "forgejo", Reason: "Links to issues and pull requests"},
	{Type: "jellyfin", Reason: "Links to films and series"},
	{Type: "jellystat", Reason: "What you watched this week, in a note"},
	{Type: "audiobookshelf", Reason: "The books you are listening to, in a note"},
	{Type: "kuma", Reason: "Monitors that are down and this week's outages, in a note"},
	{Type: "scrutiny", Reason: "Disks with warnings, in a note"},
	{Type: "backrest", Reason: "Backups that need a look, in a note"},
	{Type: "adguard", Reason: "The hosts on your network, in a note"},
	{Type: "glances", Reason: "What the server is busy with, in a note"},
	{Type: "beszel", Reason: "The homelab snapshot: systems with CPU, memory, disk and temperature"},
}

// libraryOnlyApps are Pocket apps Notes links to only to keep the notes
// folder there (Settings → Storage, always through Pocket Core): they have no
// row under Integrations, where a typed-in connection would mean nothing.
var libraryOnlyApps = map[string]bool{"files": true}

// serviceNames are the names people see.
var serviceNames = map[string]string{
	"tasks":     "Pocket Tasks",
	"nextcloud": "Nextcloud", "immich": "Immich", "forgejo": "Forgejo", "jellyfin": "Jellyfin",
	"jellystat": "Jellystat", "audiobookshelf": "Audiobookshelf", "kuma": "Uptime Kuma", "scrutiny": "Scrutiny", "backrest": "Backrest",
	"adguard": "AdGuard Home", "glances": "Glances", "beszel": "Beszel",
	"habits": "Pocket Habits", "money": "Pocket Money", "garage": "Pocket Garage", "podcasts": "Pocket Podcasts", "documents": "Pocket Documents", "bookshelf": "Pocket Bookshelf",
	"contacts": "Pocket Contacts", "files": "Pocket Files", "draw": "Pocket Draw",
}

// directFields says what a typed-in connection asks for: the key's label and
// whether a username goes with it ("" = no key at all).
var directFields = map[string][2]string{
	"tasks":     {"API token", ""},
	"nextcloud": {"App password", "Username"}, "immich": {"API key", ""}, "forgejo": {"Token", ""},
	"jellyfin": {"API key", "User id (optional)"}, "jellystat": {"API key", ""}, "audiobookshelf": {"Token", ""},
	"kuma": {"API key (optional)", "Status page slug"}, "scrutiny": {"", ""}, "backrest": {"Password", "Username"}, "adguard": {"Password", "Username"},
	"glances": {"", ""}, "beszel": {"Password", "Email"},
	"habits": {"API token", ""}, "money": {"API token", ""}, "garage": {"API token", ""}, "podcasts": {"API token", ""}, "documents": {"API token", ""}, "bookshelf": {"API token", ""},
	"contacts": {"API token", ""}, "draw": {"API token", ""},
}

// errNotConnected is a tool asked for a service the person has not connected.
type errNotConnected struct {
	Name string
	Msg  string // why, when Pocket Core said
}

func (e errNotConnected) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return serviceNames[e.Name] + " is not connected. Settings → Integrations connects it."
}

// svcCred is a person's way into one service.
type svcCred struct {
	Type      string
	URL       string // how this server reaches it
	PublicURL string // what goes into links people open
	Key       string
	Fields    map[string]string
	Source    string // core | direct
}

// Field returns a named field (USERNAME, APP_PASSWORD, …) or "".
func (c *svcCred) Field(k string) string {
	if c == nil || c.Fields == nil {
		return ""
	}
	return c.Fields[k]
}

// Link joins the service's public address with a path, for a link in a note.
func (c *svcCred) Link(p string) string {
	base := strings.TrimRight(firstNonEmpty(c.PublicURL, c.URL), "/")
	return base + p
}

// API joins the address this server uses with a path.
func (c *svcCred) API(p string) string { return strings.TrimRight(c.URL, "/") + p }

// integrationMode is how a person reaches a service ("svc") or Pocket app
// ("app"): "core", "direct" or "off". Unset means direct when an address was
// typed in, else Core when this box has it.
func (s *Server) integrationMode(ctx context.Context, uid int64, kind, name string) string {
	m := s.store.UserSetting(ctx, uid, kind+"."+name+".mode")
	switch m {
	case "core":
		if s.core.Configured() {
			return "core"
		}
		return "off"
	case "direct", "off":
		return m
	}
	if s.store.UserSetting(ctx, uid, kind+"."+name+".url") != "" {
		return "direct"
	}
	if s.core.Configured() {
		return "core"
	}
	return "off"
}

// service returns the person's way into a service, or errNotConnected.
func (s *Server) service(ctx context.Context, uid int64, typ string) (*svcCred, error) {
	switch s.integrationMode(ctx, uid, "svc", typ) {
	case "direct":
		kv := s.store.UserSettingsPrefix(ctx, uid, "svc."+typ+".")
		u := strings.TrimRight(kv["svc."+typ+".url"], "/")
		if u == "" {
			return nil, errNotConnected{Name: typ}
		}
		c := &svcCred{Type: typ, URL: u, PublicURL: strings.TrimRight(firstNonEmpty(kv["svc."+typ+".public_url"], u), "/"), Key: kv["svc."+typ+".key"], Source: "direct", Fields: map[string]string{}}
		user := kv["svc."+typ+".user"]
		switch typ {
		case "nextcloud":
			c.Fields["USERNAME"], c.Fields["APP_PASSWORD"] = user, c.Key
		case "adguard", "backrest":
			c.Fields["USERNAME"], c.Fields["PASSWORD"] = user, c.Key
		case "beszel":
			c.Fields["EMAIL"], c.Fields["PASSWORD"] = user, c.Key
		case "jellyfin", "audiobookshelf":
			c.Fields["USER_ID"] = user
		case "kuma":
			c.Fields["STATUS_SLUG"], c.Fields["API_KEY"] = user, c.Key
		}
		return c, nil
	case "core":
		cred, err := s.core.ServiceFor(ctx, s.userEmail(ctx, uid), typ)
		if err != nil {
			var le *core.LinkError
			if errors.As(err, &le) {
				return nil, errNotConnected{Name: typ, Msg: fmt.Sprintf("%s through Pocket Core: %s", serviceNames[typ], le.Message)}
			}
			return nil, fmt.Errorf("%s through Pocket Core: %w", serviceNames[typ], err)
		}
		return &svcCred{Type: typ, URL: strings.TrimRight(cred.URL, "/"), PublicURL: strings.TrimRight(firstNonEmpty(cred.PublicURL, cred.URL), "/"), Key: cred.Key, Fields: cred.Fields, Source: "core"}, nil
	}
	return nil, errNotConnected{Name: typ}
}

// appToken returns the address of a Pocket app and a token that acts for the
// person there (a short-lived Core link token, or one typed in).
func (s *Server) appToken(ctx context.Context, uid int64, app string, scopes ...string) (base, token string, err error) {
	c, err := s.appCred(ctx, uid, app, scopes...)
	if err != nil {
		return "", "", err
	}
	return c.URL, c.Key, nil
}

// appCred is appToken with the app's public address too (for links people
// open), in the shape the services use: Key is the bearer token.
func (s *Server) appCred(ctx context.Context, uid int64, app string, scopes ...string) (*svcCred, error) {
	switch s.integrationMode(ctx, uid, "app", app) {
	case "direct":
		kv := s.store.UserSettingsPrefix(ctx, uid, "app."+app+".")
		u := strings.TrimRight(kv["app."+app+".url"], "/")
		t := kv["app."+app+".key"]
		if u == "" || t == "" {
			return nil, errNotConnected{Name: app}
		}
		return &svcCred{Type: app, URL: u, PublicURL: strings.TrimRight(firstNonEmpty(kv["app."+app+".public_url"], u), "/"), Key: t, Source: "direct"}, nil
	case "core":
		lt, err := s.core.ActAs(ctx, s.userEmail(ctx, uid), app, scopes...)
		if err != nil {
			var le *core.LinkError
			if errors.As(err, &le) {
				return nil, errNotConnected{Name: app, Msg: fmt.Sprintf("%s through Pocket Core: %s", serviceNames[app], le.Message)}
			}
			return nil, fmt.Errorf("%s through Pocket Core: %w", serviceNames[app], err)
		}
		u := strings.TrimRight(lt.URL, "/")
		return &svcCred{Type: app, URL: u, PublicURL: strings.TrimRight(firstNonEmpty(lt.PublicURL, u), "/"), Key: lt.AccessToken, Source: "core"}, nil
	}
	return nil, errNotConnected{Name: app}
}

// useScopes are the scopes Notes declares for a Pocket app in linkUses.
func useScopes(app string) []string {
	for _, u := range linkUses {
		if u.App == app {
			return u.Scopes
		}
	}
	return nil
}

// appPublicURL is where people open a Pocket app (for links in notes).
func (s *Server) appPublicURL(ctx context.Context, uid int64, app string) string {
	if s.integrationMode(ctx, uid, "app", app) == "direct" {
		return strings.TrimRight(firstNonEmpty(s.store.UserSetting(ctx, uid, "app."+app+".public_url"), s.store.UserSetting(ctx, uid, "app."+app+".url")), "/")
	}
	if s.core.Configured() {
		if r, err := s.core.Resolve(ctx, app); err == nil {
			return strings.TrimRight(firstNonEmpty(r.PublicURL, r.URL), "/")
		}
	}
	return ""
}

// ---- HTTP helpers for the tools ----

// callJSON sends a request and decodes a JSON answer into out (when not nil).
// A non-2xx answer becomes an error that names the service.
func (s *Server) callJSON(ctx context.Context, name, method, target string, hdr map[string]string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case []byte:
			rd = bytes.NewReader(b)
		case string:
			rd = strings.NewReader(b)
		default:
			raw, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(raw)
			if hdr == nil {
				hdr = map[string]string{}
			}
			if _, ok := hdr["Content-Type"]; !ok {
				hdr["Content-Type"] = "application/json"
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PocketNotes/"+Version)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s does not answer: %w", serviceNames[name], err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		text := strings.TrimSpace(string(msg))
		if strings.HasPrefix(text, "<") {
			text = ""
		}
		switch res.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%s refused the key (HTTP %d)", serviceNames[name], res.StatusCode)
		}
		if text != "" {
			return fmt.Errorf("%s: HTTP %d: %s", serviceNames[name], res.StatusCode, text)
		}
		return fmt.Errorf("%s: HTTP %d", serviceNames[name], res.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if w, ok := out.(*[]byte); ok {
		*w, err = io.ReadAll(io.LimitReader(res.Body, 200<<20))
		return err
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s answered something unexpected: %w", serviceNames[name], err)
	}
	return nil
}

// toolErr answers a tool's failure: 412 for "not connected", 502 otherwise.
func toolErr(w http.ResponseWriter, err error) {
	var nc errNotConnected
	if errors.As(err, &nc) {
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": err.Error(), "not_connected": nc.Name})
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeErr(w, http.StatusGatewayTimeout, "the service took too long to answer")
		return
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

// ---- routes ----

func (s *Server) toolRoutes(r chi.Router) {
	r.Get("/integrations", s.handleIntegrations)
	r.Put("/integrations/{kind}/{name}", s.handlePutIntegration)
	r.Post("/integrations/{kind}/{name}/test", s.handleTestIntegration)
	r.Get("/tools", s.handleToolsAvailable)

	// Links to what lives elsewhere, and bookmarks (tools_link.go).
	r.Get("/tools/link", s.handleToolLink)
	r.Post("/tools/bookmark", s.handleToolBookmark)
	// People from Pocket Contacts for @ in the editor (tools_contacts.go).
	r.Get("/tools/contacts", s.handleToolContacts)
	r.Get("/tools/contacts/{id}/photo", s.handleToolContactPhoto)
	// Pictures from Immich and files from Nextcloud (tools_media.go).
	r.Get("/tools/photos", s.handleToolPhotos)
	r.Get("/tools/photos/{asset}/thumb", s.handleToolPhotoThumb)
	r.Post("/tools/photos/attach", s.handleToolPhotosAttach)
	r.Get("/tools/files", s.handleToolFiles)
	r.Post("/tools/files/attach", s.handleToolFilesAttach)
	// Drawings from Pocket Draw, shown in the note (tools_draw.go).
	r.Get("/tools/draw", s.handleToolDraw)
	r.Post("/tools/draw", s.handleToolDrawCreate)
	r.Get("/tools/addresses", s.handleToolAddresses)
	r.Get("/draw/{id}/picture.svg", s.handleDrawPicture)
	// Blocks written from what the services know (tools_reports.go).
	r.Post("/tools/report/{kind}", s.handleToolReport)
	// A line sent away: a task, a card, a transaction (tools_actions.go).
	r.Get("/tools/task/targets", s.handleToolTaskTargets)
	r.Post("/tools/task", s.handleToolTask)
	r.Get("/tools/expense/options", s.handleToolExpenseOptions)
	r.Post("/tools/expense", s.handleToolExpense)
}

// integrationTests is a cheap read per service that proves the key works.
var integrationTests = map[string]func(s *Server, ctx context.Context, c *svcCred) error{
	"nextcloud": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "nextcloud", http.MethodGet, c.API("/ocs/v2.php/cloud/user?format=json"), map[string]string{"OCS-APIRequest": "true", "Authorization": basicAuth(c.Field("USERNAME"), firstNonEmpty(c.Field("APP_PASSWORD"), c.Key))}, nil, nil)
	},
	"immich": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "immich", http.MethodGet, c.API("/api/users/me"), map[string]string{"x-api-key": c.Key}, nil, nil)
	},
	"forgejo": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "forgejo", http.MethodGet, c.API("/api/v1/user"), map[string]string{"Authorization": "token " + c.Key}, nil, nil)
	},
	"jellyfin": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "jellyfin", http.MethodGet, c.API("/System/Info"), map[string]string{"X-Emby-Token": c.Key}, nil, nil)
	},
	"jellystat": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "jellystat", http.MethodGet, c.API("/stats/getLibraryOverview"), map[string]string{"x-api-token": c.Key}, nil, nil)
	},
	"audiobookshelf": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "audiobookshelf", http.MethodGet, c.API("/api/me"), map[string]string{"Authorization": "Bearer " + c.Key}, nil, nil)
	},
	"scrutiny": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "scrutiny", http.MethodGet, c.API("/api/summary"), nil, nil, nil)
	},
	"glances": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "glances", http.MethodGet, c.API("/api/4/cpu"), nil, nil, nil)
	},
	"adguard": func(s *Server, ctx context.Context, c *svcCred) error {
		return s.callJSON(ctx, "adguard", http.MethodGet, c.API("/control/status"), map[string]string{"Authorization": basicAuth(c.Field("USERNAME"), c.Field("PASSWORD"))}, nil, nil)
	},
}

func basicAuth(user, pass string) string {
	req, _ := http.NewRequest(http.MethodGet, "http://x", nil)
	req.SetBasicAuth(user, pass)
	return req.Header.Get("Authorization")
}

type integrationView struct {
	Kind      string `json:"kind"` // svc | app
	Name      string `json:"name"`
	Title     string `json:"title"`
	Reason    string `json:"reason"`
	Mode      string `json:"mode"`
	URL       string `json:"url"`
	PublicURL string `json:"public_url"`
	User      string `json:"user"`
	KeySet    bool   `json:"key_set"`
	KeyLabel  string `json:"key_label"`
	UserLabel string `json:"user_label"`
}

func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	out := []integrationView{}
	add := func(kind, name, reason string) {
		kv := s.store.UserSettingsPrefix(ctx, uid, kind+"."+name+".")
		f := directFields[name]
		out = append(out, integrationView{Kind: kind, Name: name, Title: serviceNames[name], Reason: reason, Mode: s.integrationMode(ctx, uid, kind, name),
			URL: kv[kind+"."+name+".url"], PublicURL: kv[kind+"."+name+".public_url"], User: kv[kind+"."+name+".user"], KeySet: kv[kind+"."+name+".key"] != "",
			KeyLabel: f[0], UserLabel: f[1]})
	}
	for _, u := range serviceUses {
		add("svc", u.Type, u.Reason)
	}
	for _, u := range linkUses {
		if !libraryOnlyApps[u.App] {
			add("app", u.App, u.Reason)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"core": s.coreReady(), "items": out})
}

func validIntegration(kind, name string) bool {
	if kind == "svc" {
		for _, u := range serviceUses {
			if u.Type == name {
				return true
			}
		}
	}
	if kind == "app" {
		for _, u := range linkUses {
			if u.App == name {
				return !libraryOnlyApps[name]
			}
		}
	}
	return false
}

func (s *Server) handlePutIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	kind, name := chi.URLParam(r, "kind"), chi.URLParam(r, "name")
	if !validIntegration(kind, name) {
		writeErr(w, http.StatusNotFound, "unknown integration")
		return
	}
	var in struct {
		Mode      string  `json:"mode"`
		URL       string  `json:"url"`
		PublicURL string  `json:"public_url"`
		User      string  `json:"user"`
		Key       *string `json:"key"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := kind + "." + name + "."
	switch in.Mode {
	case "core", "off", "direct", "":
	default:
		writeErr(w, http.StatusBadRequest, "mode must be core, direct or off")
		return
	}
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if in.Mode == "direct" {
		if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeErr(w, http.StatusBadRequest, "the address must start with http:// or https://")
			return
		}
	}
	kv := map[string]string{p + "mode": in.Mode, p + "url": in.URL, p + "public_url": strings.TrimRight(strings.TrimSpace(in.PublicURL), "/"), p + "user": strings.TrimSpace(in.User)}
	if in.Key != nil {
		kv[p+"key"] = strings.TrimSpace(*in.Key)
	}
	for k, v := range kv {
		if err := s.store.SetUserSetting(ctx, uid, k, v); err != nil {
			s.fail(w, err, "save integration")
			return
		}
	}
	go s.reportModes(context.Background())
	writeJSON(w, http.StatusOK, map[string]string{"mode": s.integrationMode(ctx, uid, kind, name)})
}

func (s *Server) handleTestIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	kind, name := chi.URLParam(r, "kind"), chi.URLParam(r, "name")
	if !validIntegration(kind, name) {
		writeErr(w, http.StatusNotFound, "unknown integration")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if kind == "app" {
		base, tok, err := s.appToken(cctx, uid, name, useScopes(name)...)
		if err == nil {
			err = s.callJSON(cctx, name, http.MethodGet, base+"/api/v1/auth/me", map[string]string{"Authorization": "Bearer " + tok}, nil, nil)
		}
		if err != nil {
			toolErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "via": s.integrationMode(ctx, uid, kind, name)})
		return
	}
	c, err := s.service(cctx, uid, name)
	if err == nil {
		if test := integrationTests[name]; test != nil {
			err = test(s, cctx, c)
		} else {
			err = s.callJSON(cctx, name, http.MethodGet, c.API("/"), nil, nil, nil)
		}
	}
	if err != nil {
		toolErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "via": c.Source})
}

// toolNeeds says what each editor tool needs: any one of the listed
// services ("svc:x") or apps ("app:x") connected.
var toolNeeds = map[string][]string{
	"link":      {"svc:nextcloud", "app:contacts", "app:documents", "app:bookshelf", "app:podcasts", "app:draw", "svc:forgejo", "svc:jellyfin", "svc:immich"},
	"photos":    {"svc:immich"},
	"files":     {"svc:nextcloud"},
	"bookmark":  {},
	"snapshot":  {"svc:beszel", "svc:glances", "svc:kuma", "svc:scrutiny", "svc:backrest"},
	"attention": {"svc:kuma", "svc:scrutiny", "svc:backrest"},
	"media":     {"svc:jellystat", "app:podcasts", "svc:audiobookshelf"},
	"spent":     {"app:money"},
	"fuel":      {"app:garage"},
	"hosts":     {"svc:adguard"},
	"processes": {"svc:glances"},
	"habits":    {"app:habits"},
	"task":      {"svc:nextcloud"},
	"expense":   {"app:money"},
	"contacts":  {"app:contacts"}, // people offered after @ in the editor
	"draw":      {"app:draw"},
}

// connected reports whether a person can reach a service ("svc") or app
// ("app") now: an address typed in, or an account Pocket Core holds for them.
func (s *Server) connected(ctx context.Context, uid int64, kind, name string) bool {
	switch s.integrationMode(ctx, uid, kind, name) {
	case "direct":
		return s.store.UserSetting(ctx, uid, kind+"."+name+".url") != ""
	case "core":
		email := s.userEmail(ctx, uid)
		if kind == "app" {
			on, _ := s.core.LinkOn(ctx, email, name, useScopes(name)...)
			return on
		}
		_, err := s.core.ServiceFor(ctx, email, name)
		return err == nil
	}
	return false
}

// handleToolsAvailable lists the tools this person can use now: a tool is
// offered when one of the things it reads from is connected.
func (s *Server) handleToolsAvailable(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	needs := map[string]bool{}
	for _, list := range toolNeeds {
		for _, n := range list {
			needs[n] = false
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for n := range needs {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			kind, name, _ := strings.Cut(n, ":")
			ok := s.connected(cctx, uid, kind, name)
			mu.Lock()
			needs[n] = ok
			mu.Unlock()
		}(n)
	}
	wg.Wait()
	out := map[string]bool{}
	for tool, list := range toolNeeds {
		ok := len(list) == 0
		for _, n := range list {
			if needs[n] {
				ok = true
				break
			}
		}
		out[tool] = ok
	}
	writeJSON(w, http.StatusOK, out)
}
