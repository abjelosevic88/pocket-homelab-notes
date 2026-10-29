package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/config"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// fakeDraw is Pocket Core issuing pcx_ link tokens for Pocket Draw (its
// public address https://draw.example.com, its server address the fake), and
// Pocket Draw's API accepting only those tokens, within their scopes.
type fakeDraw struct {
	t    *testing.T
	core *httptest.Server
	api  *httptest.Server

	mu      sync.Mutex
	n       int
	scopes  map[string]string // token → scopes Core gave it
	queries []url.Values      // what the drawings list was asked
	created []map[string]string
}

const drawPublic = "https://draw.example.com"

const drawSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`

func newFakeDraw(t *testing.T) *fakeDraw {
	f := &fakeDraw{t: t, scopes: map[string]string{}}
	f.api = httptest.NewServer(http.HandlerFunc(f.serveAPI))
	t.Cleanup(f.api.Close)
	f.core = httptest.NewServer(http.HandlerFunc(f.serveCore))
	t.Cleanup(f.core.Close)
	return f
}

func (f *fakeDraw) serveCore(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/v1/links/token":
		var in struct {
			App    string   `json:"app"`
			Scopes []string `json:"scopes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.App != "draw" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no such app", "reason": "target_missing"})
			return
		}
		f.n++
		tok := "pcx_" + itoa(int64(f.n))
		f.scopes[tok] = strings.Join(in.Scopes, " ")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "token_type": "Bearer", "expires_in": 300, "scope": f.scopes[tok], "app": "draw", "url": f.api.URL, "public_url": drawPublic})
	case r.Method == "GET" && r.URL.Path == "/api/v1/links/status":
		_ = json.NewEncoder(w).Encode([]map[string]any{{"app": "draw", "scope": "drawings:read", "on": true, "state": "on"}, {"app": "draw", "scope": "drawings:write", "on": true, "state": "on"}})
	case r.Method == "GET" && r.URL.Path == "/api/v1/resolve":
		_ = json.NewEncoder(w).Encode(map[string]any{"slug": "draw", "url": f.api.URL, "public_url": drawPublic})
	case strings.HasPrefix(r.URL.Path, "/api/v1/services/"):
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no account"})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}
}

// allowed reports whether the request's token may use scope.
func (f *fakeDraw) allowed(r *http.Request, scope string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.Contains(" "+f.scopes[tok]+" ", " "+scope+" ")
}

func (f *fakeDraw) serveAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1/drawings":
		if !f.allowed(r, "drawings:read") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.Query())
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"drawings":[
			{"id":12,"title":"Kitchen plan","kind":"sketch","folder_name":"House","edited_at":"2026-10-28T10:00:00Z","has_preview":true,"preview_rev":3,"rev":3},
			{"id":13,"title":"Flow \"v2\" [draft] (old)\nsecond line","kind":"mermaid","folder_name":"","edited_at":"2026-10-27T10:00:00Z","has_preview":false,"preview_rev":0,"rev":1},
			{"id":14,"title":"  ","kind":"drawio","edited_at":"2026-10-26T10:00:00Z","has_preview":true,"preview_rev":5,"rev":6}],"total":3}`)
	case r.Method == "GET" && r.URL.Path == "/api/v1/drawings/12/preview.svg":
		if !f.allowed(r, "drawings:read") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, drawSVG)
	case r.Method == "GET" && r.URL.Path == "/api/v1/drawings/15/preview.svg":
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><script>alert(1)</script></html>")
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/preview.svg"):
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"no picture yet"}`)
	case r.Method == "POST" && r.URL.Path == "/api/v1/drawings":
		if !f.allowed(r, "drawings:write") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.created = append(f.created, in)
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 21, "title": in["title"], "kind": in["kind"], "rev": 1})
	default:
		f.t.Errorf("draw: unexpected %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func drawHarness(t *testing.T) (*harness, *fakeDraw) {
	f := newFakeDraw(t)
	return newHarnessWith(t, func(c *config.Config) { c.CoreURL = f.core.URL }), f
}

