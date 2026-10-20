package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// Edits made in the folder by other apps reach the index: new files, changes,
// renames (history and favourites kept) and deletions (into the trash).
func TestSyncFollowsTheFolder(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("me@example.com")
	ctx := context.Background()
	dir := h.s.builtinDir(u.ID)
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Projects/Plan.md", "# Plan\n- [ ] one #work")
	write("Daily/2026-10-13.md", "# 2026-10-13\n\n- 9:12 Check [[Plan]]")
	write("attachments/pic.jpg", "jpeg")
	write(".hidden/secret.md", "no")
	stats, err := h.s.syncUser(ctx, u.ID, true)
	if err != nil || stats.Added != 2 {
		t.Fatalf("first sync: %+v %v", stats, err)
	}
	plan, err := h.st.GetNoteByPath(ctx, u.ID, "Projects/Plan.md")
	if err != nil || plan.Title != "Plan" || plan.Category != "Projects" || plan.TasksOpen != 1 || len(plan.Tags) != 1 {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	var bl struct{ Backlinks []store.Note }
	h.do(tok, "GET", "/api/v1/notes/"+itoa(plan.ID)+"/backlinks", nil, &bl)
	if len(bl.Backlinks) != 1 {
		t.Fatalf("backlinks: %+v", bl)
	}
	fav := true
	if _, err := h.s.updateNote(ctx, u.ID, plan.ID, NoteInput{Favorite: &fav}, "", "web"); err != nil {
		t.Fatal(err)
	}
	// Changed outside: re-read, a version kept.
	time.Sleep(10 * time.Millisecond)
	write("Projects/Plan.md", "# Plan\n- [x] one #work\n- [ ] two")
	if stats, _ := h.s.syncUser(ctx, u.ID, true); stats.Changed != 1 {
		t.Fatalf("change: %+v", stats)
	}
	// Renamed outside (no file ids here): followed by its content.
	if err := os.Rename(filepath.Join(dir, "Projects/Plan.md"), filepath.Join(dir, "Archive.md")); err != nil {
		t.Fatal(err)
	}
	if stats, _ := h.s.syncUser(ctx, u.ID, true); stats.Moved != 1 || stats.Removed != 0 {
		t.Fatalf("move: %+v", stats)
	}
	moved, err := h.st.GetNote(ctx, u.ID, plan.ID)
	if err != nil || moved.Path != "Archive.md" || !moved.Favorite {
		t.Fatalf("moved note kept its id and star: %+v %v", moved, err)
	}
	vs, _ := h.st.ListVersions(ctx, u.ID, plan.ID)
	if len(vs) < 2 {
		t.Fatalf("versions: %d", len(vs))
	}
	// Deleted outside: into the trash with its text.
	os.Remove(filepath.Join(dir, "Daily/2026-10-13.md"))
	if stats, _ := h.s.syncUser(ctx, u.ID, true); stats.Removed != 1 {
		t.Fatalf("delete: %+v", stats)
	}
	trash, _ := h.st.ListTrash(ctx, u.ID)
	if len(trash) != 1 || trash[0].Reason != "gone" {
		t.Fatalf("trash: %+v", trash)
	}
	// An empty folder (a storage gone wrong) never empties the index.
	for i := 0; i < 4; i++ {
		title := "N" + itoa(int64(i))
		if _, err := h.s.createNote(ctx, u.ID, NoteInput{Title: &title}, "web"); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
	if _, err := h.s.syncUser(ctx, u.ID, true); err == nil || !strings.Contains(err.Error(), "nothing was removed") {
		t.Fatalf("empty folder: %v", err)
	}
	if n := h.st.NoteDashboard(ctx, u.ID).Notes; n != 5 {
		t.Fatalf("index kept %d notes", n)
	}
}

// A save never overwrites what another app wrote meanwhile.
func TestConflicts(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("me@example.com")
	var n store.Note
	h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "Doc", "content": "one"}, &n)
	// A stale etag from the client: 409 with the current note.
	var out map[string]any
	if c := h.do(tok, "PUT", "/api/v1/notes/"+itoa(n.ID), map[string]any{"content": "two", "etag": "stale"}, &out); c != 409 || out["conflict"] != true {
		t.Fatalf("stale etag: %d %v", c, out)
	}
	// The file changed on disk since the index read it.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(h.s.builtinDir(u.ID), n.Path), []byte("changed elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := h.do(tok, "PUT", "/api/v1/notes/"+itoa(n.ID), map[string]any{"content": "mine", "etag": n.Hash}, &out); c != 409 {
		t.Fatalf("outside change: %d %v", c, out)
	}
	data, _ := os.ReadFile(filepath.Join(h.s.builtinDir(u.ID), n.Path))
	if string(data) != "changed elsewhere" {
		t.Fatalf("overwrote the other edit: %q", data)
	}
	// With the current etag it goes through, and the title follows the first line.
	var cur store.Note
	h.do(tok, "GET", "/api/v1/notes/"+itoa(n.ID), nil, &cur)
	if c := h.do(tok, "PUT", "/api/v1/notes/"+itoa(n.ID), map[string]any{"content": "# Renamed\nmine", "etag": cur.Hash}, &cur, "X-Pocket-Source", "web"); c != 200 || cur.Title != "Renamed" || cur.Path != "Renamed.md" {
		t.Fatalf("save: %d %+v", c, cur)
	}
}

