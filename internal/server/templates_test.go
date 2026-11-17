package server

import (
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

func TestFillPlaceholders(t *testing.T) {
	at := time.Date(2026, 11, 4, 9, 5, 0, 0, time.UTC)
	st := store.Settings{DateFormat: "iso"}
	for in, want := range map[string]string{
		"{{date}} Meeting":               "2026-11-04 Meeting",
		"{{date:YYMMDD}} Ticket":         "261104 Ticket",
		"{{ date : DD.MM.YYYY }}":        "04.11.2026",
		"{{date:YYYY-[W]MM}}":            "2026-W11",
		"{{tomorrow:YYMMDD}}":            "261105",
		"{{time}} / {{time:HHmm}}":       "09:05 / 0905",
		"{{weekday}} {{month}} {{year}}": "Wednesday November 2026",
		"week {{week}}":                  "week 45",
		"# {{title}} from {{name}}":      "# Call 1 from Customer call",
		"{{unknown}} {{date:ddd D MMM}}": "{{unknown}} Wed 4 Nov",
	} {
		if got := fillPlaceholders(in, "Call 1", "Customer call", at, st); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	if got := fillPlaceholders("{{date}}", "", "", at, store.Settings{DateFormat: "yymmdd"}); got != "261104" {
		t.Errorf("yymmdd date format → %q", got)
	}
}

// Save a note as a template, make a note from it in one step, change and
// delete it; share it with everyone and someone else uses it.
func TestTemplates(t *testing.T) {
	h := newHarness(t)
	_, aTok := h.user("alice@example.com")
	_, bTok := h.user("bob@example.com")
	var src store.Note
	h.do(aTok, "POST", "/api/v1/notes", map[string]any{"title": "Weekly sync", "category": "Work", "content": "# Weekly sync\n\n**When:** {{date:DD.MM.}}\n\n- [ ] \n"}, &src)

	var tpl templateOut
	if code := h.do(aTok, "POST", "/api/v1/templates", map[string]any{"from_note_id": src.ID, "name": "Meeting", "title_pattern": "{{date:YYMMDD}} {{name}}", "folder": "Meetings"}, &tpl); code != 201 {
		t.Fatalf("save as template → %d", code)
	}
	if tpl.Category != "Templates" || tpl.Name != "Meeting" || tpl.Folder != "Meetings" || tpl.Everyone {
		t.Fatalf("template: %+v", tpl)
	}
	want := time.Now().Format("060102") + " Meeting"
	if tpl.NextTitle != want {
		t.Errorf("next title %q, want %q", tpl.NextTitle, want)
	}
	var full store.Note
	h.do(aTok, "GET", "/api/v1/notes/"+itoa(tpl.ID), nil, &full)
	if !strings.HasPrefix(full.Content, "# {{title}}\n") {
		t.Errorf("the heading should become {{title}}: %q", full.Content)
	}

	// One step: a note in the template's folder, titled and filled in.
	var n store.Note
	if code := h.do(aTok, "POST", "/api/v1/templates/"+itoa(tpl.ID)+"/use", nil, &n); code != 201 {
		t.Fatalf("use → %d", code)
	}
	h.do(aTok, "GET", "/api/v1/notes/"+itoa(n.ID), nil, &full)
	if n.Title != want || n.Category != "Meetings" || !strings.HasPrefix(full.Content, "# "+want+"\n") || strings.Contains(full.Content, "{{") || !strings.Contains(full.Content, time.Now().Format("02.01.")) {
		t.Fatalf("new note %q in %q: %q", n.Title, n.Category, full.Content)
	}
	// A title of their own, and "where I am" only counts when the template names no folder.
	h.do(aTok, "POST", "/api/v1/templates/"+itoa(tpl.ID)+"/use", map[string]any{"title": "Kickoff", "here": "Elsewhere"}, &n)
	if n.Title != "Kickoff" || n.Category != "Meetings" {
		t.Errorf("own title: %q in %q", n.Title, n.Category)
	}

	// Change it: name, title, folder.
	var upd templateOut
	if code := h.do(aTok, "PUT", "/api/v1/templates/"+itoa(tpl.ID), map[string]any{"name": "Team meeting", "title_pattern": "", "folder": ""}, &upd); code != 200 {
		t.Fatalf("update → %d", code)
	}
	if upd.Name != "Team meeting" || upd.NextTitle != "Team meeting" || upd.Folder != "" {
		t.Errorf("updated: %+v", upd)
	}
	h.do(aTok, "POST", "/api/v1/templates/"+itoa(tpl.ID)+"/use", map[string]any{"here": "Projects/X"}, &n)
	if n.Category != "Projects/X" {
		t.Errorf("here: %q", n.Category)
	}

	// Bob neither sees nor uses it until it is shared with everyone.
	var list []templateOut
	h.do(bTok, "GET", "/api/v1/templates", nil, &list)
	if len(list) != 0 {
		t.Fatalf("bob sees %d templates", len(list))
	}
	if code := h.do(bTok, "POST", "/api/v1/templates/"+itoa(tpl.ID)+"/use", nil, nil); code != 404 {
		t.Errorf("bob uses a private template → %d", code)
	}
	h.do(aTok, "PUT", "/api/v1/templates/"+itoa(tpl.ID), map[string]any{"everyone": true}, nil)
	h.do(bTok, "GET", "/api/v1/templates", nil, &list)
	if len(list) != 1 || list[0].Owner == nil || list[0].Owner.Email != "alice@example.com" {
		t.Fatalf("bob's list: %+v", list)
	}
	if code := h.do(bTok, "POST", "/api/v1/templates/"+itoa(tpl.ID)+"/use", nil, &n); code != 201 || n.Title != "Team meeting" {
		t.Fatalf("bob uses it → %d %q", code, n.Title)
	}
	if owner, _ := h.st.NoteOwner(t.Context(), n.ID); owner == 0 || owner == list[0].Owner.OwnerID {
		t.Errorf("bob's note belongs to %d", owner)
	}
	for _, c := range []struct{ method, path string }{{"PUT", "/api/v1/templates/" + itoa(tpl.ID)}, {"DELETE", "/api/v1/templates/" + itoa(tpl.ID)}} {
		if code := h.do(bTok, c.method, c.path, map[string]any{"name": "x"}, nil); code != 403 {
			t.Errorf("bob %s → %d", c.method, code)
		}
	}
	// Bob's own copy is his, not shared.
	var copy templateOut
	if code := h.do(bTok, "POST", "/api/v1/templates", map[string]any{"from_note_id": tpl.ID}, &copy); code != 201 || copy.Name != "Team meeting" || copy.Everyone {
		t.Fatalf("copy → %d %+v", code, copy)
	}

	// A plain note is not a template; a blank one can be made by name.
	if code := h.do(aTok, "POST", "/api/v1/templates/"+itoa(src.ID)+"/use", nil, nil); code != 404 {
		t.Errorf("a plain note used as a template → %d", code)
	}
	if code := h.do(aTok, "POST", "/api/v1/templates", map[string]any{"name": " "}, nil); code != 400 {
		t.Errorf("no name → %d", code)
	}

	// Delete: to the trash, out of everyone's list.
	if code := h.do(aTok, "DELETE", "/api/v1/templates/"+itoa(tpl.ID), nil, nil); code != 204 {
		t.Fatalf("delete → %d", code)
	}
	h.do(bTok, "GET", "/api/v1/templates", nil, &list)
	if len(list) != 1 || list[0].ID != copy.ID {
		t.Errorf("after delete bob sees %+v", list)
	}
	var trash []store.TrashItem
	h.do(aTok, "GET", "/api/v1/trash", nil, &trash)
	if len(trash) != 1 || trash[0].Title != "Team meeting" {
		t.Errorf("trash: %+v", trash)
	}

	// A template without settings takes its title from a heading with placeholders.
	var old store.Note
	h.do(aTok, "POST", "/api/v1/notes", map[string]any{"title": "Standup", "category": "Templates", "content": "# Standup, {{date:YYMMDD}}\n\n- "}, &old)
	h.do(aTok, "GET", "/api/v1/templates?mine=1", nil, &list)
	if len(list) != 1 || list[0].TitlePattern != "Standup, {{date:YYMMDD}}" || list[0].NextTitle != "Standup, "+time.Now().Format("060102") {
		t.Errorf("heading as title: %+v", list)
	}
	h.do(aTok, "POST", "/api/v1/templates/"+itoa(old.ID)+"/use", nil, &n)
	if n.Title != "Standup, "+time.Now().Format("060102") {
		t.Errorf("from the heading: %q", n.Title)
	}

	var p map[string]string
	h.do(aTok, "POST", "/api/v1/templates/preview", map[string]any{"pattern": "{{date:YYMMDD}} {{name}}", "name": "Call"}, &p)
	if p["title"] != time.Now().Format("060102")+" Call" {
		t.Errorf("preview %q", p["title"])
	}
}