// get fetches a path with a bearer token and answers status, headers and body.
func (h *harness) get(tok, path string) (int, http.Header, string) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.ts.URL+path, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(body)
}

// The picture comes from Draw as the person, locked down as a document of
// its own; anything that goes wrong is a grey card, never a broken picture.
func TestDrawPictureProxy(t *testing.T) {
	h, _ := drawHarness(t)
	alice, tok := h.user("alice@example.com")

	code, hdr, body := h.get(tok, "/api/v1/draw/12/picture.svg")
	if code != 200 || body != drawSVG {
		t.Fatalf("picture: %d %q", code, body)
	}
	for k, v := range map[string]string{"Content-Type": "image/svg+xml", "Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:", "X-Content-Type-Options": "nosniff", "Cache-Control": "private, max-age=30"} {
		if hdr.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, hdr.Get(k), v)
		}
	}

	// No picture yet in Draw.
	code, hdr, body = h.get(tok, "/api/v1/draw/13/picture.svg")
	if code != 200 || !strings.Contains(body, "No picture yet") || !strings.HasPrefix(body, "<svg") || hdr.Get("Cache-Control") != "no-store" || hdr.Get("Content-Security-Policy") == "" || hdr.Get("Content-Type") != "image/svg+xml" {
		t.Errorf("no picture: %d %v %q", code, hdr, body)
	}
	// Something that is not a picture is not passed on.
	if _, _, body = h.get(tok, "/api/v1/draw/15/picture.svg"); strings.Contains(body, "script") || !strings.Contains(body, "not available") {
		t.Errorf("not a picture: %q", body)
	}
	// Draw switched off for this person.
	_ = h.st.SetUserSetting(context.Background(), alice.ID, "app.draw.mode", "off")
	code, hdr, body = h.get(tok, "/api/v1/draw/12/picture.svg")
	if code != 200 || !strings.Contains(body, "Drawing not available here") || hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("not connected: %d %v %q", code, hdr, body)
	}
	// Signed out: nothing at all.
	if code, _, _ := h.get("", "/api/v1/draw/12/picture.svg"); code != 401 {
		t.Errorf("signed out: %d", code)
	}
}

// No Pocket Core and nothing typed in: the placeholder, and the picker says
// Draw is not connected.
func TestDrawNotConnected(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("alice@example.com")
	if code, hdr, body := h.get(tok, "/api/v1/draw/12/picture.svg"); code != 200 || !strings.Contains(body, "not available") || hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("placeholder: %d %v %q", code, hdr, body)
	}
	var out struct {
		Items     []drawItem `json:"items"`
		Connected bool       `json:"connected"`
		Error     string     `json:"error"`
	}
	if c := h.do(tok, "GET", "/api/v1/tools/draw", nil, &out); c != 200 || out.Connected || out.Items == nil || len(out.Items) != 0 || !strings.Contains(out.Error, "Pocket Draw") {
		t.Errorf("picker: %d %+v", c, out)
	}
	var tools map[string]bool
	if h.do(tok, "GET", "/api/v1/tools", nil, &tools); tools["draw"] {
		t.Errorf("draw offered without Draw: %v", tools)
	}
	var addr map[string]string
	if h.do(tok, "GET", "/api/v1/tools/addresses", nil, &addr); len(addr) != 0 {
		t.Errorf("addresses: %v", addr)
	}
}

