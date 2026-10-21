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

type contactsAnswer struct {
	Enabled  bool         `json:"enabled"`
	Contacts []personItem `json:"contacts"`
	Message  string       `json:"message"`
}

// fakeContacts is Pocket Contacts' search and photos, for one bearer token.
func fakeContacts(t *testing.T, token string) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	asked := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		asked = append(asked, r.URL.RequestURI())
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/search":
			if !strings.HasPrefix(strings.ToLower(r.URL.Query().Get("q")), "mar") {
				_, _ = io.WriteString(w, `{"contacts":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"contacts":[
				{"id":12,"fn":"Marko Petrović","org":"Acme","title":"Inženjer","emails":["marko@example.com"],"phones":["+387 65 123 456"],"photo_url":"/api/v1/contacts/12/photo?v=ab12cd","initials":"MP","color":"#0ea5e9"},
				{"id":123,"fn":"Marija [Maja] Ilić","org":"","emails":[],"phones":["+387 66 000 111"],"photo_url":"","initials":"","color":"red;}"}]}`)
		case "/api/v1/contacts/12/photo":
			if r.URL.Query().Get("s") != "64" {
				t.Errorf("photo size %q", r.URL.Query().Get("s"))
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "JPEGDATA")
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

// The @ list and "Link to something": people from Pocket Contacts, linked to
// their page at the address people open (not the one this server uses).
func TestToolContactsDirect(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	srv, asked := fakeContacts(t, "pcon_alice")
	connectApp(t, h, alice.ID, "contacts", srv.URL, "pcon_alice")
	if err := h.st.SetUserSetting(context.Background(), alice.ID, "app.contacts.public_url", "https://contacts.example"); err != nil {
		t.Fatal(err)
	}

	var a contactsAnswer
	if c := h.do(tok, "GET", "/api/v1/tools/contacts?q=mar", nil, &a); c != 200 || !a.Enabled || len(a.Contacts) != 2 {
		t.Fatalf("contacts: %d %+v", c, a)
	}
	m := a.Contacts[0]
	if m.Name != "Marko Petrović" || m.Detail != "Acme · marko@example.com" || m.Initials != "MP" || m.Color != "#0ea5e9" ||
		m.URL != "https://contacts.example/contacts/12" || m.Markdown != "[Marko Petrović](https://contacts.example/contacts/12)" ||
		m.Photo != "/api/v1/tools/contacts/12/photo?s=64&v=ab12cd" {
		t.Errorf("marko %+v", m)
	}
	// No brackets in the link's words, initials made up, a colour that is no colour replaced, the phone when there is no email.
	j := a.Contacts[1]
	if j.Markdown != "[Marija Maja Ilić](https://contacts.example/contacts/123)" || j.Initials != "MM" || j.Color != "#64748b" || j.Photo != "" || j.Detail != "+387 66 000 111" {
		t.Errorf("marija %+v", j)
	}
	if got := (*asked)[0]; got != "/api/v1/search?limit=8&q=mar" {
		t.Errorf("asked %q", got)
	}
	// Nothing typed: nobody, and Contacts is not asked.
	n := len(*asked)
	if c := h.do(tok, "GET", "/api/v1/tools/contacts?q=", nil, &a); c != 200 || !a.Enabled || len(a.Contacts) != 0 || len(*asked) != n {
		t.Errorf("empty query: %d %+v", c, a)
	}

	// The picture comes through this server; one there is none of is a 404.
	req, _ := http.NewRequest("GET", h.ts.URL+"/api/v1/tools/contacts/12/photo?s=64&v=ab12cd", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "JPEGDATA" || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("photo: %d %q %q", res.StatusCode, body, res.Header.Get("Content-Type"))
	}
	if c := h.do(tok, "GET", "/api/v1/tools/contacts/99/photo", nil, nil); c != 404 {
		t.Errorf("missing photo: %d", c)
	}
	if c := h.do(tok, "GET", "/api/v1/tools/contacts/x/photo", nil, nil); c != 400 {
		t.Errorf("bad id: %d", c)
	}

	// "Link to something" lists them as a section of their own, with the picture.
	var la linkAnswer
	if c := h.do(tok, "GET", "/api/v1/tools/link?q=marko", nil, &la); c != 200 || len(la.Errors) != 0 {
		t.Fatalf("link: %d %v", c, la.Errors)
	}
	p := la.section("contacts")
	if len(p) != 2 || p[0].Icon != "person" || p[0].Title != "Marko Petrović" || p[0].Markdown != m.Markdown || p[0].Photo != m.Photo || p[0].Initials != "MP" || p[0].Color != "#0ea5e9" {
		t.Errorf("link section %+v", p)
	}
	var tools map[string]bool
	if h.do(tok, "GET", "/api/v1/tools", nil, &tools); !tools["contacts"] || !tools["link"] {
		t.Errorf("tools %v", tools)
	}

	// A note with the link is the one the person's page lists.
	var note store.Note
	h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "Kickoff", "content": "# Kickoff\n\nWith " + m.Markdown + " today.\n"}, &note)
	var found []store.Note
	if h.do(tok, "GET", "/api/v1/mentions?url="+url.QueryEscape(m.URL), nil, &found); len(found) != 1 || found[0].ID != note.ID {
		t.Errorf("mentions %+v", found)
	}

	// A refused token is an error for the list, not a crash.
	connectApp(t, h, alice.ID, "contacts", srv.URL, "pcon_wrong")
	var e struct{ Error string }
	if c := h.do(tok, "GET", "/api/v1/tools/contacts?q=mar", nil, &e); c != 502 || !strings.Contains(e.Error, "Pocket Contacts") {
		t.Errorf("refused: %d %+v", c, e)
	}
}