// The Nextcloud Notes API, as its apps (and the Pocket Homelab app) use it.
func TestNextcloudNotesAPI(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("me@example.com")
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@example.com:"+tok))
	api := "/index.php/apps/notes/api/v1"
	var created ncNote
	if c := h.do("", "POST", api+"/notes", map[string]any{"title": "Shopping", "category": "Lists", "content": "- milk"}, &created, "Authorization", basic); c != 200 || created.ID == 0 || created.Category != "Lists" {
		t.Fatalf("create: %d %+v", c, created)
	}
	var list []ncNote
	h.do("", "GET", api+"/notes?exclude=content", nil, &list, "Authorization", basic)
	if len(list) != 1 || list[0].Content != "" || list[0].Title != "Shopping" {
		t.Fatalf("list: %+v", list)
	}
	var cur ncNote
	if c := h.do("", "PUT", api+"/notes/"+itoa(created.ID), map[string]any{"content": "- milk\n- eggs"}, &cur, "Authorization", basic, "If-Match", `"`+created.ETag+`"`); c != 200 {
		t.Fatalf("update: %d", c)
	}
	if c := h.do("", "PUT", api+"/notes/"+itoa(created.ID), map[string]any{"content": "x"}, &cur, "Authorization", basic, "If-Match", `"`+created.ETag+`"`); c != 412 || !strings.Contains(cur.Content, "eggs") {
		t.Fatalf("stale If-Match: %d %+v", c, cur)
	}
	var set map[string]string
	h.do("", "GET", api+"/settings", nil, &set, "Authorization", basic)
	if set["notesPath"] != "Notes" || set["fileSuffix"] != ".md" {
		t.Fatalf("settings: %v", set)
	}
	// WebDAV under the notes path, as the app makes folders and stores files.
	req, _ := http.NewRequest("MKCOL", h.ts.URL+"/remote.php/dav/files/me/Notes/Lists/Sub", nil)
	req.Header.Set("Authorization", basic)
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 201 {
		t.Fatalf("mkcol: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("PUT", h.ts.URL+"/remote.php/dav/files/me/Notes/attachments/a%20b.png", strings.NewReader("png"))
	req.Header.Set("Authorization", basic)
	req.Header.Set("If-None-Match", "*")
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 201 {
		t.Fatalf("dav put: %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(h.s.builtinDir(u.ID), "attachments", "a b.png")); err != nil {
		t.Fatalf("dav put landed elsewhere: %v", err)
	}
	req, _ = http.NewRequest("PUT", h.ts.URL+"/remote.php/dav/files/me/Documents/x.txt", strings.NewReader("no"))
	req.Header.Set("Authorization", basic)
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 404 && res.StatusCode != 403 {
		t.Fatalf("dav outside the notes folder: %d", res.StatusCode)
	}
	// The attachment the note links to.
	var att []byte
	_ = att
	if c := h.do("", "GET", "/index.php/apps/notes/notes/"+itoa(created.ID)+"/attachment?path=../attachments/a%20b.png", nil, nil, "Authorization", basic); c != 200 {
		t.Fatalf("attachment: %d", c)
	}
	// Unified search and capabilities.
	var srch struct {
		OCS struct {
			Data struct {
				Entries []map[string]any `json:"entries"`
			} `json:"data"`
		} `json:"ocs"`
	}
	h.do("", "GET", "/ocs/v2.php/search/providers/notes/search?term=eggs&format=json", nil, &srch, "Authorization", basic)
	if len(srch.OCS.Data.Entries) != 1 {
		t.Fatalf("search: %+v", srch)
	}
	if c := h.do("", "GET", "/ocs/v2.php/cloud/capabilities?format=json", nil, nil); c != 200 {
		t.Fatalf("capabilities: %d", c)
	}
	// A wrong password is a Basic challenge.
	req, _ = http.NewRequest("GET", h.ts.URL+api+"/notes", nil)
	req.SetBasicAuth("me@example.com", "pn_wrong")
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 401 || res.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("bad token: %d", res.StatusCode)
	}
	if c := h.do("", "DELETE", api+"/notes/"+itoa(created.ID), nil, nil, "Authorization", basic); c != 200 {
		t.Fatalf("delete: %d", c)
	}
}

// Daily notes and capture write the owner's format ("- 9:12 text").
func TestDailyAndCapture(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("me@example.com")
	var n store.Note
	if c := h.do(tok, "POST", "/api/v1/capture", map[string]any{"text": "Call the plumber"}, &n); c != 200 {
		t.Fatalf("capture: %d", c)
	}
	today := time.Now().Format("2006-01-02")
	if n.Category != "Daily" || n.Title != today || !strings.Contains(n.Content, "Call the plumber") || !strings.HasPrefix(n.Content, "# "+today) {
		t.Fatalf("daily: %+v", n)
	}
	h.do(tok, "POST", "/api/v1/capture", map[string]any{"text": "Second"}, &n)
	if strings.Count(n.Content, "\n- ") != 2 {
		t.Fatalf("second capture: %q", n.Content)
	}
	var tasks []TaskLine
	h.do(tok, "POST", "/api/v1/notes/"+itoa(n.ID)+"/append", map[string]any{"text": "- [ ] buy milk 📅 2020-01-01"}, nil)
	h.do(tok, "GET", "/api/v1/tasks", nil, &tasks)
	if len(tasks) != 1 || tasks[0].Due != "2020-01-01" {
		t.Fatalf("tasks: %+v", tasks)
	}
	var d store.Dashboard
	h.do(tok, "GET", "/api/v1/dashboard", nil, &d)
	if d.Notes != 1 || d.OpenTasks != 1 || d.DueTasks != 1 {
		t.Fatalf("dashboard: %+v", d)
	}
}
