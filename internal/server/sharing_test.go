package server

import (
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// A note shared with someone: they find it under Shared, read it, edit it
// only when allowed, and reach nothing else of the owner's.
func TestShareWithPerson(t *testing.T) {
	h := newHarness(t)
	_, aTok := h.user("alice@example.com")
	bob, bTok := h.user("bob@example.com")
	_, cTok := h.user("carol@example.com")
	var n, other store.Note
	h.do(aTok, "POST", "/api/v1/notes", map[string]any{"title": "Trip", "category": "Plans", "content": "# Trip\n- [ ] book"}, &n)
	h.do(aTok, "POST", "/api/v1/notes", map[string]any{"title": "Diary", "category": "Private", "content": "secret"}, &other)
	id := itoa(n.ID)

	var people []map[string]any
	h.do(aTok, "GET", "/api/v1/people", nil, &people)
	if len(people) != 2 {
		t.Fatalf("people: %v", people)
	}
	if code := h.do(aTok, "POST", "/api/v1/notes/"+id+"/members", map[string]any{"email": "nobody@example.com"}, nil); code != 404 {
		t.Errorf("unknown person → %d", code)
	}
	if code := h.do(aTok, "POST", "/api/v1/notes/"+id+"/members", map[string]any{"email": "bob@example.com"}, nil); code != 200 {
		t.Fatalf("share → %d", code)
	}

	// Bob reads it, read only.
	var got store.Note
	if code := h.do(bTok, "GET", "/api/v1/notes/"+id, nil, &got); code != 200 {
		t.Fatalf("bob get → %d", code)
	}
	if got.SharedFrom == nil || got.SharedFrom.Email != "alice@example.com" || got.SharedFrom.CanEdit || !got.Readonly {
		t.Fatalf("bob sees %+v", got.SharedFrom)
	}
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/notes/" + id, `{"content":"x"}`}, {"POST", "/api/v1/notes/" + id + "/append", `{"text":"x"}`},
		{"POST", "/api/v1/notes/" + id + "/tasks", `{"line":2,"done":true}`}, {"DELETE", "/api/v1/notes/" + id, ""},
		{"POST", "/api/v1/notes/" + id + "/members", `{"email":"carol@example.com"}`},
		{"DELETE", "/api/v1/notes/" + id + "/members/1", ""},
	} {
		var body any
		if c.body != "" {
			body = c.body
		}
		if code := h.do(bTok, c.method, c.path, body, nil); code != 403 {
			t.Errorf("read-only bob %s %s → %d, want 403", c.method, c.path, code)
		}
	}
	// Nothing else of Alice's, and nothing of it for Carol.
	if code := h.do(bTok, "GET", "/api/v1/notes/"+itoa(other.ID), nil, nil); code != 404 {
		t.Errorf("bob reads alice's diary: %d", code)
	}
	if code := h.do(bTok, "GET", "/api/v1/notes/"+id+"/attachment?path="+"../Private/Diary.md", nil, nil); code != 404 {
		t.Errorf("bob reads a file the note does not link: %d", code)
	}
	if code := h.do(cTok, "GET", "/api/v1/notes/"+id, nil, nil); code != 404 {
		t.Errorf("carol reads it: %d", code)
	}

	var shared struct {
		WithMe []store.Note `json:"with_me"`
		ByMe   []store.Note `json:"by_me"`
	}
	h.do(bTok, "GET", "/api/v1/shared", nil, &shared)
	if len(shared.WithMe) != 1 || shared.WithMe[0].ID != n.ID || len(shared.ByMe) != 0 {
		t.Fatalf("bob's shared: %+v", shared)
	}
	h.do(aTok, "GET", "/api/v1/shared", nil, &shared)
	if len(shared.ByMe) != 1 || shared.ByMe[0].Shared != 1 || len(shared.WithMe) != 0 {
		t.Fatalf("alice's shared: %+v", shared)
	}
	var list []store.Note
	h.do(aTok, "GET", "/api/v1/notes", nil, &list)
	for _, x := range list {
		if (x.ID == n.ID) != (x.Shared == 1) {
			t.Errorf("list marks %q shared=%d", x.Title, x.Shared)
		}
	}

	// Allowed to edit: his save lands in Alice's note, folder untouched.
	h.do(aTok, "POST", "/api/v1/notes/"+id+"/members", map[string]any{"user_id": bob.ID, "can_edit": true}, nil)
	var saved store.Note
	if code := h.do(bTok, "PUT", "/api/v1/notes/"+id, map[string]any{"content": "# Trip\n- [ ] book\n- [ ] pack", "category": "Mine"}, &saved); code != 200 {
		t.Fatalf("bob edit → %d", code)
	}
	if code := h.do(bTok, "POST", "/api/v1/notes/"+id+"/tasks", map[string]any{"line": 1, "done": true}, nil); code != 200 {
		t.Errorf("bob ticks → %d", code)
	}
	got = store.Note{}
	h.do(aTok, "GET", "/api/v1/notes/"+id, nil, &got)
	if got.Category != "Plans" || !strings.Contains(got.Content, "- [ ] pack") || !strings.Contains(got.Content, "- [x] book") || got.SharedFrom != nil || got.Shared != 1 {
		t.Fatalf("alice's note after bob: %q in %q shared=%d from=%+v", got.Content, got.Category, got.Shared, got.SharedFrom)
	}
	var versions []store.Version
	h.do(aTok, "GET", "/api/v1/notes/"+id+"/versions", nil, &versions)
	if len(versions) == 0 || !strings.HasPrefix(versions[0].Source, "by ") {
		t.Errorf("versions: %+v", versions)
	}

	// A copy for himself, then he leaves; Alice keeps her note.
	var copy store.Note
	if code := h.do(bTok, "POST", "/api/v1/notes/"+id+"/duplicate", nil, &copy); code != 201 {
		t.Fatalf("bob copy → %d", code)
	}
	got = store.Note{}
	if code := h.do(bTok, "GET", "/api/v1/notes/"+itoa(copy.ID), nil, &got); code != 200 || got.SharedFrom != nil {
		t.Errorf("bob's copy: %d %+v", code, got.SharedFrom)
	}
	if code := h.do(bTok, "DELETE", "/api/v1/notes/"+id+"/members/"+itoa(bob.ID), nil, nil); code != 204 {
		t.Fatalf("bob leaves → %d", code)
	}
	if code := h.do(bTok, "GET", "/api/v1/notes/"+id, nil, nil); code != 404 {
		t.Errorf("bob still reads it: %d", code)
	}
	if code := h.do(aTok, "GET", "/api/v1/notes/"+id, nil, nil); code != 200 {
		t.Errorf("alice lost it: %d", code)
	}
}
