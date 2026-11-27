package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/config"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/notesmd"
)

// fakeMoveCore is Pocket Core answering introspection for Pocket Move's link
// tokens: pcx_<scope>_<name> acts for <name>@example.com with that one scope.
func fakeMoveCore(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/introspect" {
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		var in struct{ Token string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.Split(in.Token, "_") // pcx, scope, name
		if len(parts) != 3 {
			_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
			return
		}
		scope := map[string]string{"exp": core.ScopeMoveExport, "imp": core.ScopeMoveImport}[parts[1]]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true, "kind": "delegated", "scope": scope, "audience": "notes", "iss": "https://core.test",
			"exp":  time.Now().Add(time.Hour).Unix(),
			"user": map[string]any{"sub": "sub-" + parts[2], "email": parts[2] + "@example.com", "name": parts[2]},
			"app":  map[string]any{"id": 9, "slug": "move", "name": "Pocket Move"},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// upload sends files as multipart field "file" to /api/v1/import.
func (h *harness) upload(tok, name string, data []byte, out any) int {
	h.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/v1/import", &body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func (h *harness) fetch(tok, p string) (int, []byte) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.ts.URL+p, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type importAnswer struct {
	Imported, Notes, Files, Skipped int
	Problems                        []string
}

// Pocket Move's link tokens reach exactly the move routes: move:export reads
// the description and the export, move:import posts the import, and nothing
// else (settings, notes) opens to either.
func TestPortableLinkScopes(t *testing.T) {
	coreSrv := fakeMoveCore(t)
	h := newHarnessWith(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
	h.user("alice@example.com")

	exp, imp := "pcx_exp_alice", "pcx_imp_alice"
	var desc core.Portable
	code, raw := h.fetch(exp, core.PortablePath)
	if code != 200 || json.Unmarshal(raw, &desc) != nil || len(desc.Exports) != 1 || desc.Exports[0].Kind != core.KindPocket || len(desc.Imports) != 2 {
		t.Fatalf("portable with move:export: %d %s", code, raw)
	}
	if code, _ := h.fetch(imp, core.PortablePath); code != 200 {
		t.Errorf("portable with move:import: %d", code)
	}
	if code, _ := h.fetch(exp, "/api/v1/export.zip"); code != 200 {
		t.Errorf("export with move:export: %d", code)
	}
	for _, c := range []struct {
		tok, method, path string
	}{
		{exp, "POST", "/api/v1/import"},
		{exp, "GET", "/api/v1/settings"},
		{exp, "GET", "/api/v1/notes"},
		{exp, "GET", "/api/v1/backup"},
		{imp, "GET", "/api/v1/export.zip"},
		{imp, "GET", "/api/v1/settings"},
		{imp, "POST", "/api/v1/notes"},
	} {
		if code := h.do(c.tok, c.method, c.path, map[string]any{}, nil); code != http.StatusForbidden {
			t.Errorf("%s %s with %s: %d, want 403", c.method, c.path, c.tok, code)
		}
	}
	var ans importAnswer
	if code := h.upload(imp, "one.md", []byte("# One\n"), &ans); code != 200 || ans.Notes != 1 {
		t.Errorf("import with move:import: %d %+v", code, ans)
	}
}

// A Markdown zip as Pocket Move makes it: folders become folders, front matter
// names and dates the note, tags stay, pictures next to it are kept, and the
// same zip twice adds nothing.
func TestImportMarkdownZip(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	zipped := zipOf(t, map[string]string{
		"Work/Projects/plan.md": "---\ntitle: \"The big plan\"\ncreated: 2021-03-04T05:06:07Z\nupdated: 2022-01-02 03:04:05\ntags: [alpha, beta]\n---\n# The big plan\n\n![map](map.png)\n",
		"Work/Projects/map.png": "\x89PNG fake",
		"Inbox.md":              "Just text #gamma\n",
		"../escape.md":          "nope",
	})
	var ans importAnswer
	if code := h.upload(tok, "evernote.zip", zipped, &ans); code != 200 || ans.Notes != 3 || ans.Files != 1 {
		t.Fatalf("first import: %d %+v", code, ans)
	}
	// "../escape.md" stays inside the person's notes.
	if esc, err := h.st.FindNoteByTitle(context.Background(), alice.ID, "escape", ""); err != nil || strings.Contains(esc.Path, "..") {
		t.Errorf("zip-slip entry: %+v %v", esc, err)
	}
	n, err := h.st.FindNoteByTitle(context.Background(), alice.ID, "The big plan", "Work/Projects")
	if err != nil {
		t.Fatal(err)
	}
	if n.Category != "Work/Projects" || strings.Contains(n.Content, "title:") || !strings.Contains(n.Content, "tags: [alpha, beta]") {
		t.Errorf("note %q in %q: %q", n.Title, n.Category, n.Content)
	}
	if n.Modified != time.Date(2022, 1, 2, 3, 4, 5, 0, time.UTC).Unix() || !strings.HasPrefix(n.CreatedAt, "2021-03-04T05:06:07") {
		t.Errorf("dates: modified %d created %s", n.Modified, n.CreatedAt)
	}
	var tags []struct{ Tag string }
	h.do(tok, "GET", "/api/v1/tags", nil, &tags)
	if got := len(tags); got < 3 {
		t.Errorf("tags %+v", tags)
	}
	if code, _ := h.fetch(tok, "/api/v1/notes/"+itoa(n.ID)+"/attachment?path=map.png"); code != 200 {
		t.Errorf("picture beside the note: %d", code)
	}

	ans = importAnswer{}
	if code := h.upload(tok, "evernote.zip", zipped, &ans); code != 200 || ans.Imported != 0 || ans.Notes != 0 || ans.Skipped < 4 {
		t.Fatalf("second import: %d %+v", code, ans)
	}
	var list []map[string]any
	h.do(tok, "GET", "/api/v1/notes", nil, &list)
	if len(list) != 3 {
		t.Errorf("after two imports %d notes", len(list))
	}
}

// Notes' own export goes back in through the import (another box, here Bob),
// and only the exporting person's notes are in it.
func TestExportImportRoundTripPerPerson(t *testing.T) {
	h := newHarness(t)
	_, aliceTok := h.user("alice@example.com")
	bob, bobTok := h.user("bob@example.com")
	h.do(aliceTok, "POST", "/api/v1/notes", map[string]any{"title": "Alice secret", "category": "Diary", "content": "# Alice secret\nonly mine"}, nil)
	h.do(bobTok, "POST", "/api/v1/notes", map[string]any{"title": "Bob list", "content": "milk"}, nil)

	code, zipped := h.fetch(aliceTok, "/api/v1/export.zip")
	if code != 200 {
		t.Fatalf("export %d", code)
	}
	if bytes.Contains(zipped, []byte("Bob list")) {
		t.Error("Bob's note in Alice's export")
	}
	_, bobZip := h.fetch(bobTok, "/api/v1/export.zip")
	if bytes.Contains(bobZip, []byte("Alice secret")) {
		t.Error("Alice's note in Bob's export")
	}

	var ans importAnswer
	if code := h.upload(bobTok, "notes.zip", zipped, &ans); code != 200 || ans.Notes != 1 {
		t.Fatalf("round trip: %d %+v", code, ans)
	}
	if _, err := h.st.FindNoteByTitle(context.Background(), bob.ID, "Alice secret", "Diary"); err != nil {
		t.Error("imported note not in Bob's Diary")
	}
	ans = importAnswer{}
	if h.upload(bobTok, "notes.zip", zipped, &ans); ans.Notes != 0 || ans.Skipped != 1 {
		t.Errorf("second round trip %+v", ans)
	}
	var aliceNotes []map[string]any
	h.do(aliceTok, "GET", "/api/v1/notes", nil, &aliceNotes)
	if len(aliceNotes) != 1 {
		t.Errorf("Alice has %d notes after Bob's import", len(aliceNotes))
	}
}

func TestTakeFrontMatter(t *testing.T) {
	meta, body := notesmd.TakeFrontMatter("---\ntitle: 'It''s here'\ndate: 2020-05-06\n---\n\nBody")
	if meta.Title != "It's here" || meta.Created.Format("2006-01-02") != "2020-05-06" || body != "Body" {
		t.Errorf("%+v %q", meta, body)
	}
	if _, body := notesmd.TakeFrontMatter("no front matter\n---\n"); body != "no front matter\n---\n" {
		t.Errorf("changed %q", body)
	}
	if _, body := notesmd.TakeFrontMatter("---\ntitle: x\nsource: keep\n---\nB"); body != "---\nsource: keep\n---\nB" {
		t.Errorf("kept %q", body)
	}
}
