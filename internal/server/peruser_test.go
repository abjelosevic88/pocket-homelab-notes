package server

import (
	"testing"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// Two people, one box: nothing of one is reachable by the other.
func TestStrictlyPerUser(t *testing.T) {
	h := newHarness(t)
	alice, aTok := h.user("alice@example.com")
	bob, bTok := h.user("bob@example.com")
	if alice.Role != "admin" || bob.Role != "member" {
		t.Fatalf("roles: %s %s", alice.Role, bob.Role)
	}
	var n store.Note
	if c := h.do(aTok, "POST", "/api/v1/notes", map[string]any{"title": "Secret", "category": "Private", "content": "# Secret\n- [ ] task #mine"}, &n); c != 201 {
		t.Fatalf("create: %d", c)
	}
	var sh store.Share
	h.do(aTok, "POST", "/api/v1/notes/"+itoa(n.ID)+"/shares", map[string]any{}, &sh)
	id := itoa(n.ID)
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/v1/notes/" + id, ""}, {"PUT", "/api/v1/notes/" + id, `{"content":"x"}`}, {"DELETE", "/api/v1/notes/" + id, ""},
		{"POST", "/api/v1/notes/" + id + "/append", `{"text":"x"}`}, {"POST", "/api/v1/notes/" + id + "/tasks", `{"line":1,"done":true}`},
		{"POST", "/api/v1/notes/" + id + "/duplicate", ""}, {"GET", "/api/v1/notes/" + id + "/backlinks", ""},
		{"GET", "/api/v1/notes/" + id + "/versions", ""}, {"GET", "/api/v1/notes/" + id + "/versions/1", ""}, {"POST", "/api/v1/notes/" + id + "/versions/1/restore", ""},
		{"GET", "/api/v1/notes/" + id + "/attachment?path=x.png", ""}, {"GET", "/api/v1/notes/" + id + "/shares", ""}, {"POST", "/api/v1/notes/" + id + "/shares", "{}"},
		{"GET", "/api/v1/notes/" + id + "/download", ""}, {"DELETE", "/api/v1/shares/" + itoa(sh.ID), ""},
		{"GET", "/index.php/apps/notes/api/v1/notes/" + id, ""}, {"PUT", "/index.php/apps/notes/api/v1/notes/" + id, `{"content":"x"}`},
		{"DELETE", "/index.php/apps/notes/api/v1/notes/" + id, ""},
	} {
		var body any
		if c.body != "" {
			body = c.body
		}
		if code := h.do(bTok, c.method, c.path, body, nil); code != 404 {
			t.Errorf("bob %s %s → %d, want 404", c.method, c.path, code)
		}
	}
	if code := h.do(aTok, "GET", "/api/v1/notes/"+id, nil, nil); code != 200 {
		t.Fatalf("alice lost her note: %d", code)
	}
	var list []store.Note
	h.do(bTok, "GET", "/api/v1/notes", nil, &list)
	if len(list) != 0 {
		t.Errorf("bob sees %d notes", len(list))
	}
	var hits []store.SearchHit
	h.do(bTok, "GET", "/api/v1/search?q=secret", nil, &hits)
	if len(hits) != 0 {
		t.Errorf("bob finds %d notes", len(hits))
	}
	var tags []store.TagCount
	h.do(bTok, "GET", "/api/v1/tags", nil, &tags)
	if len(tags) != 0 {
		t.Errorf("bob sees tags %v", tags)
	}
	var tasks []TaskLine
	h.do(bTok, "GET", "/api/v1/tasks", nil, &tasks)
	if len(tasks) != 0 {
		t.Errorf("bob sees tasks %v", tasks)
	}
	// Bob deletes nothing of Alice's and her trash stays hers.
	h.do(aTok, "DELETE", "/api/v1/notes/"+id, nil, nil)
	var trash []store.TrashItem
	h.do(bTok, "GET", "/api/v1/trash", nil, &trash)
	if len(trash) != 0 {
		t.Errorf("bob sees alice's trash")
	}
	h.do(aTok, "GET", "/api/v1/trash", nil, &trash)
	if len(trash) != 1 {
		t.Fatalf("alice's trash: %d", len(trash))
	}
	if code := h.do(bTok, "POST", "/api/v1/trash/"+itoa(trash[0].ID)+"/restore", nil, nil); code != 404 {
		t.Errorf("bob restores alice's note: %d", code)
	}
	if code := h.do(bTok, "GET", "/api/v1/backup", nil, nil); code != 403 {
		t.Errorf("member backup → %d, want 403", code)
	}
	// Each person's files are in their own folder.
	if code := h.do(bTok, "PUT", "/api/v1/storage", map[string]any{"kind": "folder", "path": h.s.builtinDir(alice.ID), "mode": "switch"}, nil); code != 400 {
		t.Errorf("bob points his storage at alice's folder: %d", code)
	}
}
