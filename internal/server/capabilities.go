package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/notesmd"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask every app (capabilities
// in links.go; docs/capabilities.md in pocket-homelab-core). Each runs as the
// request's person, like the handlers whose queries it reuses. The three that
// read change nothing, not even the index: they do not look at the storage
// first, the sync keeps the index fresh.

func noteURL(id int64) string { return "/note/" + strconv.FormatInt(id, 10) }

func noteTitle(n *store.Note) string { return firstNonEmpty(n.Title, "Untitled") }

// countOf writes "1 note" or "3 notes".
func countOf(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + one + "s"
}

// ---- search ----

// snippetMarks are what the search puts around the words it found;
// noteMarks what is left of a note's own writing after Markdown is taken off:
// the brackets of a [[link]] and the ticks around code.
var (
	snippetMarks = strings.NewReplacer("[[[", "", "]]]", "")
	noteMarks    = strings.NewReplacer("[[", "", "]]", "", "`", "")
)

// pocketSearch is the app's own search (GET /search): the same words, the
// same filters ("#tag", "in:Folder", "is:pinned" …), the same order.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx, uid := r.Context(), me(r)
	words, f := parseQuery(q)
	var hits []store.SearchHit
	// What the search found comes as a piece of the note's Markdown with marks
	// around the words; an excerpt is plain already.
	snippet := func(h store.SearchHit) string { return notesmd.Plain(snippetMarks.Replace(h.Snippet)) }
	if strings.TrimSpace(words) == "" {
		// Only filters: those notes, newest first. Nothing to filter by finds nothing.
		if f.Tag == "" && f.Category == nil && !f.Favorite && !f.Pinned && !f.HasTasks {
			return nil, nil
		}
		f.Limit = limit
		snippet = func(h store.SearchHit) string { return h.Snippet }
		notes, err := s.store.ListNotes(ctx, uid, f)
		if err != nil {
			return nil, err
		}
		for _, n := range notes {
			hits = append(hits, store.SearchHit{Note: n, Snippet: n.Excerpt})
		}
	} else {
		var err error
		if hits, err = s.store.Search(ctx, uid, words, f, limit); err != nil {
			return nil, err
		}
	}
	items := make([]core.SearchItem, 0, len(hits))
	for _, h := range hits {
		items = append(items, core.SearchItem{
			ID: strconv.FormatInt(h.ID, 10), Title: noteTitle(&h.Note), Kind: "note",
			Snippet: noteMarks.Replace(snippet(h)),
			Date:    time.Unix(h.Modified, 0).UTC().Format(time.RFC3339),
			URL:     noteURL(h.ID),
			// The rank is smaller for a better match (bm25): turned round, higher is better.
			Score: -h.Rank,
		})
	}
	return items, nil
}

// ---- day ----

// dayNotes is how many notes of a day are read at most (what the answer may hold).
const dayNotes = 200

// pocketDay answers the notes written or changed on a day and that day's
// daily note. The day is the caller's (tz), else this server's, as the Today
// page counts days.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx, uid := r.Context(), me(r)
	if tz == nil {
		tz = time.Local
	}
	start, err := time.ParseInLocation("2006-01-02", date, tz)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 0, 1)
	within := func(t time.Time) bool { return !t.Before(start) && t.Before(end) }
	clock := func(t time.Time) string { return t.In(tz).Format("15:04") }

	daily, _, err := s.dailyNote(ctx, uid, start, false)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	changed, made, err := s.store.NotesOfDay(ctx, uid, start, end, dayNotes)
	if err != nil {
		return nil, err
	}

	day := &core.Day{Date: date}
	item := func(n *store.Note, kind, at string) {
		day.Items = append(day.Items, core.DayItem{ID: strconv.FormatInt(n.ID, 10), Time: at, Title: noteTitle(n), Sub: n.Category, Kind: kind, URL: noteURL(n.ID)})
	}
	if daily != nil {
		// Written on another day (tomorrow's plan, yesterday's catch-up) it has no time here.
		at := ""
		if t := time.Unix(daily.Modified, 0); within(t) {
			at = clock(t)
		}
		item(daily, "daily", at)
	}
	written, edited := 0, 0
	for i := range changed {
		n := &changed[i]
		if daily != nil && n.ID == daily.ID {
			continue
		}
		kind := "edited"
		if within(store.ParseTime(n.CreatedAt)) {
			kind = "written"
			written++
		} else {
			edited++
		}
		item(n, kind, clock(time.Unix(n.Modified, 0)))
	}
	// Written that day and changed again since: the day still has them, at the time they were made.
	for i := range made {
		n := &made[i]
		if daily != nil && n.ID == daily.ID {
			continue
		}
		written++
		item(n, "written", clock(store.ParseTime(n.CreatedAt)))
	}
	if len(day.Items) == 0 {
		return nil, nil
	}
	day.Summary = daySummary(written, edited, daily != nil)
	return day, nil
}

