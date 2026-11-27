package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/config"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// fakeCapCore is Pocket Core for the capability tests. It answers
// introspection for link tokens (pcx_<scope>_<name> acts for
// <name>@example.com with that one scope, sent by Pocket Hub) and keeps the
// manifest and the events the app sends it.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
	events   []map[string]any
}

var capScopes = map[string]string{
	"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
	"read": "notes:read", "write": "notes:write",
}

func newFakeCapCore(t *testing.T) *fakeCapCore {
	f := &fakeCapCore{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/apps/register":
			f.manifest = body
			_ = json.NewEncoder(w).Encode(map[string]any{"app": map[string]any{}})
		case "/api/v1/events":
			f.events = append(f.events, body)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case "/api/v1/auth/introspect":
			tok, _ := body["token"].(string)
			parts := strings.Split(tok, "_") // pcx, scope, name
			if len(parts) != 3 || capScopes[parts[1]] == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active": true, "kind": "delegated", "scope": capScopes[parts[1]], "audience": "notes", "iss": "https://core.test",
				"exp":  time.Now().Add(time.Hour).Unix(),
				"user": map[string]any{"sub": "sub-" + parts[2], "email": parts[2] + "@example.com", "name": parts[2]},
				"app":  map[string]any{"id": 7, "slug": "hub", "name": "Pocket Hub"},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// event waits for an event the app publishes (the outbox sends it within a second or so).
func (f *fakeCapCore) event(t *testing.T, topic, subject string) map[string]any {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		f.mu.Lock()
		for _, e := range f.events {
			if e["topic"] == topic && e["subject"] == subject {
				f.mu.Unlock()
				p, _ := e["payload"].(map[string]any)
				return p
			}
		}
		f.mu.Unlock()
	}
	t.Fatalf("no %s event for %s", topic, subject)
	return nil
}

// note makes a note through the API, as the web app does.
func (h *harness) note(tok string, in map[string]any) store.Note {
	h.t.Helper()
	var n store.Note
	if code := h.do(tok, "POST", "/api/v1/notes", in, &n); code != 201 {
		h.t.Fatalf("create note %v: %d", in, code)
	}
	return n
}

// dated moves a note in time: when it was made and when it last changed.
func (h *harness) dated(id int64, created, modified time.Time) {
	h.t.Helper()
	if _, err := h.st.DB.Exec(`UPDATE notes SET created_at = ?, modified = ? WHERE id = ?`, created.UTC().Format(time.RFC3339Nano), modified.Unix(), id); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) noteCount(uid int64) int {
	h.t.Helper()
	var n int
	if err := h.st.DB.QueryRow(`SELECT COUNT(1) FROM notes WHERE user_id = ?`, uid).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

// Pocket Search's route is the app's own search: best match first, a plain
// snippet, only the person's notes, and never more than asked for.
func TestPocketSearch(t *testing.T) {
	h := newHarness(t)
	_, aTok := h.user("alice@example.com")
	_, bTok := h.user("bob@example.com")
	bread := h.note(aTok, map[string]any{"title": "Bread recipe", "category": "Recipes", "content": "# Bread recipe\n\n- [ ] **flour** and water #baking\n"})
	h.note(aTok, map[string]any{"title": "Shopping", "content": "milk, eggs and `rye` bread, see [[Bread recipe]] for the weekend"})
	h.note(aTok, map[string]any{"title": "Taxes", "content": "nothing to eat here"})
	h.note(bTok, map[string]any{"title": "Bob's bread", "content": "bread bread bread"})

	var res core.SearchResults
	if code := h.do(aTok, "GET", "/api/v1/pocket/search?q=bread", nil, &res); code != 200 || len(res.Items) != 2 {
		t.Fatalf("search: %d %+v", code, res)
	}
	first := res.Items[0]
	if first.ID != itoa(bread.ID) || first.Title != "Bread recipe" || first.Kind != "note" || first.URL != "/note/"+itoa(bread.ID) {
		t.Errorf("first match: %+v", first)
	}
	if _, err := time.Parse(time.RFC3339, first.Date); err != nil {
		t.Errorf("date %q: %v", first.Date, err)
	}
	if first.Score <= res.Items[1].Score || res.Items[1].Score <= 0 {
		t.Errorf("scores not higher = better: %v then %v", first.Score, res.Items[1].Score)
	}
	for _, it := range res.Items {
		if strings.Contains(it.Title, "Bob") {
			t.Errorf("Bob's note in Alice's search: %+v", it)
		}
		if strings.ContainsAny(it.Snippet, "[]*#`\n") || !strings.Contains(strings.ToLower(it.Snippet), "bread") {
			t.Errorf("snippet not plain: %q", it.Snippet)
		}
	}

	res = core.SearchResults{}
	if h.do(aTok, "GET", "/api/v1/pocket/search?q=bread&limit=1", nil, &res); len(res.Items) != 1 {
		t.Errorf("limit=1 gave %d", len(res.Items))
	}
	// The app's own filters work here too: a tag alone lists its notes.
	res = core.SearchResults{}
	if h.do(aTok, "GET", "/api/v1/pocket/search?q=%23baking", nil, &res); len(res.Items) != 1 || res.Items[0].ID != itoa(bread.ID) || res.Items[0].Snippet == "" {
		t.Errorf("#baking: %+v", res.Items)
	}
	for _, q := range []string{"", "%20", "is:nothing", "zebra"} {
		var raw map[string]any
		if code := h.do(aTok, "GET", "/api/v1/pocket/search?q="+q, nil, &raw); code != 200 {
			t.Errorf("q=%q: %d", q, code)
		}
		if items, ok := raw["items"].([]any); !ok || len(items) != 0 {
			t.Errorf("q=%q: %v", q, raw)
		}
	}
	if code := h.do("", "GET", "/api/v1/pocket/search?q=bread", nil, nil); code != 401 {
		t.Errorf("without a sign-in: %d", code)
	}
}

// Pocket Day's route: what was written and changed on the caller's day, that
// day's daily note, a line that sums it up, and nobody else's notes.
func TestPocketDay(t *testing.T) {
	h := newHarness(t)
	alice, aTok := h.user("alice@example.com")
	_, bTok := h.user("bob@example.com")
	tz, err := time.LoadLocation("Europe/Sarajevo")
	if err != nil {
		t.Skip("no time zone data here")
	}
	at := func(day, hour, min int) time.Time { return time.Date(2026, 3, day, hour, min, 0, 0, tz) }

	written := h.note(aTok, map[string]any{"title": "Written", "category": "Work", "content": "new that day"})
	h.dated(written.ID, at(10, 9, 0), at(10, 9, 30))
	edited := h.note(aTok, map[string]any{"title": "Edited", "content": "older, changed that day"})
	h.dated(edited.ID, at(5, 8, 0), at(10, 14, 5))
	later := h.note(aTok, map[string]any{"title": "Changed since", "content": "made that day, changed two days on"})
	h.dated(later.ID, at(10, 8, 15), at(12, 10, 0))
	night := h.note(aTok, map[string]any{"title": "After midnight", "content": "00:30 in Sarajevo is still the 9th in UTC"})
	h.dated(night.ID, at(10, 0, 30), at(10, 0, 30))
	before := h.note(aTok, map[string]any{"title": "The day before", "content": "not that day"})
	h.dated(before.ID, at(9, 12, 0), at(9, 12, 0))
	bobs := h.note(bTok, map[string]any{"title": "Bob's day", "content": "his"})
	h.dated(bobs.ID, at(10, 10, 0), at(10, 10, 0))
	var daily struct{ Note store.Note }
	if code := h.do(aTok, "POST", "/api/v1/daily", map[string]any{"date": "2026-03-10"}, &daily); code != 200 || daily.Note.ID == 0 {
		t.Fatalf("daily note: %d", code)
	}

	var day core.Day
	if code := h.do(aTok, "GET", "/api/v1/pocket/day/2026-03-10?tz=Europe/Sarajevo", nil, &day); code != 200 {
		t.Fatalf("day: %d", code)
	}
	if day.Date != "2026-03-10" || day.Summary != "3 notes written, 1 edited, daily note" {
		t.Errorf("day %q: %q", day.Date, day.Summary)
	}
	got := map[string]core.DayItem{}
	for _, it := range day.Items {
		got[it.Title] = it
	}
	for title, want := range map[string]core.DayItem{
		"Written":        {Kind: "written", Time: "09:30", Sub: "Work", URL: "/note/" + itoa(written.ID)},
		"Edited":         {Kind: "edited", Time: "14:05", URL: "/note/" + itoa(edited.ID)},
		"Changed since":  {Kind: "written", Time: "08:15", URL: "/note/" + itoa(later.ID)},
		"After midnight": {Kind: "written", Time: "00:30", URL: "/note/" + itoa(night.ID)},
		"2026-03-10":     {Kind: "daily", Time: "", Sub: "Daily", URL: "/note/" + itoa(daily.Note.ID)},
	} {
		it := got[title]
		if it.Kind != want.Kind || it.Time != want.Time || it.Sub != want.Sub || it.URL != want.URL || it.ID == "" {
			t.Errorf("%s: %+v, want %+v", title, it, want)
		}
	}
	if len(day.Items) != 5 {
		t.Errorf("%d items: %+v", len(day.Items), day.Items)
	}

	// The same day counted in UTC: 00:30 in Sarajevo was the evening before there.
	day = core.Day{}
	h.do(aTok, "GET", "/api/v1/pocket/day/2026-03-10?tz=UTC", nil, &day)
	if day.Summary != "2 notes written, 1 edited, daily note" {
		t.Errorf("in UTC: %q", day.Summary)
	}
	day = core.Day{}
	h.do(aTok, "GET", "/api/v1/pocket/day/2026-03-09?tz=UTC", nil, &day)
	if day.Summary != "2 notes written" || len(day.Items) != 2 {
		t.Errorf("the 9th in UTC: %q %+v", day.Summary, day.Items)
	}
	// One note changed, nothing written: the line names the notes once.
	day = core.Day{}
	h.do(aTok, "GET", "/api/v1/pocket/day/2026-03-12?tz=Europe/Sarajevo", nil, &day)
	if day.Summary != "1 note edited" || len(day.Items) != 1 || day.Items[0].Time != "10:00" {
		t.Errorf("the 12th: %q %+v", day.Summary, day.Items)
	}
	// A day with nothing says nothing; the server's own zone when none is given.
	var raw map[string]any
	if code := h.do(aTok, "GET", "/api/v1/pocket/day/2020-01-01", nil, &raw); code != 200 || raw["summary"] != nil || len(raw["items"].([]any)) != 0 {
		t.Errorf("empty day: %d %v", code, raw)
	}
	for _, bad := range []string{"today", "2026-13-40", "20260310"} {
		if code := h.do(aTok, "GET", "/api/v1/pocket/day/"+bad, nil, nil); code != 400 {
			t.Errorf("day %q: %d, want 400", bad, code)
		}
	}
	// Reading a day changes nothing.
	if n := h.noteCount(alice.ID); n != 6 {
		t.Errorf("alice has %d notes after reading days", n)
	}
	day = core.Day{}
	h.do(bTok, "GET", "/api/v1/pocket/day/2026-03-10?tz=Europe/Sarajevo", nil, &day)
	if day.Summary != "1 note written" || len(day.Items) != 1 || day.Items[0].Title != "Bob's day" {
		t.Errorf("bob's day: %q %+v", day.Summary, day.Items)
	}
}

// Pocket Hub's widget: the notes changed last, pinned ones first, a line
// about today, and nobody else's notes.
func TestPocketWidget(t *testing.T) {
	h := newHarness(t)
	_, aTok := h.user("alice@example.com")
	_, bTok := h.user("bob@example.com")
	_, cTok := h.user("carol@example.com")
	now := time.Now()
	var pins []store.Note
	for _, title := range []string{"Pin one", "Pin two", "Pin three", "Pin four"} {
		n := h.note(aTok, map[string]any{"title": title, "category": "Home/Plans", "content": "kept on top", "pinned": true})
		h.dated(n.ID, now.AddDate(0, 0, -40), now.AddDate(0, 0, -30-len(pins)))
		pins = append(pins, n)
	}
	h.note(aTok, map[string]any{"title": "Old", "content": "a while ago"})
	old := h.note(aTok, map[string]any{"title": "Yesterday's", "content": "from yesterday"})
	h.dated(old.ID, now.AddDate(0, 0, -1), now.AddDate(0, 0, -1))
	fresh := h.note(aTok, map[string]any{"title": "Fresh", "category": "Inbox", "content": "just now"})
	h.dated(fresh.ID, now, now.Add(time.Minute))
	h.note(bTok, map[string]any{"title": "Bob's own", "content": "his"})

	var wd core.WidgetData
	if code := h.do(aTok, "GET", "/api/v1/pocket/widget", nil, &wd); code != 200 {
		t.Fatalf("widget: %d", code)
	}
	if wd.URL != "/" || wd.Line != "2 notes changed today" || len(wd.Items) != 6 {
		t.Fatalf("widget: %q %q %d items %+v", wd.URL, wd.Line, len(wd.Items), wd.Items)
	}
	titles := make([]string, len(wd.Items))
	for i, it := range wd.Items {
		titles[i] = it.Title
		if it.ID == "" || it.URL != "/note/"+it.ID || it.Detail == "" {
			t.Errorf("row %+v", it)
		}
	}
	if got := strings.Join(titles, ", "); got != "Pin one, Pin two, Pin three, Fresh, Old, Yesterday's" {
		t.Errorf("rows: %s", got)
	}
	if wd.Items[0].Detail != now.AddDate(0, 0, -30).Format("2 Jan")+" · Plans" && wd.Items[0].Detail != now.AddDate(0, 0, -30).Format("2 Jan 2006")+" · Plans" {
		t.Errorf("pinned detail %q", wd.Items[0].Detail)
	}
	if wd.Items[3].Detail != "today · Inbox" || wd.Items[4].Detail != "today" || wd.Items[5].Detail != "yesterday" {
		t.Errorf("details: %q, %q, %q", wd.Items[3].Detail, wd.Items[4].Detail, wd.Items[5].Detail)
	}

	wd = core.WidgetData{}
	if h.do(bTok, "GET", "/api/v1/pocket/widget", nil, &wd); len(wd.Items) != 1 || wd.Items[0].Title != "Bob's own" || wd.Line != "1 note changed today" {
		t.Errorf("bob's widget: %q %+v", wd.Line, wd.Items)
	}
	var raw map[string]any
	if code := h.do(cTok, "GET", "/api/v1/pocket/widget", nil, &raw); code != 200 || raw["line"] != nil || len(raw["items"].([]any)) != 0 {
		t.Errorf("widget with no notes: %d %v", code, raw)
	}
}

func TestDayWord(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC) // a Friday
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-9 * time.Hour), "today"},
		{now.Add(time.Hour), "today"},
		{now.Add(-11 * time.Hour), "yesterday"},
		{now.AddDate(0, 0, -3), "Tuesday"},
		{now.AddDate(0, 0, -7), "20 Nov"},
		{now.AddDate(-1, 0, 0), "27 Nov 2025"},
	} {
		if got := dayWord(c.t, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.t.Format(time.RFC3339), got, c.want)
		}
	}
}

