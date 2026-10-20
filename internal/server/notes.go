package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/diff"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/notesmd"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// source names who made a change, for the version history.
func source(r *http.Request) string {
	switch v := strings.ToLower(r.Header.Get("X-Pocket-Source")); v {
	case "web", "app", "capture", "shortcut":
		return v
	}
	return "api"
}

// refreshNote re-reads one note's file when it changed in the storage since
// the index last saw it: opening a note always shows its latest text.
func (s *Server) refreshNote(ctx context.Context, uid, id int64) {
	l, err := s.lib(ctx, uid)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil || n.Readonly {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	e, err := l.backend.Stat(cctx, n.Path)
	if err != nil || e.ETag == "" || e.ETag == n.RemoteETag {
		return
	}
	data, ent, err := l.backend.Read(cctx, n.Path)
	if err != nil {
		return
	}
	if hashOf(string(data)) == n.Hash {
		_ = s.store.SetRemote(ctx, uid, id, firstNonEmpty(e.ETag, ent.ETag), e.FileID, 0)
		return
	}
	tags, links := fill(n, string(data))
	n.RemoteETag, n.Modified = firstNonEmpty(e.ETag, ent.ETag), modifiedOf(e)
	if err := s.store.UpdateNote(ctx, n); err == nil {
		s.saveMeta(ctx, n, tags, links)
		s.addVersion(ctx, n, "sync")
	}
}

// ---- list / search ----

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	s.fresh(ctx, uid, 30*time.Second)
	st, _ := s.store.GetSettings(ctx, uid)
	q := r.URL.Query()
	f := store.NoteFilter{Tag: q.Get("tag"), Favorite: queryBool(r, "favorite"), Pinned: queryBool(r, "pinned"), HasTasks: queryBool(r, "tasks"),
		Sort: firstNonEmpty(q.Get("sort"), st.SortBy), Limit: clampInt(queryInt(r, "limit"), 0, 5000)}
	if q.Has("category") {
		c := q.Get("category")
		f.Category = &c
		f.Recursive = queryBool(r, "recursive")
	}
	if since := q.Get("since"); since != "" {
		f.Since, _ = strconv.ParseInt(since, 10, 64)
	}
	notes, err := s.store.ListNotes(ctx, uid, f)
	if err != nil {
		s.fail(w, err, "list notes")
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

var filterRe = regexp.MustCompile(`(?i)(?:^|\s)(tag|in|is|has):("[^"]*"|\S+)`)

// parseQuery takes the filters out of a search ("#tag", "tag:x", "in:Folder",
// "is:favorite", "is:pinned", "has:tasks") and returns the words left.
func parseQuery(q string) (string, store.NoteFilter) {
	var f store.NoteFilter
	words := filterRe.ReplaceAllStringFunc(q, func(m string) string {
		sm := filterRe.FindStringSubmatch(m)
		v := strings.Trim(sm[2], `"`)
		switch strings.ToLower(sm[1]) {
		case "tag":
			f.Tag = strings.TrimPrefix(strings.ToLower(v), "#")
		case "in":
			c := v
			f.Category = &c
			f.Recursive = true
		case "is":
			switch strings.ToLower(v) {
			case "fav", "favorite", "favourite", "starred":
				f.Favorite = true
			case "pinned":
				f.Pinned = true
			}
		case "has":
			if strings.HasPrefix(strings.ToLower(v), "task") || v == "todo" {
				f.HasTasks = true
			}
		}
		return " "
	})
	// A lone "#tag" is a tag filter too.
	var rest []string
	for _, w := range strings.Fields(words) {
		if strings.HasPrefix(w, "#") && len(w) > 1 && f.Tag == "" {
			f.Tag = strings.ToLower(w[1:])
			continue
		}
		rest = append(rest, w)
	}
	return strings.Join(rest, " "), f
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	words, f := parseQuery(r.URL.Query().Get("q"))
	limit := clampInt(queryInt(r, "limit"), 1, 200)
	if queryInt(r, "limit") == 0 {
		limit = 50
	}
	if strings.TrimSpace(words) == "" {
		f.Limit = limit
		notes, err := s.store.ListNotes(ctx, uid, f)
		if err != nil {
			s.fail(w, err, "search")
			return
		}
		hits := make([]store.SearchHit, 0, len(notes))
		for _, n := range notes {
			hits = append(hits, store.SearchHit{Note: n, Snippet: n.Excerpt})
		}
		writeJSON(w, http.StatusOK, hits)
		return
	}
	hits, err := s.store.Search(ctx, uid, words, f, limit)
	if err != nil {
		s.fail(w, err, "search")
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

// handleSuggest answers note titles for the quick switcher and [[ links.
func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	notes, err := s.store.TitleMatches(r.Context(), me(r), strings.TrimSpace(r.URL.Query().Get("q")), clampInt(queryInt(r, "limit"), 1, 50))
	if err != nil {
		s.fail(w, err, "suggest")
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

// handleResolve finds the note a link means: [[Title]] or a relative path.
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	from := int64(queryInt(r, "from"))
	folder := ""
	if from > 0 {
		if n, err := s.store.GetNote(ctx, uid, from); err == nil {
			folder = n.Category
		}
	}
	if i := strings.IndexAny(target, "#|"); i >= 0 && !strings.Contains(target, "/") {
		target = target[:i]
	}
	if isNoteFile(target) || strings.Contains(target, "/") {
		if p, err := storage.Resolve(folder, target); err == nil {
			if n, err := s.store.GetNoteByPath(ctx, uid, p); err == nil {
				n.Content = ""
				writeJSON(w, http.StatusOK, n)
				return
			}
		}
	}
	n, err := s.store.FindNoteByTitle(ctx, uid, strings.TrimSuffix(path.Base(target), path.Ext(target)), folder)
	if err != nil {
		n, err = s.store.FindNoteByTitle(ctx, uid, target, folder)
	}
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such note", "title": target})
		return
	}
	n.Content = ""
	writeJSON(w, http.StatusOK, n)
}

// ---- one note ----

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	if _, err := s.store.GetNote(ctx, uid, id); err != nil {
		s.fail(w, err, "get note")
		return
	}
	s.refreshNote(ctx, uid, id)
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "get note")
		return
	}
	if !queryBool(r, "peek") {
		s.store.TouchOpened(ctx, uid, id)
	}
	writeJSON(w, http.StatusOK, n)
}