// daySummary says a day of notes in a line: "2 notes written, 1 edited, daily note".
func daySummary(written, edited int, daily bool) string {
	var parts []string
	if written > 0 {
		parts = append(parts, countOf(written, "note")+" written")
	}
	switch {
	case edited > 0 && written > 0:
		parts = append(parts, strconv.Itoa(edited)+" edited")
	case edited > 0:
		parts = append(parts, countOf(edited, "note")+" edited")
	}
	switch {
	case daily && len(parts) == 0:
		parts = append(parts, "Daily note")
	case daily:
		parts = append(parts, "daily note")
	}
	return strings.Join(parts, ", ")
}

// ---- widget ----

const (
	widgetNotes  = 6 // rows of the widget
	widgetPinned = 3 // of them pinned at most, so the latest notes always show
)

// pocketWidget answers the notes changed last, pinned ones first as in the
// notes list, and how many changed today.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx, uid := r.Context(), me(r)
	pinned, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Pinned: true, Sort: "plain", Limit: widgetPinned})
	if err != nil {
		return nil, err
	}
	latest, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Sort: "plain", Limit: widgetNotes})
	if err != nil {
		return nil, err
	}
	if len(latest) == 0 {
		return nil, nil
	}
	now := time.Now()
	out := &core.WidgetData{URL: "/"}
	seen := map[int64]bool{}
	for _, n := range append(pinned, latest...) {
		if seen[n.ID] || len(out.Items) == widgetNotes {
			continue
		}
		seen[n.ID] = true
		detail := dayWord(time.Unix(n.Modified, 0), now)
		if n.Category != "" {
			detail += " · " + path.Base(n.Category)
		}
		out.Items = append(out.Items, core.WidgetItem{ID: strconv.FormatInt(n.ID, 10), Title: noteTitle(&n), Detail: detail, URL: noteURL(n.ID)})
	}
	if today := s.store.NoteDashboard(ctx, uid).EditedToday; today > 0 {
		out.Line = countOf(today, "note") + " changed today"
	}
	return out, nil
}

// dayWord says which day t was, seen from now: "today", "yesterday",
// "Monday" within a week, then "12 Nov" and "12 Nov 2025".
func dayWord(t, now time.Time) string {
	t = t.In(now.Location())
	midnight := func(x time.Time) time.Time {
		return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, now.Location())
	}
	days := int(midnight(now).Sub(midnight(t)).Hours()/24 + 0.5)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 7:
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// ---- capture ----

// captureLock is the person's lock for quick lines.
func (s *Server) captureLock(uid int64) *sync.Mutex {
	m, _ := s.captures.LoadOrStore(uid, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// pocketCapture keeps a quick line as a new note where the person's new notes
// go (Settings → "New notes go to"): made like any other note, so it is a
// file in their storage, in the history, and a notes.created event. The
// request id is remembered with the note, in one transaction: the same line
// sent again (a retry after a lost answer) answers that note and adds nothing.
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx, uid := r.Context(), me(r)
	slug, name := viaApp(r)
	op, err := core.NewOperation("pocket:capture|"+strconv.FormatInt(uid, 10)+"|"+slug, in.RequestID, in)
	if err != nil {
		return nil, err
	}
	lock := s.captureLock(uid)
	lock.Lock()
	defer lock.Unlock()

	var res core.CaptureResult
	done, err := op.Replay(ctx, s.store.DB, &res)
	switch {
	case errors.Is(err, core.ErrOperationConflict):
		return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
	case err != nil:
		return nil, err
	case done:
		return &res, nil
	}

	// Without a title the note is named after its first line, like any new
	// note. With one, the title is its first line too, as the app writes its
	// own notes: "Title follows the first line" then keeps the name.
	note := NoteInput{Content: &in.Text}
	if in.Title != "" {
		title := strings.Join(strings.Fields(in.Title), " ")
		content := "# " + title + "\n"
		if in.Text != "" && notesmd.TitleFromContent(in.Text) != title {
			content += "\n" + in.Text + "\n"
		} else if in.Text != "" {
			content = in.Text + "\n"
		}
		note.Title, note.Content = &title, &content
	}
	note.Saved = func(tx *sql.Tx, n *store.Note) error {
		res = core.CaptureResult{ID: strconv.FormatInt(n.ID, 10), Title: n.Title, URL: noteURL(n.ID)}
		return op.Save(ctx, tx, res)
	}
	// In the history and the event: the app it came through ("Pocket Hub"),
	// else what the client calls itself, else a quick capture.
	src := firstNonEmpty(name, slug)
	if src == "" {
		if src = source(r); src == "api" {
			src = "capture"
		}
	}
	if _, err := s.createNote(ctx, uid, note, src); err != nil {
		switch {
		case errors.Is(err, storage.ErrAuth):
			return nil, fmt.Errorf("%w: the notes storage refused the sign-in: check the storage settings in Pocket Notes", core.ErrCapture)
		case errors.Is(err, storage.ErrLocked):
			return nil, fmt.Errorf("%w: the notes storage is busy; try again in a moment", core.ErrCapture)
		}
		return nil, err
	}
	return &res, nil
}