func TestDaySummary(t *testing.T) {
	for _, c := range []struct {
		written, edited int
		daily           bool
		want            string
	}{
		{0, 0, false, ""}, {1, 0, false, "1 note written"}, {2, 1, false, "2 notes written, 1 edited"},
		{0, 1, false, "1 note edited"}, {0, 3, false, "3 notes edited"}, {0, 0, true, "Daily note"},
		{1, 0, true, "1 note written, daily note"}, {0, 2, true, "2 notes edited, daily note"},
	} {
		if got := daySummary(c.written, c.edited, c.daily); got != c.want {
			t.Errorf("%d written, %d edited, daily %v: %q, want %q", c.written, c.edited, c.daily, got, c.want)
		}
	}
}

// A quick line becomes one new note where the person's new notes go, named
// after its first line or the title sent with it; the same request again
// answers the same note.
func TestPocketCapture(t *testing.T) {
	h := newHarness(t)
	alice, aTok := h.user("alice@example.com")
	bob, bTok := h.user("bob@example.com")
	if err := h.st.SetUserSetting(context.Background(), alice.ID, "default_folder", "Quick Notes"); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/pocket/capture"

	var res, again core.CaptureResult
	line := map[string]any{"request_id": "req-0000000000000001", "kind": "note", "text": "Milk, eggs\nand bread"}
	if code := h.do(aTok, "POST", path, line, &res); code != 201 || res.ID == "" || res.Title != "Milk, eggs" || res.URL != "/note/"+res.ID {
		t.Fatalf("capture: %d %+v", code, res)
	}
	var n store.Note
	if code := h.do(aTok, "GET", "/api/v1/notes/"+res.ID, nil, &n); code != 200 || n.Category != "Quick Notes" || n.Content != "Milk, eggs\nand bread" || n.Path != "Quick Notes/Milk, eggs.md" {
		t.Errorf("the note: %d %q in %q at %q", code, n.Content, n.Category, n.Path)
	}
	var versions []store.Version
	if h.do(aTok, "GET", "/api/v1/notes/"+res.ID+"/versions", nil, &versions); len(versions) != 1 || versions[0].Source != "capture" {
		t.Errorf("history: %+v", versions)
	}
	// Sent again (the answer was lost on the way): the same note, nothing new.
	if code := h.do(aTok, "POST", path, line, &again); code != 201 || again != res {
		t.Errorf("same request again: %d %+v, want %+v", code, again, res)
	}
	if got := h.noteCount(alice.ID); got != 1 {
		t.Errorf("alice has %d notes after the same request twice", got)
	}
	// The id is the person's own: Bob using the same one gets his own note.
	var bobs core.CaptureResult
	if code := h.do(bTok, "POST", path, line, &bobs); code != 201 || bobs.ID == res.ID {
		t.Errorf("bob with the same request id: %d %+v", code, bobs)
	}
	if code := h.do(bTok, "GET", "/api/v1/notes/"+res.ID, nil, nil); code != 404 {
		t.Errorf("bob reads alice's captured note: %d", code)
	}
	if a, b := h.noteCount(alice.ID), h.noteCount(bob.ID); a != 1 || b != 1 {
		t.Errorf("notes: alice %d, bob %d", a, b)
	}

	// With a title: the note carries it as its name and its first line. No kind = the first one.
	var titled core.CaptureResult
	if code := h.do(aTok, "POST", path, map[string]any{"request_id": "req-0000000000000002", "title": "  Shopping  list ", "text": "Milk, eggs"}, &titled, "X-Pocket-Source", "shortcut"); code != 201 || titled.Title != "Shopping list" {
		t.Fatalf("with a title: %d %+v", code, titled)
	}
	n = store.Note{}
	if h.do(aTok, "GET", "/api/v1/notes/"+titled.ID, nil, &n); n.Title != "Shopping list" || n.Content != "# Shopping list\n\nMilk, eggs\n" || n.Category != "Quick Notes" {
		t.Errorf("titled note: %q %q in %q", n.Title, n.Content, n.Category)
	}
	versions = nil
	if h.do(aTok, "GET", "/api/v1/notes/"+titled.ID+"/versions", nil, &versions); len(versions) != 1 || versions[0].Source != "shortcut" {
		t.Errorf("history of the titled note: %+v", versions)
	}
	var only core.CaptureResult
	h.do(aTok, "POST", path, map[string]any{"request_id": "req-0000000000000003", "title": "Call the plumber"}, &only)
	n = store.Note{}
	if h.do(aTok, "GET", "/api/v1/notes/"+only.ID, nil, &n); n.Title != "Call the plumber" || n.Content != "# Call the plumber\n" {
		t.Errorf("title only: %q %q", n.Title, n.Content)
	}

	before := h.noteCount(alice.ID)
	for name, body := range map[string]map[string]any{
		"nothing to keep":            {"request_id": "req-0000000000000004", "kind": "note", "title": " ", "text": "\n "},
		"no request id":              {"kind": "note", "text": "x"},
		"a short request id":         {"request_id": "short", "text": "x"},
		"a kind the app has not got": {"request_id": "req-0000000000000005", "kind": "task", "text": "x"},
		"a bad date":                 {"request_id": "req-0000000000000006", "text": "x", "date": "tomorrow"},
		"a used id, other words":     {"request_id": "req-0000000000000001", "kind": "note", "text": "Something else"},
	} {
		var e apiError
		if code := h.do(aTok, "POST", path, body, &e); code != 400 || e.Error == "" {
			t.Errorf("%s: %d %q, want 400", name, code, e.Error)
		}
	}
	if code := h.do(aTok, "POST", path, "not json", nil); code != 400 {
		t.Errorf("not json: %d", code)
	}
	if got := h.noteCount(alice.ID); got != before {
		t.Errorf("refused captures made %d notes", got-before)
	}
}