type createBody struct {
	NoteInput
	TemplateID int64  `json:"template_id"`
	Template   string `json:"template"` // markdown to fill in
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in createBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	title := ""
	if in.Title != nil {
		title = *in.Title
	}
	if in.TemplateID > 0 || in.Template != "" {
		tpl := in.Template
		if in.TemplateID > 0 {
			t, err := s.store.GetNote(ctx, uid, in.TemplateID)
			if err != nil {
				s.fail(w, err, "template")
				return
			}
			tpl = t.Content
		}
		st, _ := s.store.GetSettings(ctx, uid)
		c := fillTemplate(tpl, title, time.Now(), st)
		if in.Content == nil || *in.Content == "" {
			in.Content = &c
		}
	}
	n, err := s.createNote(ctx, uid, in.NoteInput, source(r))
	if err != nil {
		s.fail(w, err, "create note")
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

type updateBody struct {
	NoteInput
	ETag string `json:"etag"`
}

func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	var in updateBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ifMatch := firstNonEmpty(r.Header.Get("If-Match"), in.ETag)
	n, err := s.updateNote(r.Context(), me(r), idParam(r, "id"), in.NoteInput, ifMatch, source(r))
	if err != nil {
		s.fail(w, err, "save note")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	if err := s.deleteNote(r.Context(), me(r), idParam(r, "id"), source(r)); err != nil {
		s.fail(w, err, "delete note")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// appendText adds text at the end of a note (a line, or a block after an
// empty line), retrying once when the note changed between read and write.
func (s *Server) appendText(ctx context.Context, uid, id int64, text, src string) (*store.Note, error) {
	for attempt := 0; attempt < 3; attempt++ {
		n, err := s.store.GetNote(ctx, uid, id)
		if err != nil {
			return nil, err
		}
		content := strings.TrimRight(n.Content, "\n")
		if content != "" {
			content += "\n"
			if !strings.HasPrefix(text, "- ") && !strings.HasPrefix(text, "* ") && !strings.HasPrefix(text, "|") {
				content += "\n"
			}
		}
		content += strings.TrimRight(text, "\n") + "\n"
		out, err := s.updateNote(ctx, uid, id, NoteInput{Content: &content}, n.Hash, src)
		var ce *conflictError
		if errors.As(err, &ce) {
			continue
		}
		return out, err
	}
	return nil, errors.New("the note kept changing; try again")
}

func (s *Server) handleAppendNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}
	n, err := s.appendText(r.Context(), me(r), idParam(r, "id"), in.Text, source(r))
	if err != nil {
		s.fail(w, err, "append")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleToggleTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	var in struct {
		Line int    `json:"line"`
		Text string `json:"text"`
		Done bool   `json:"done"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "task")
		return
	}
	content, ok := notesmd.ToggleTask(n.Content, in.Line, in.Text, in.Done)
	if !ok {
		writeErr(w, http.StatusConflict, "that task is not in the note any more")
		return
	}
	out, err := s.updateNote(ctx, uid, id, NoteInput{Content: &content}, n.Hash, source(r))
	if err != nil {
		s.fail(w, err, "task")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDuplicateNote(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	n, err := s.store.GetNote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "duplicate")
		return
	}
	title := n.Title + " (copy)"
	out, err := s.createNote(ctx, uid, NoteInput{Title: &title, Category: &n.Category, Content: &n.Content}, source(r))
	if err != nil {
		s.fail(w, err, "duplicate")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleBacklinks(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	n, err := s.store.GetNote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "backlinks")
		return
	}
	links, err := s.store.Backlinks(ctx, uid, n)
	if err != nil {
		s.fail(w, err, "backlinks")
		return
	}
	mentions, _ := s.store.Mentions(ctx, uid, n, 20)
	writeJSON(w, http.StatusOK, map[string]any{"backlinks": links, "mentions": mentions})
}

// ---- versions ----

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	if _, err := s.store.GetNote(ctx, uid, id); err != nil {
		s.fail(w, err, "versions")
		return
	}
	vs, err := s.store.ListVersions(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "versions")
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "version")
		return
	}
	v, err := s.store.GetVersion(ctx, uid, id, idParam(r, "vid"))
	if err != nil {
		s.fail(w, err, "version")
		return
	}
	ops := diff.Lines(v.Content, n.Content)
	added, removed := diff.Stat(ops)
	writeJSON(w, http.StatusOK, map[string]any{"version": v, "diff": diff.Compact(ops, 3), "added": added, "removed": removed})
}

func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	v, err := s.store.GetVersion(ctx, uid, id, idParam(r, "vid"))
	if err != nil {
		s.fail(w, err, "restore")
		return
	}
	n, err := s.updateNote(ctx, uid, id, NoteInput{Content: &v.Content}, "", "restore")
	if err != nil {
		s.fail(w, err, "restore")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// ---- folders, tags ----

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	s.fresh(r.Context(), me(r), 60*time.Second)
	f, err := s.folders(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "folders")
		return
	}
	sort.Slice(f, func(i, j int) bool { return strings.ToLower(f[i].Path) < strings.ToLower(f[j].Path) })
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	_ = decode(r, &in)
	p, err := s.makeFolder(r.Context(), me(r), in.Path)
	if err != nil {
		s.fail(w, err, "make folder")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": p})
}

func (s *Server) handleRenameFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	_ = decode(r, &in)
	if err := s.renameFolder(r.Context(), me(r), in.From, in.To); err != nil {
		if errors.Is(err, storage.ErrExists) {
			writeErr(w, http.StatusConflict, "a folder with that name is already there")
			return
		}
		s.fail(w, err, "rename folder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": notesmd.SafeFolder(in.To)})
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := s.deleteFolder(r.Context(), me(r), r.URL.Query().Get("path")); err != nil {
		s.fail(w, err, "delete folder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Tags(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "tags")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ---- tasks ----

// TaskLine is a checkbox with the note it is in.
type TaskLine struct {
	notesmd.Task
	NoteID   int64  `json:"note_id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Modified int64  `json:"modified"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	state := r.URL.Query().Get("state")
	notes, err := s.store.ListNotes(ctx, uid, store.NoteFilter{HasTasks: state != "done" && state != "all", Content: true, Sort: "plain"})
	if err != nil {
		s.fail(w, err, "tasks")
		return
	}
	out := []TaskLine{}
	for _, n := range notes {
		if n.TasksOpen+n.TasksDone == 0 {
			continue
		}
		for _, t := range notesmd.Analyze(n.Content).Tasks {
			if (state == "" || state == "open") && t.Done || state == "done" && !t.Done {
				continue
			}
			out = append(out, TaskLine{Task: t, NoteID: n.ID, Title: n.Title, Category: n.Category, Modified: n.Modified})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- daily notes, capture, templates ----

func dateLayout(st store.Settings) string {
	switch st.DateFormat {
	case "dmy-dot":
		return "02.01.2006"
	case "long":
		return "Monday, 2 January 2006"
	}
	return "2006-01-02"
}

var placeholderRe = regexp.MustCompile(`\{\{\s*(date|today|time|now|iso|weekday|title|tomorrow|yesterday)\s*\}\}`)

// fillTemplate fills {{date}}, {{time}}, {{now}}, {{iso}}, {{weekday}},
// {{title}}, {{tomorrow}} and {{yesterday}} in.
func fillTemplate(tpl, title string, at time.Time, st store.Settings) string {
	layout := dateLayout(st)
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		key := placeholderRe.FindStringSubmatch(m)[1]
		switch key {
		case "time":
			return at.Format("15:04")
		case "now":
			return at.Format(layout + " 15:04")
		case "iso":
			return at.Format("2006-01-02")
		case "weekday":
			return at.Format("Monday")
		case "title":
			return title
		case "tomorrow":
			return at.AddDate(0, 0, 1).Format(layout)
		case "yesterday":
			return at.AddDate(0, 0, -1).Format(layout)
		}
		return at.Format(layout)
	})
}

func parseDay(v string) (time.Time, error) {
	if v == "" || v == "today" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local), nil
	}
	return time.ParseInLocation("2006-01-02", v, time.Local)
}

// dailyNote finds the person's note for day; with create, makes it from
// their daily template when missing.
func (s *Server) dailyNote(ctx context.Context, uid int64, day time.Time, create bool) (*store.Note, string, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	title := notesmd.SafeName(day.Format(st.DailyFormat))
	folder := notesmd.SafeFolder(st.DailyFolder)
	for _, ext := range []string{st.FileSuffix, ".md", ".txt"} {
		p := title + ext
		if folder != "" {
			p = folder + "/" + p
		}
		if n, err := s.store.GetNoteByPath(ctx, uid, p); err == nil {
			return n, title, nil
		}
	}
	if !create {
		return nil, title, store.ErrNotFound
	}
	content := fillTemplate(st.DailyTemplate, title, day, st)
	n, err := s.createNote(ctx, uid, NoteInput{Title: &title, Category: &folder, Content: &content}, "daily")
	return n, title, err
}

func (s *Server) handleGetDaily(w http.ResponseWriter, r *http.Request) {
	day, err := parseDay(r.URL.Query().Get("date"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	n, title, err := s.dailyNote(r.Context(), me(r), day, false)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"note": nil, "title": title, "date": day.Format("2006-01-02")})
		return
	}
	if err != nil {
		s.fail(w, err, "daily note")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"note": n, "title": title, "date": day.Format("2006-01-02")})
}

func (s *Server) handleCreateDaily(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date string `json:"date"`
	}
	_ = decode(r, &in)
	day, err := parseDay(in.Date)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	n, title, err := s.dailyNote(r.Context(), me(r), day, true)
	if err != nil {
		s.fail(w, err, "daily note")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"note": n, "title": title, "date": day.Format("2006-01-02")})
}