// The picker lists drawings with the Markdown that shows each in the note,
// at Draw's public address; Link to something offers them too; the tool is
// offered once Draw is linked.
func TestDrawPicker(t *testing.T) {
	h, f := drawHarness(t)
	_, tok := h.user("alice@example.com")

	var out struct {
		Items     []drawItem `json:"items"`
		Connected bool       `json:"connected"`
		Error     string     `json:"error"`
	}
	if c := h.do(tok, "GET", "/api/v1/tools/draw?q=plan", nil, &out); c != 200 || !out.Connected || out.Error != "" || len(out.Items) != 3 {
		t.Fatalf("picker: %d %+v", c, out)
	}
	if q := f.queries[0]; q.Get("q") != "plan" || q.Get("limit") != "24" || q.Get("sort") != "edited" {
		t.Errorf("asked %v", q)
	}
	a, b, c := out.Items[0], out.Items[1], out.Items[2]
	if a.ID != 12 || a.Title != "Kitchen plan" || a.Kind != "sketch" || a.Detail != "Sketch · House" || a.EditedAt != "2026-10-28T10:00:00Z" ||
		a.Picture != "/api/v1/draw/12/picture.svg?r=3" || a.URL != drawPublic+"/d/12" ||
		a.Markdown != `![Kitchen plan](https://draw.example.com/d/12/picture.svg "Kitchen plan")` {
		t.Errorf("first %+v", a)
	}
	if b.Title != "Flow v2 draft old second line" || b.Detail != "Text diagram" || b.Markdown != `![Flow v2 draft old second line](https://draw.example.com/d/13/picture.svg "Flow v2 draft old second line")` {
		t.Errorf("cleaned %+v", b)
	}
	if c.Title != "Drawing 14" || c.Markdown != `![Drawing 14](https://draw.example.com/d/14/picture.svg "Drawing 14")` || c.Picture != "/api/v1/draw/14/picture.svg?r=5" {
		t.Errorf("untitled %+v", c)
	}

	var links linkAnswer
	if code := h.do(tok, "GET", "/api/v1/tools/link?q=plan", nil, &links); code != 200 {
		t.Fatalf("link: %d", code)
	}
	if d := links.section("draw"); len(d) != 3 || d[0].Markdown != a.Markdown || d[0].URL != a.URL || d[0].Icon != "drawing" {
		t.Errorf("link section %+v (%v)", d, links.Errors)
	}

	var tools map[string]bool
	if h.do(tok, "GET", "/api/v1/tools", nil, &tools); !tools["draw"] || !tools["link"] {
		t.Errorf("tools %v", tools)
	}
	var addr map[string]string
	if h.do(tok, "GET", "/api/v1/tools/addresses", nil, &addr); addr["draw"] != drawPublic {
		t.Errorf("addresses %v", addr)
	}
	var integ struct {
		Items []integrationView `json:"items"`
	}
	h.do(tok, "GET", "/api/v1/integrations", nil, &integ)
	found := false
	for _, i := range integ.Items {
		if i.Kind == "app" && i.Name == "draw" && i.Title == "Pocket Draw" && i.Mode == "core" {
			found = true
		}
	}
	if !found {
		t.Errorf("integrations %+v", integ.Items)
	}
}

// A drawing started from a note: made in Draw with the write scope, and the
// answer shows it in the note and opens its page.
func TestDrawCreate(t *testing.T) {
	h, f := drawHarness(t)
	_, tok := h.user("alice@example.com")
	var out struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		Markdown string `json:"markdown"`
	}
	if c := h.do(tok, "POST", "/api/v1/tools/draw", map[string]string{"kind": "mermaid", "title": " Release flow "}, &out); c != 200 {
		t.Fatalf("create: %d %+v", c, out)
	}
	if len(f.created) != 1 || f.created[0]["kind"] != "mermaid" || f.created[0]["title"] != "Release flow" {
		t.Errorf("Draw got %v", f.created)
	}
	if out.ID != 21 || out.Title != "Release flow" || out.URL != drawPublic+"/d/21" || out.Markdown != `![Release flow](https://draw.example.com/d/21/picture.svg "Release flow")` {
		t.Errorf("answer %+v", out)
	}
	if c := h.do(tok, "POST", "/api/v1/tools/draw", map[string]string{"kind": "drawio"}, nil); c != 400 {
		t.Errorf("drawio: %d", c)
	}
	if c := h.do(tok, "POST", "/api/v1/tools/draw", map[string]string{}, &out); c != 200 || f.created[1]["kind"] != "sketch" || out.Title != "Drawing 21" {
		t.Errorf("default: %d %v %+v", c, f.created, out)
	}
}

