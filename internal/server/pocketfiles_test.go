package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/webdav"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/config"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// fakeFiles is Pocket Core issuing pcx_ link tokens for Pocket Files, and
// Pocket Files' WebDAV (/dav/, one home per person) accepting them only as
// the Basic password while Core says they are good.
type fakeFiles struct {
	t     *testing.T
	core  *httptest.Server
	dav   *httptest.Server
	homes string

	mu       sync.Mutex
	n        int
	tokens   map[string]string // token → email
	asked    []string          // emails Core issued tokens for
	refuse   string            // a refusal reason ("off_admin"), or ""
	expires  int
	used     map[string]int // token → requests it signed
	badCalls int
}

func newFakeFiles(t *testing.T) *fakeFiles {
	f := &fakeFiles{t: t, homes: t.TempDir(), tokens: map[string]string{}, used: map[string]int{}, expires: 300}
	f.dav = httptest.NewServer(http.HandlerFunc(f.serveDAV))
	t.Cleanup(f.dav.Close)
	f.core = httptest.NewServer(http.HandlerFunc(f.serveCore))
	t.Cleanup(f.core.Close)
	return f
}

func (f *fakeFiles) serveCore(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/v1/links/token":
		var in struct {
			User   string   `json:"user"`
			App    string   `json:"app"`
			Scopes []string `json:"scopes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.App != "files" || strings.Join(in.Scopes, " ") != "files:read files:write" {
			f.t.Errorf("token asked for %s %v", in.App, in.Scopes)
		}
		if f.refuse != "" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "files files:read: switched off for the whole box", "reason": f.refuse})
			return
		}
		f.n++
		tok := "pcx_" + itoa(int64(f.n))
		f.tokens[tok] = in.User
		f.asked = append(f.asked, in.User)
		json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "token_type": "Bearer", "expires_in": f.expires, "scope": "files:read files:write", "app": "files", "url": f.dav.URL, "public_url": "https://drive.example.com"})
	case r.Method == "GET" && r.URL.Path == "/api/v1/links/status":
		on, state := f.refuse == "", "on"
		if !on {
			state = f.refuse
		}
		json.NewEncoder(w).Encode([]map[string]any{{"app": "files", "scope": "files:read", "on": on, "state": state}, {"app": "files", "scope": "files:write", "on": on, "state": state}})
	case r.Method == "GET" && r.URL.Path == "/api/v1/resolve":
		json.NewEncoder(w).Encode(map[string]any{"slug": "files", "url": f.dav.URL, "public_url": "https://drive.example.com"})
	case strings.HasPrefix(r.URL.Path, "/api/v1/services/"):
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "no account"})
	default:
		json.NewEncoder(w).Encode(map[string]any{})
	}
}

func (f *fakeFiles) serveDAV(w http.ResponseWriter, r *http.Request) {
	_, pass, ok := r.BasicAuth()
	f.mu.Lock()
	email := f.tokens[pass]
	if ok && email != "" {
		f.used[pass]++
	} else {
		f.badCalls++
	}
	f.mu.Unlock()
	if !ok || email == "" || !strings.HasPrefix(pass, "pcx_") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	home := filepath.Join(f.homes, email)
	_ = os.MkdirAll(home, 0o755)
	(&webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(home), LockSystem: webdav.NewMemLS()}).ServeHTTP(w, r)
}

// revoke makes every token issued so far unknown to the WebDAV side.
func (f *fakeFiles) revoke() {
	f.mu.Lock()
	f.tokens = map[string]string{}
	f.mu.Unlock()
}

func (f *fakeFiles) issued() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

func (f *fakeFiles) home(email string, rel ...string) string {
	return filepath.Join(append([]string{f.homes, email}, rel...)...)
}

// Pocket Files as the notes folder: offered through Pocket Core, one link
// token per few minutes used as the WebDAV password (a refused one renewed),
// ids by path, strictly per person, refusals shown as Core gives them.
func TestPocketFilesLibrary(t *testing.T) {
	ff := newFakeFiles(t)
	h := newHarnessWith(t, func(c *config.Config) { c.CoreURL = ff.core.URL })
	alice, tok := h.user("alice@example.com")
	bob, btok := h.user("bob@example.com")
	ctx := context.Background()

	var info map[string]any
	h.do(tok, "GET", "/api/v1/storage", nil, &info)
	if cf, _ := info["core_files"].(map[string]any); cf["on"] != true || cf["url"] != "https://drive.example.com" {
		t.Fatalf("storage info: %v", info)
	}
	var integ struct{ Items []integrationView }
	h.do(tok, "GET", "/api/v1/integrations", nil, &integ)
	for _, it := range integ.Items {
		if it.Name == "files" {
			t.Fatal("Pocket Files has no Integrations row: it is a library only")
		}
	}

	// No folder: refused; with one, made and used.
	if c := h.do(tok, "PUT", "/api/v1/storage", map[string]any{"kind": "files", "folder": "", "mode": "switch"}, nil); c != 400 {
		t.Fatalf("empty folder accepted: %d", c)
	}
	var res map[string]any
	if c := h.do(tok, "PUT", "/api/v1/storage", map[string]any{"kind": "files", "folder": "Notes", "mode": "switch"}, &res); c != 200 || res["where"] != "Pocket Files · Notes" {
		t.Fatalf("choose Pocket Files: %d %v", c, res)
	}
	if fi, err := os.Stat(ff.home("alice@example.com", "Notes")); err != nil || !fi.IsDir() {
		t.Fatalf("notes folder not made: %v", err)
	}

	// Writing a note puts a Markdown file there.
	var n store.Note
	if c := h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "Plan", "category": "Work", "content": "# Plan\n- [ ] one"}, &n); c != 201 {
		t.Fatalf("create: %d", c)
	}
	if data, err := os.ReadFile(ff.home("alice@example.com", "Notes", "Work", "Plan.md")); err != nil || string(data) != "# Plan\n- [ ] one" {
		t.Fatalf("file: %q %v", data, err)
	}
	if n.ID < 1_000_000_000 {
		t.Fatalf("no file ids in Pocket Files: the id comes from the index, got %d", n.ID)
	}
	// Saving goes through with the file's ETag, and a change made in Pocket
	// Files meanwhile is not overwritten.
	var cur store.Note
	if c := h.do(tok, "PUT", "/api/v1/notes/"+itoa(n.ID), map[string]any{"content": "# Plan\n- [x] one", "etag": n.Hash}, &cur); c != 200 {
		t.Fatalf("save: %d %+v", c, cur)
	}
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(ff.home("alice@example.com", "Notes", "Work", "Plan.md"), []byte("# Plan\nedited in Files"), 0o644)
	if c := h.do(tok, "PUT", "/api/v1/notes/"+itoa(n.ID), map[string]any{"content": "mine", "etag": cur.Hash}, nil); c != 409 {
		t.Fatalf("outside change overwritten: %d", c)
	}

	// Files added and moved in Pocket Files come in; a move keeps the note (by its text).
	_ = os.MkdirAll(ff.home("alice@example.com", "Notes", "Ideas"), 0o755)
	_ = os.WriteFile(ff.home("alice@example.com", "Notes", "Ideas", "Boat.md"), []byte("# Boat\nsail"), 0o644)
	if stats, err := h.s.syncUser(ctx, alice.ID, true); err != nil || stats.Added != 1 || stats.Files != 2 {
		t.Fatalf("sync: %+v %v", stats, err)
	}
	if got, _ := h.st.GetNote(ctx, alice.ID, n.ID); got == nil || got.Content != "# Plan\nedited in Files" {
		t.Fatalf("the edit made in Pocket Files did not come in: %+v", got)
	}
	if err := os.Rename(ff.home("alice@example.com", "Notes", "Work", "Plan.md"), ff.home("alice@example.com", "Notes", "Plan.md")); err != nil {
		t.Fatal(err)
	}
	if stats, err := h.s.syncUser(ctx, alice.ID, true); err != nil || stats.Moved != 1 || stats.Removed != 0 {
		t.Fatalf("move: %+v %v", stats, err)
	}
	if moved, err := h.st.GetNote(ctx, alice.ID, n.ID); err != nil || moved.Path != "Plan.md" {
		t.Fatalf("moved note kept its id: %+v %v", moved, err)
	}

	// All of that on one token, sent as the Basic password.
	if got := ff.issued(); got != 1 {
		t.Fatalf("%d tokens asked for; one is kept for a few minutes", got)
	}
	if ff.used["pcx_1"] < 5 {
		t.Fatalf("token used for %d requests", ff.used["pcx_1"])
	}
	// A token Pocket Files no longer takes is replaced at once (401 → new token, same request again).
	ff.revoke()
	if _, err := h.s.syncUser(ctx, alice.ID, true); err != nil {
		t.Fatalf("sync after the token was refused: %v", err)
	}
	if got := ff.issued(); got != 2 || ff.used["pcx_2"] == 0 {
		t.Fatalf("refused token not renewed: issued %d, used %v", got, ff.used)
	}

	// Deleting a note deletes the file (Pocket Files keeps it in its trash).
	var boat store.Note
	if b, err := h.st.GetNoteByPath(ctx, alice.ID, "Ideas/Boat.md"); err == nil {
		boat = *b
	}
	if c := h.do(tok, "DELETE", "/api/v1/notes/"+itoa(boat.ID), nil, nil); c >= 300 {
		t.Fatalf("delete: %d", c)
	}
	if _, err := os.Stat(ff.home("alice@example.com", "Notes", "Ideas", "Boat.md")); !os.IsNotExist(err) {
		t.Fatalf("file still there: %v", err)
	}

	// Strictly per person: Bob's own folder in his own Pocket Files, tokens for him.
	if c := h.do(btok, "PUT", "/api/v1/storage", map[string]any{"kind": "files", "folder": "Notes", "mode": "switch"}, nil); c != 200 {
		t.Fatalf("bob: %d", c)
	}
	var list []store.Note
	h.do(btok, "GET", "/api/v1/notes", nil, &list)
	if len(list) != 0 {
		t.Fatalf("bob sees %d notes of alice's", len(list))
	}
	title := "Bob's"
	if _, err := h.s.createNote(ctx, bob.ID, NoteInput{Title: &title}, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ff.home("bob@example.com", "Notes", "Bob's.md")); err != nil {
		t.Fatalf("bob's note not in his home: %v", err)
	}
	ff.mu.Lock()
	for i, e := range ff.asked {
		if (i < 2 && e != "alice@example.com") || (i >= 2 && e != "bob@example.com") {
			t.Errorf("token %d issued for %s", i+1, e)
		}
	}
	ff.mu.Unlock()
	if _, err := h.st.GetNoteByPath(ctx, alice.ID, "Bob's.md"); err == nil {
		t.Fatal("bob's note in alice's index")
	}

	// Core switches the link off: the next sync fails with Core's reason, the
	// index is kept, and the Storage page says why.
	ff.mu.Lock()
	ff.refuse = "off_admin"
	ff.mu.Unlock()
	ff.revoke()
	_, err := h.s.syncUser(ctx, alice.ID, true)
	if err == nil || !strings.Contains(err.Error(), "Pocket Files through Pocket Core: files files:read: switched off for the whole box") {
		t.Fatalf("refusal: %v", err)
	}
	if _, err := h.st.GetNote(ctx, alice.ID, n.ID); err != nil {
		t.Fatalf("index emptied on a refusal: %v", err)
	}
	info = nil
	h.do(tok, "GET", "/api/v1/storage", nil, &info)
	cf, _ := info["core_files"].(map[string]any)
	st, _ := info["status"].(map[string]any)
	if cf["on"] != false || !strings.Contains(cf["reason"].(string), "whole box") || !strings.Contains(st["error"].(string), "switched off") {
		t.Fatalf("storage info when off: %v", info)
	}
}

// A token is kept for at most four minutes and never past its last half-minute.
func TestLinkTokensKeep(t *testing.T) {
	calls := 0
	expires := 300
	lt := &linkTokens{fetch: func(context.Context) (*core.LinkToken, error) {
		calls++
		return &core.LinkToken{AccessToken: "pcx_" + itoa(int64(calls)), ExpiresIn: expires, URL: "http://files"}, nil
	}}
	ctx := context.Background()
	a, _ := lt.get(ctx)
	b, _ := lt.get(ctx)
	if calls != 1 || a.AccessToken != b.AccessToken || time.Until(lt.until) > linkTokenKeep {
		t.Fatalf("kept: calls=%d until=%v", calls, time.Until(lt.until))
	}
	lt.drop()
	if c, _ := lt.get(ctx); c.AccessToken != "pcx_2" {
		t.Fatalf("dropped token reused: %s", c.AccessToken)
	}
	expires = 30 // nothing left to use safely: asked for again every time
	lt.drop()
	lt.get(ctx)
	lt.get(ctx)
	if calls != 4 {
		t.Fatalf("short-lived token kept: calls=%d", calls)
	}
}

// Without Pocket Core there is no Pocket Files option.
func TestPocketFilesNeedsCore(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("alice@example.com")
	var info map[string]any
	h.do(tok, "GET", "/api/v1/storage", nil, &info)
	if info["core"] != false || info["core_files"] != nil {
		t.Fatalf("storage info without Core: %v", info)
	}
	var out map[string]any
	if c := h.do(tok, "PUT", "/api/v1/storage", map[string]any{"kind": "files", "folder": "Notes", "mode": "switch"}, &out); c != 400 || !strings.Contains(out["error"].(string), "CORE_URL") {
		t.Fatalf("files without Core: %d %v", c, out)
	}
}