// handleCalendar answers, for a month, which days have a daily note and how
// many notes were edited on each day.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	month, err := time.ParseInLocation("2006-01", r.URL.Query().Get("month"), time.Local)
	if err != nil {
		now := time.Now()
		month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	}
	st, _ := s.store.GetSettings(ctx, uid)
	type day struct {
		Daily  int64 `json:"daily,omitempty"`
		Edited int   `json:"edited"`
	}
	days := map[string]*day{}
	get := func(k string) *day {
		if days[k] == nil {
			days[k] = &day{}
		}
		return days[k]
	}
	end := month.AddDate(0, 1, 0)
	notes, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Since: month.Unix() - 1, Sort: "plain"})
	if err != nil {
		s.fail(w, err, "calendar")
		return
	}
	for _, n := range notes {
		t := time.Unix(n.Modified, 0).In(time.Local)
		if t.Before(end) {
			get(t.Format("2006-01-02")).Edited++
		}
	}
	folder := notesmd.SafeFolder(st.DailyFolder)
	dailies, _ := s.store.ListNotes(ctx, uid, store.NoteFilter{Category: &folder, Sort: "plain"})
	for _, n := range dailies {
		if t, err := time.ParseInLocation(st.DailyFormat, n.Title, time.Local); err == nil && !t.Before(month) && t.Before(end) {
			get(t.Format("2006-01-02")).Daily = n.ID
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"month": month.Format("2006-01"), "days": days})
}