// Contacts switched off (or never connected): no people, no error, and
// "Link to something" goes on without them.
func TestToolContactsOff(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	_, bobTok := h.user("bob@example.com")
	srv, asked := fakeContacts(t, "pcon_alice")
	connectApp(t, h, alice.ID, "contacts", srv.URL, "pcon_alice")
	if err := h.st.SetUserSetting(context.Background(), alice.ID, "app.contacts.mode", "off"); err != nil {
		t.Fatal(err)
	}
	for who, tk := range map[string]string{"off": tok, "never connected": bobTok} {
		var a contactsAnswer
		if c := h.do(tk, "GET", "/api/v1/tools/contacts?q=mar", nil, &a); c != 200 || a.Enabled || a.Contacts == nil || len(a.Contacts) != 0 || !strings.Contains(a.Message, "Pocket Contacts") {
			t.Errorf("%s: %d %+v", who, c, a)
		}
		var la linkAnswer
		if c := h.do(tk, "GET", "/api/v1/tools/link?q=marko", nil, &la); c != 200 || la.section("contacts") != nil || len(la.Errors) != 0 {
			t.Errorf("%s link: %d %+v", who, c, la)
		}
		var tools map[string]bool
		if h.do(tk, "GET", "/api/v1/tools", nil, &tools); tools["contacts"] {
			t.Errorf("%s: contacts offered", who)
		}
		if c := h.do(tk, "GET", "/api/v1/tools/contacts/12/photo", nil, nil); c != 412 {
			t.Errorf("%s photo: %d", who, c)
		}
	}
	if len(*asked) != 0 {
		t.Errorf("Contacts asked while off: %v", *asked)
	}
	// Settings → Integrations lists it like the other Pocket apps.
	var integ struct{ Items []integrationView }
	h.do(tok, "GET", "/api/v1/integrations", nil, &integ)
	var row *integrationView
	for i := range integ.Items {
		if integ.Items[i].Name == "contacts" {
			row = &integ.Items[i]
		}
	}
	if row == nil || row.Kind != "app" || row.Title != "Pocket Contacts" || row.Mode != "off" || row.KeyLabel != "API token" {
		t.Errorf("integration row %+v", row)
	}
}

// Through Pocket Core: a contacts:read link token acts for the person, and
// the link goes to the public address Core gives with it.
func TestToolContactsThroughCore(t *testing.T) {
	srv, asked := fakeContacts(t, "pcx_contacts_alice")
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/links/token":
			var in struct {
				User   string   `json:"user"`
				App    string   `json:"app"`
				Scopes []string `json:"scopes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.App != "contacts" || strings.Join(in.Scopes, " ") != "contacts:read" || in.User != "alice@example.com" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "no link", "reason": "off_user"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_contacts_alice", "token_type": "Bearer", "expires_in": 300, "scope": "contacts:read", "app": "contacts", "url": srv.URL, "public_url": "https://contacts.bjelke.example"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer coreSrv.Close()
	h := newHarnessWith(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
	_, tok := h.user("alice@example.com")
	_, bobTok := h.user("bob@example.com")

	var a contactsAnswer
	if c := h.do(tok, "GET", "/api/v1/tools/contacts?q=Mar", nil, &a); c != 200 || !a.Enabled || len(a.Contacts) != 2 ||
		a.Contacts[0].Markdown != "[Marko Petrović](https://contacts.bjelke.example/contacts/12)" {
		t.Fatalf("through core: %d %+v", c, a)
	}
	if len(*asked) != 1 {
		t.Errorf("asked %v", *asked)
	}
	// Core refusing the link (switched off for bob) is "not enabled", not an error.
	if c := h.do(bobTok, "GET", "/api/v1/tools/contacts?q=mar", nil, &a); c != 200 || a.Enabled || len(a.Contacts) != 0 {
		t.Errorf("refused link: %d %+v", c, a)
	}
}