// Two of the same request at once still make one note.
func TestPocketCaptureAtOnce(t *testing.T) {
	h := newHarness(t)
	alice, aTok := h.user("alice@example.com")
	line := map[string]any{"request_id": "req-at-once-00000001", "text": "Only once"}
	ids := make([]string, 6)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := json.Marshal(line)
			req, _ := http.NewRequest("POST", h.ts.URL+"/api/v1/pocket/capture", strings.NewReader(string(b)))
			req.Header.Set("Authorization", "Bearer "+aTok)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer res.Body.Close()
			var out core.CaptureResult
			_ = json.NewDecoder(res.Body).Decode(&out)
			ids[i] = out.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("answers differ: %v", ids)
		}
	}
	if got := h.noteCount(alice.ID); got != 1 {
		t.Errorf("%d notes from one request sent six times", got)
	}
}

// A link token with one of the pocket:* scopes reaches exactly its route (and
// who it acts for); the app's own API stays closed to it, and notes:read does
// not open these routes either.
func TestPocketLinkScopes(t *testing.T) {
	coreSrv := newFakeCapCore(t)
	h := newHarnessWith(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
	alice, aTok := h.user("alice@example.com")
	bob, _ := h.user("bob@example.com")
	h.note(aTok, map[string]any{"title": "Bread recipe", "content": "flour"})

	line := func(id string) map[string]any {
		return map[string]any{"request_id": id, "kind": "note", "text": "From the hub " + id[len(id)-1:]}
	}
	routes := []struct {
		scope, method, path string
		body                any
		want                int
	}{
		{"search", "GET", "/api/v1/pocket/search?q=bread", nil, 200},
		{"day", "GET", "/api/v1/pocket/day/" + time.Now().Format("2006-01-02"), nil, 200},
		{"widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"capture", "POST", "/api/v1/pocket/capture", line("req-link-000000000001"), 201},
	}
	for _, tok := range []string{"search", "day", "widget", "capture", "read", "write"} {
		for _, rt := range routes {
			want := http.StatusForbidden
			if tok == rt.scope {
				want = rt.want
			}
			if code := h.do("pcx_"+tok+"_alice", rt.method, rt.path, rt.body, nil); code != want {
				t.Errorf("%s %s with %s: %d, want %d", rt.method, rt.path, capScopes[tok], code, want)
			}
		}
	}
	for _, tok := range []string{"search", "day", "widget", "capture"} {
		for _, c := range []struct{ method, path string }{
			{"GET", "/api/v1/notes"}, {"POST", "/api/v1/notes"}, {"GET", "/api/v1/search?q=bread"}, {"POST", "/api/v1/capture"},
			{"GET", "/api/v1/dashboard"}, {"GET", "/api/v1/daily"}, {"GET", "/api/v1/settings"}, {"GET", "/api/v1/export.zip"},
			{"GET", "/api/v1/pocket/portable"}, {"GET", "/api/v1/pocket/day/today"},
		} {
			if code := h.do("pcx_"+tok+"_alice", c.method, c.path, map[string]any{}, nil); code != http.StatusForbidden {
				t.Errorf("%s %s with %s: %d, want 403", c.method, c.path, capScopes[tok], code)
			}
		}
		var who struct{ Email string }
		if code := h.do("pcx_"+tok+"_alice", "GET", "/api/v1/auth/me", nil, &who); code != 200 || who.Email != "alice@example.com" {
			t.Errorf("auth/me with %s: %d %q", capScopes[tok], code, who.Email)
		}
	}

	// The link acts for one person: Alice's notes, not Bob's.
	var res core.SearchResults
	if h.do("pcx_search_alice", "GET", "/api/v1/pocket/search?q=bread", nil, &res); len(res.Items) != 1 {
		t.Errorf("alice through the link finds %d", len(res.Items))
	}
	res = core.SearchResults{}
	if h.do("pcx_search_bob", "GET", "/api/v1/pocket/search?q=bread", nil, &res); len(res.Items) != 0 {
		t.Errorf("bob through the link finds %d", len(res.Items))
	}

	// A line kept through the link: one note however often it is sent, Alice's,
	// with the app it came from in its history and in the event, like a note
	// made in the app.
	var kept, again core.CaptureResult
	h.do("pcx_capture_alice", "POST", "/api/v1/pocket/capture", line("req-link-000000000002"), &kept)
	if code := h.do("pcx_capture_alice", "POST", "/api/v1/pocket/capture", line("req-link-000000000002"), &again); code != 201 || kept.ID == "" || again != kept {
		t.Fatalf("capture through the link twice: %d %+v then %+v", code, kept, again)
	}
	if a, b := h.noteCount(alice.ID), h.noteCount(bob.ID); a != 3 || b != 0 {
		t.Errorf("notes: alice %d (want 3), bob %d", a, b)
	}
	var versions []store.Version
	if h.do(aTok, "GET", "/api/v1/notes/"+kept.ID+"/versions", nil, &versions); len(versions) != 1 || versions[0].Source != "Pocket Hub" {
		t.Errorf("history: %+v", versions)
	}
	ev := coreSrv.event(t, "notes.created", kept.ID)
	if ev["source"] != "Pocket Hub" || ev["user_email"] != "alice@example.com" || ev["title"] != "From the hub 2" {
		t.Errorf("event: %v", ev)
	}
	// The same id sent by the person themself is another request: ids are kept per app.
	var own core.CaptureResult
	if h.do(aTok, "POST", "/api/v1/pocket/capture", line("req-link-000000000002"), &own); own.ID == "" || own.ID == kept.ID {
		t.Errorf("the person's own capture with an app's request id: %+v", own)
	}
}

// What Core is told: the four capabilities, their offers next to the app's
// own, and the shelf the app sits on.
func TestManifestCapabilities(t *testing.T) {
	coreSrv := newFakeCapCore(t)
	h := newHarnessWith(t, func(c *config.Config) {
		c.CoreURL, c.CoreRegistrationKey, c.CoreAppURL = coreSrv.URL, "pcr_test", "http://notes:8080"
	})
	if err := h.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(coreSrv.manifest)
	var m core.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Slug != "notes" || m.Category != "Everyday" {
		t.Errorf("manifest %q on shelf %q", m.Slug, m.Category)
	}
	if m.Search == nil || len(m.Search.Bangs) != 1 || m.Search.Bangs[0] != "notes" || len(m.Search.Kinds) != 1 || m.Search.Kinds[0] != "note" || m.Search.What == "" {
		t.Errorf("search: %+v", m.Search)
	}
	if m.Day == nil || m.Day.Label != "Notes" || m.Day.One != "note" || m.Day.Many != "notes" || m.Day.Unit != "count" || m.Day.What == "" {
		t.Errorf("day: %+v", m.Day)
	}
	if m.Widget == nil || m.Widget.Title != "Recent notes" || m.Widget.What == "" {
		t.Errorf("widget: %+v", m.Widget)
	}
	if m.Capture == nil || len(m.Capture.Kinds) != 1 || m.Capture.Kinds[0].Kind != "note" || len(m.Capture.Kinds[0].Fields) != 1 || m.Capture.Kinds[0].Fields[0] != "title" {
		t.Errorf("capture: %+v", m.Capture)
	}
	offers := map[string]bool{}
	for _, o := range m.Offers {
		if o.Name == "" || o.Description == "" {
			t.Errorf("offer %q has no words", o.Scope)
		}
		offers[o.Scope] = true
	}
	for _, scope := range []string{core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture, "notes:read", "notes:write", core.ScopeMoveExport, core.ScopeMoveImport} {
		if !offers[scope] {
			t.Errorf("offer %s is missing", scope)
		}
	}
}