// captureLine formats a captured thought: "- 9:12 text" by default.
func captureLine(format, text string, at time.Time) string {
	clock := fmt.Sprintf("%d:%02d", at.Hour(), at.Minute())
	out := strings.ReplaceAll(format, "{time}", clock)
	out = strings.ReplaceAll(out, "{date}", at.Format("2006-01-02"))
	return strings.ReplaceAll(out, "{text}", strings.TrimSpace(text))
}

// handleCapture puts a line into today's daily note (or into a given note),
// for Raycast, Shortcuts, a share sheet or a quick box in the UI.
func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Text   string `json:"text"`
		Date   string `json:"date"`
		NoteID int64  `json:"note_id"`
		Raw    bool   `json:"raw"` // the text as it is, without the time
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain") {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		in.Text = string(b)
	} else if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	line := in.Text
	if !in.Raw {
		// A multi-line capture keeps its lines, under one stamped first line.
		first, rest, _ := strings.Cut(strings.TrimSpace(in.Text), "\n")
		line = captureLine(st.CaptureFormat, first, time.Now())
		if rest != "" {
			line += "\n" + rest
		}
	}
	id := in.NoteID
	if id == 0 {
		day, err := parseDay(in.Date)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		n, _, err := s.dailyNote(ctx, uid, day, true)
		if err != nil {
			s.fail(w, err, "daily note")
			return
		}
		id = n.ID
	}
	n, err := s.appendText(ctx, uid, id, line, "capture")
	if err != nil {
		s.fail(w, err, "capture")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	folder := notesmd.SafeFolder(st.TemplatesFolder)
	if folder == "" {
		writeJSON(w, http.StatusOK, []store.Note{})
		return
	}
	notes, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Category: &folder, Recursive: true, Sort: "title"})
	if err != nil {
		s.fail(w, err, "templates")
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

// ---- trash ----

func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.ListTrash(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "trash")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleGetTrash(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTrash(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "trash")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleRestoreTrash(w http.ResponseWriter, r *http.Request) {
	n, err := s.restoreTrash(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "restore")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleDeleteTrash(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTrash(r.Context(), me(r), idParam(r, "id"), false); err != nil {
		s.fail(w, err, "trash")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTrash(r.Context(), me(r), 0, true); err != nil {
		s.fail(w, err, "trash")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- attachments ----

// attachmentDir is where a note's files go: the person's attachments folder,
// or beside the note as Nextcloud Notes keeps them (.attachments.<id>).
func attachmentDir(st store.Settings, n *store.Note) string {
	if f := notesmd.SafeFolder(st.AttachmentsFolder); f != "" {
		return f
	}
	return path.Join(n.Category, ".attachments."+strconv.FormatInt(n.ID, 10))
}

var unsafeFileChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

func safeFileName(name string) string {
	name = strings.TrimSpace(unsafeFileChars.ReplaceAllString(path.Base(strings.ReplaceAll(name, "\\", "/")), "-"))
	name = strings.TrimLeft(name, ".")
	if name == "" || name == "/" {
		name = "file"
	}
	if path.Ext(name) == "" {
		name += ".bin"
	}
	if len(name) > 150 {
		ext := path.Ext(name)
		name = name[:150-len(ext)] + ext
	}
	return name
}

// storeAttachment keeps a file with a note and answers the link to write
// into it (relative to the note's folder).
func (s *Server) storeAttachment(ctx context.Context, uid int64, n *store.Note, name string, data []byte) (string, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	l, err := s.lib(ctx, uid)
	if err != nil {
		return "", err
	}
	dir := attachmentDir(st, n)
	name = safeFileName(name)
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := 1; i < 200; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		p := path.Join(dir, candidate)
		_, err := l.backend.Create(ctx, p, data)
		if errors.Is(err, storage.ErrExists) {
			continue
		}
		if err != nil {
			return "", err
		}
		return storage.Rel(n.Category, p), nil
	}
	return "", errors.New("too many files with that name")
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	n, err := s.store.GetNote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "a file is required (form field \"file\")")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the file could not be read")
		return
	}
	rel, err := s.storeAttachment(ctx, uid, n, hdr.Filename, data)
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": rel, "filename": rel, "markdown": attachmentMarkdown(hdr.Filename, rel, hdr.Header.Get("Content-Type"))})
}

// encodeLink makes a path safe inside a Markdown link target.
func encodeLink(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		s = strings.NewReplacer("%", "%25", " ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(s)
		parts[i] = s
	}
	return strings.Join(parts, "/")
}

func attachmentMarkdown(name, rel, ctype string) string {
	label := strings.NewReplacer("[", "", "]", "").Replace(path.Base(name))
	if strings.HasPrefix(ctype, "image/") || isImageName(name) {
		return "![" + label + "](" + encodeLink(rel) + ")"
	}
	return "[" + label + "](" + encodeLink(rel) + ")"
}

func isImageName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".heic", ".avif", ".bmp":
		return true
	}
	return false
}

// serveFile answers a file of the notes folder with a fitting content type.
// SVG and HTML are sent as downloads: they could carry script.
func serveFile(w http.ResponseWriter, name string, data []byte, etag string) {
	ctype := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	if strings.Contains(ctype, "svg") || strings.Contains(ctype, "html") || strings.Contains(ctype, "xml") {
		w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(path.Base(name), `"`, "")+`"`)
	} else {
		w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(path.Base(name), `"`, "")+`"`)
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	if etag != "" {
		w.Header().Set("ETag", `"`+etag+`"`)
	}
	_, _ = w.Write(data)
}