// A note showing a drawing is a note about it: Draw finds it by the
// drawing's page. A shared note shows the drawings it holds, and no others.
func TestDrawNoteMentionsAndShare(t *testing.T) {
	h, _ := drawHarness(t)
	_, tok := h.user("alice@example.com")
	var n, other store.Note
	h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "House", "content": "# House\n\n![Kitchen plan](https://draw.example.com/d/12/picture.svg \"Kitchen plan\")\n"}, &n)
	h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "Other", "content": "![x](https://draw.example.com/d/123/picture.svg)\n"}, &other)

	var got []store.Note
	if c := h.do(tok, "GET", "/api/v1/mentions?url="+url.QueryEscape(drawPublic+"/d/12"), nil, &got); c != 200 || len(got) != 1 || got[0].ID != n.ID {
		t.Fatalf("mentions: %d %+v", c, got)
	}
	if h.do(tok, "GET", "/api/v1/mentions?url="+url.QueryEscape(drawPublic+"/d/1"), nil, &got); len(got) != 0 {
		t.Errorf("d/1 matched %+v", got)
	}

	var sh store.Share
	h.do(tok, "POST", "/api/v1/notes/"+itoa(n.ID)+"/shares", map[string]any{}, &sh)
	code, hdr, body := h.get("", "/api/public/share/"+sh.Token+"/draw/12/picture.svg")
	if code != 200 || body != drawSVG || hdr.Get("Content-Security-Policy") == "" {
		t.Errorf("shared picture: %d %q", code, body)
	}
	if code, _, _ := h.get("", "/api/public/share/"+sh.Token+"/draw/123/picture.svg"); code != 404 {
		t.Errorf("a drawing the note does not show: %d", code)
	}
	if code, _, _ := h.get("", "/api/public/share/nope/draw/12/picture.svg"); code != 404 {
		t.Errorf("unknown share: %d", code)
	}
}

// /new?…&image=1 starts a note that shows the thing as a picture; without it
// the note links to it, in the folder for new notes.
func TestNoteFrom(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	_ = h.st.SetUserSetting(context.Background(), alice.ID, "default_folder", "Inbox")
	var n store.Note
	if c := h.do(tok, "POST", "/api/v1/notes/from", map[string]any{"title": "Kitchen plan", "url": drawPublic + "/d/12/picture.svg", "label": `Kitchen "plan" [v2]`, "folder": "House", "image": true}, &n); c != 201 {
		t.Fatalf("image: %d", c)
	}
	if n.Category != "House" || n.Content != "# Kitchen plan\n\n![Kitchen plan v2](https://draw.example.com/d/12/picture.svg \"Kitchen plan v2\")\n\n" {
		t.Errorf("image note %q in %q", n.Content, n.Category)
	}
	if c := h.do(tok, "POST", "/api/v1/notes/from", map[string]any{"title": "Dune", "url": "https://bookshelf.example.com/books/12 (1)", "label": "📖 Dune [1965]", "text": "Read it again"}, &n); c != 201 {
		t.Fatalf("link: %d", c)
	}
	if n.Category != "Inbox" || n.Content != "# Dune\n\n[📖 Dune \\[1965\\]](https://bookshelf.example.com/books/12%20%281%29)\n\nRead it again\n\n" {
		t.Errorf("link note %q in %q", n.Content, n.Category)
	}
	if c := h.do(tok, "POST", "/api/v1/notes/from", map[string]any{"title": "x", "url": "javascript:alert(1)"}, nil); c != 400 {
		t.Errorf("not a web address: %d", c)
	}
}