// readNoteFile reads a file a note links to (relative to the note's folder).
func (s *Server) readNoteFile(ctx context.Context, uid int64, n *store.Note, link string) (string, []byte, storage.Entry, error) {
	p, err := storage.Resolve(n.Category, link)
	if err != nil {
		return "", nil, storage.Entry{}, err
	}
	l, err := s.lib(ctx, uid)
	if err != nil {
		return "", nil, storage.Entry{}, err
	}
	data, ent, err := l.backend.Read(ctx, p)
	return p, data, ent, err
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	n, err := s.store.GetNote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	p, data, ent, err := s.readNoteFile(ctx, uid, n, r.URL.Query().Get("path"))
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" && ent.ETag != "" && strings.Trim(inm, `"`) == ent.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	serveFile(w, p, data, ent.ETag)
}

func (s *Server) handleDownloadNote(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.GetNote(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "download")
		return
	}
	name := path.Base(n.Path)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"; filename*=UTF-8''`+urlPathEscape(name))
	_, _ = io.WriteString(w, n.Content)
}

// ---- shares ----

func (s *Server) handleNoteShares(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	if _, err := s.store.GetNote(ctx, uid, id); err != nil {
		s.fail(w, err, "shares")
		return
	}
	sh, err := s.store.ListShares(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "shares")
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	if _, err := s.store.GetNote(ctx, uid, id); err != nil {
		s.fail(w, err, "share")
		return
	}
	var in struct {
		Days int `json:"days"`
	}
	_ = decode(r, &in)
	sh := &store.Share{UserID: uid, NoteID: id}
	if in.Days > 0 {
		sh.ExpiresAt = time.Now().AddDate(0, 0, clampInt(in.Days, 1, 3650)).UTC().Format(time.RFC3339)
	}
	if err := s.store.CreateShare(ctx, sh); err != nil {
		s.fail(w, err, "share")
		return
	}
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	sh, err := s.store.ListShares(r.Context(), me(r), 0)
	if err != nil {
		s.fail(w, err, "shares")
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteShare(r.Context(), me(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "share")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePublicShare answers a shared note to anyone with the link.
func (s *Server) handlePublicShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sh, err := s.store.ShareByToken(ctx, chi_token(r), true)
	if err != nil {
		writeErr(w, http.StatusNotFound, "this link does not exist or has expired")
		return
	}
	n, err := s.store.GetNote(ctx, sh.UserID, sh.NoteID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "this note is gone")
		return
	}
	u, _ := s.store.GetUser(ctx, sh.UserID)
	author := ""
	if u != nil {
		author = firstNonEmpty(u.Name, strings.Split(u.Email, "@")[0])
	}
	w.Header().Set("X-Robots-Tag", "noindex")
	writeJSON(w, http.StatusOK, map[string]any{"title": n.Title, "content": n.Content, "modified": n.Modified, "author": author, "expires_at": sh.ExpiresAt})
}

func (s *Server) handlePublicShareAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sh, err := s.store.ShareByToken(ctx, chi_token(r), false)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	n, err := s.store.GetNote(ctx, sh.UserID, sh.NoteID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	link := r.URL.Query().Get("path")
	// Only files the shared note itself links to.
	if !strings.Contains(n.Content, link) && !strings.Contains(n.Content, encodeLink(link)) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	p, data, ent, err := s.readNoteFile(ctx, sh.UserID, n, link)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	serveFile(w, p, data, ent.ETag)
}

// ---- dashboard ----

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	d := s.store.NoteDashboard(ctx, uid)
	today := time.Now().Format("2006-01-02")
	if tasks, err := s.store.ListNotes(ctx, uid, store.NoteFilter{HasTasks: true, Content: true, Sort: "plain"}); err == nil {
		for _, n := range tasks {
			for _, t := range notesmd.Analyze(n.Content).Tasks {
				if !t.Done && t.Due != "" && t.Due <= today {
					d.DueTasks++
				}
			}
		}
	}
	if l, err := s.lib(ctx, uid); err == nil {
		st := l.status()
		d.Storage, _ = st["where"].(string)
		d.LastSync, _ = st["last_sync"].(string)
		d.SyncError, _ = st["error"].(string)
	}
	writeJSON(w, http.StatusOK, d)
}
