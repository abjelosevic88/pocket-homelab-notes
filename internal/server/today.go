package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/notesmd"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// The Today page: a day's note with what surrounds it — the week, the tasks
// due, what was written, the same day a week, a month and a year ago, the
// calendar from Nextcloud and the habits from Pocket Habits.

type todayNote struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Excerpt  string `json:"excerpt"`
	Modified int64  `json:"modified"`
	New      bool   `json:"new,omitempty"` // made that day
}

func shortNote(n store.Note) todayNote {
	return todayNote{ID: n.ID, Title: n.Title, Category: n.Category, Excerpt: n.Excerpt, Modified: n.Modified}
}

type todayDay struct {
	Date   string `json:"date"`
	Daily  int64  `json:"daily,omitempty"`
	Edited int    `json:"edited"`
}

type todayPast struct {
	Label string    `json:"label"`
	Date  string    `json:"date"`
	Note  todayNote `json:"note"`
}

// handleToday answers everything the page shows except the slow outside
// parts (calendar, habits), which have their own calls.
func (s *Server) handleToday(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	day, err := parseDay(r.URL.Query().Get("date"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	s.fresh(ctx, uid, 30*time.Second)
	st, _ := s.store.GetSettings(ctx, uid)
	daily, title, err := s.dailyNote(ctx, uid, day, false)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, err, "today")
		return
	}
	dayStart, dayEnd := day.Unix(), day.AddDate(0, 0, 1).Unix()

	// The week (Monday first), with daily notes and how much was written.
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	week := make([]todayDay, 7)
	idx := map[string]int{}
	for i := range week {
		d := monday.AddDate(0, 0, i)
		week[i] = todayDay{Date: d.Format("2006-01-02")}
		idx[week[i].Date] = i
	}
	recent, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Since: monday.Unix() - 1, Sort: "plain"})
	if err != nil {
		s.fail(w, err, "today")
		return
	}
	var edited []todayNote
	for _, n := range recent {
		t := time.Unix(n.Modified, 0).In(time.Local)
		if i, ok := idx[t.Format("2006-01-02")]; ok {
			week[i].Edited++
		}
		if n.Modified >= dayStart && n.Modified < dayEnd && (daily == nil || n.ID != daily.ID) {
			tn := shortNote(n)
			if c := store.ParseTime(n.CreatedAt).In(time.Local); !c.Before(day) && c.Unix() < dayEnd {
				tn.New = true
			}
			edited = append(edited, tn)
		}
	}
	folder := notesmd.SafeFolder(st.DailyFolder)
	dailies, _ := s.store.ListNotes(ctx, uid, store.NoteFilter{Category: &folder, Sort: "plain"})
	byDate := map[string]store.Note{}
	for _, n := range dailies {
		if t, err := time.ParseInLocation(st.DailyFormat, n.Title, time.Local); err == nil {
			byDate[t.Format("2006-01-02")] = n
		}
	}
	for i := range week {
		if n, ok := byDate[week[i].Date]; ok {
			week[i].Daily = n.ID
		}
	}

	// The same day before: a week, a month, a year (and more years) ago.
	var past []todayPast
	add := func(label string, d time.Time) {
		if n, ok := byDate[d.Format("2006-01-02")]; ok {
			past = append(past, todayPast{Label: label, Date: d.Format("2006-01-02"), Note: shortNote(n)})
		}
	}
	add("A week ago", day.AddDate(0, 0, -7))
	add("A month ago", day.AddDate(0, -1, 0))
	for y := 1; y <= 5; y++ {
		label := "A year ago"
		if y > 1 {
			label = itoaInt(y) + " years ago"
		}
		add(label, day.AddDate(-y, 0, 0))
	}

	// Tasks: overdue, due that day, the next week, and how many are open.
	type taskOut struct {
		Overdue  []TaskLine `json:"overdue"`
		Today    []TaskLine `json:"today"`
		Upcoming []TaskLine `json:"upcoming"`
		Open     int        `json:"open"`
	}
	tasks := taskOut{Overdue: []TaskLine{}, Today: []TaskLine{}, Upcoming: []TaskLine{}}
	dayISO, weekISO := day.Format("2006-01-02"), day.AddDate(0, 0, 7).Format("2006-01-02")
	templates := notesmd.SafeFolder(st.TemplatesFolder)
	if withTasks, err := s.store.ListNotes(ctx, uid, store.NoteFilter{HasTasks: true, Content: true, Sort: "plain"}); err == nil {
		for _, n := range withTasks {
			if templates != "" && (n.Category == templates || strings.HasPrefix(n.Category, templates+"/")) {
				continue
			}
			for _, t := range notesmd.Analyze(n.Content).Tasks {
				if t.Done || strings.TrimSpace(t.Text) == "" {
					continue
				}
				tasks.Open++
				line := TaskLine{Task: t, NoteID: n.ID, Title: n.Title, Category: n.Category, Modified: n.Modified}
				switch {
				case t.Due == "":
				case t.Due < dayISO:
					tasks.Overdue = append(tasks.Overdue, line)
				case t.Due == dayISO:
					tasks.Today = append(tasks.Today, line)
				case t.Due <= weekISO:
					tasks.Upcoming = append(tasks.Upcoming, line)
				}
			}
		}
	}
	sort.Slice(tasks.Overdue, func(i, j int) bool { return tasks.Overdue[i].Due < tasks.Overdue[j].Due })
	sort.Slice(tasks.Upcoming, func(i, j int) bool { return tasks.Upcoming[i].Due < tasks.Upcoming[j].Due })
	// The daily note's own open boxes are today's too.
	if daily != nil {
		for _, t := range notesmd.Analyze(daily.Content).Tasks {
			if !t.Done && t.Due == "" && strings.TrimSpace(t.Text) != "" {
				tasks.Today = append(tasks.Today, TaskLine{Task: t, NoteID: daily.ID, Title: daily.Title, Category: daily.Category, Modified: daily.Modified})
			}
		}
	}

	opened, _ := s.store.ListNotes(ctx, uid, store.NoteFilter{Sort: "opened", Limit: 8})
	var recentOpened []todayNote
	for _, n := range opened {
		if n.OpenedAt != "" && (daily == nil || n.ID != daily.ID) {
			recentOpened = append(recentOpened, shortNote(n))
		}
	}

	if edited == nil {
		edited = []todayNote{}
	}
	if past == nil {
		past = []todayPast{}
	}
	if recentOpened == nil {
		recentOpened = []todayNote{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date": dayISO, "today": time.Now().Format("2006-01-02"), "title": title, "note": daily, "week": week,
		"tasks": tasks, "edited": edited, "on_this_day": past, "recent": recentOpened,
		"capture_format": st.CaptureFormat, "daily_folder": st.DailyFolder,
	})
}

func itoaInt(n int) string { return strconv.Itoa(n) }

// ---- calendar (Nextcloud CalDAV) ----

type agendaEvent struct {
	Title    string `json:"title"`
	Start    string `json:"start"` // RFC3339, local time
	End      string `json:"end,omitempty"`
	AllDay   bool   `json:"all_day"`
	Location string `json:"location,omitempty"`
	Calendar string `json:"calendar"`
	Color    string `json:"color,omitempty"`
}

const agendaCalendarsBody = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:x1="http://apple.com/ns/ical/"><d:prop><d:displayname/><d:resourcetype/><c:supported-calendar-component-set/><x1:calendar-color/></d:prop></d:propfind>`

// handleTodayAgenda lists the day's events from the person's Nextcloud
// calendars (recurring ones expanded by Nextcloud).
func (s *Server) handleTodayAgenda(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	day, err := parseDay(r.URL.Query().Get("date"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := s.service(cctx, uid, "nextcloud")
	if err != nil {
		var nc errNotConnected
		if errors.As(err, &nc) {
			writeJSON(w, http.StatusOK, map[string]any{"connected": false, "events": []agendaEvent{}})
			return
		}
		toolErr(w, err)
		return
	}
	events, err := s.agenda(cctx, c, day)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "events": []agendaEvent{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "events": events, "url": c.Link("/apps/calendar/timeGridDay/" + day.Format("2006-01-02"))})
}

func (s *Server) agenda(ctx context.Context, c *svcCred, day time.Time) ([]agendaEvent, error) {
	user, err := s.actNCUser(ctx, c)
	if err != nil {
		return nil, err
	}
	hdr := actNCHeaders(c)
	hdr["Depth"], hdr["Content-Type"] = "1", "application/xml; charset=utf-8"
	var raw []byte
	if err := s.callJSON(ctx, "nextcloud", "PROPFIND", c.API(actCalendarsRoot(user)), hdr, agendaCalendarsBody, &raw); err != nil {
		return nil, err
	}
	cals, err := actParseMultistatus(raw)
	if err != nil {
		return nil, err
	}
	start, end := day.UTC().Format("20060102T150405Z"), day.AddDate(0, 0, 1).UTC().Format("20060102T150405Z")
	query := `<?xml version="1.0"?><c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-data><c:expand start="` + start + `" end="` + end + `"/></c:calendar-data></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="` + start + `" end="` + end + `"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	out := []agendaEvent{}
	type result struct {
		events []agendaEvent
	}
	ch := make(chan result)
	n := 0
	for _, cal := range cals {
		if !cal.types["calendar"] || cal.types["deleted-calendar"] || !cal.comps["VEVENT"] {
			continue
		}
		href := cal.href
		name := firstNonEmpty(cal.props["displayname"], strings.Trim(href, "/"))
		color := cal.props["calendar-color"]
		if len(color) > 7 {
			color = color[:7]
		}
		n++
		go func() {
			h := actNCHeaders(c)
			h["Depth"], h["Content-Type"] = "1", "application/xml; charset=utf-8"
			target := href
			if !strings.HasPrefix(target, "http") {
				u, _ := url.Parse(c.API("/"))
				target = u.Scheme + "://" + u.Host + href
			}
			var body []byte
			var evs []agendaEvent
			if err := s.callJSON(ctx, "nextcloud", "REPORT", target, h, query, &body); err == nil {
				if resp, err := actParseMultistatus(body); err == nil {
					for _, r := range resp {
						for _, e := range parseVEvents(r.props["calendar-data"], day) {
							e.Calendar, e.Color = name, color
							evs = append(evs, e)
						}
					}
				}
			}
			ch <- result{evs}
		}()
	}
	for i := 0; i < n; i++ {
		out = append(out, (<-ch).events...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AllDay != out[j].AllDay {
			return out[i].AllDay
		}
		return out[i].Start < out[j].Start
	})
	return out, nil
}

// parseVEvents reads the events of an iCalendar text that fall on day.
func parseVEvents(ics string, day time.Time) []agendaEvent {
	// Unfold continuation lines.
	ics = strings.ReplaceAll(ics, "\r\n", "\n")
	ics = strings.ReplaceAll(ics, "\n ", "")
	ics = strings.ReplaceAll(ics, "\n\t", "")
	var out []agendaEvent
	var cur map[string][2]string // name → params, value
	for _, line := range strings.Split(ics, "\n") {
		switch {
		case line == "BEGIN:VEVENT":
			cur = map[string][2]string{}
		case line == "END:VEVENT" && cur != nil:
			if e, ok := eventFrom(cur, day); ok {
				out = append(out, e)
			}
			cur = nil
		case cur != nil:
			i := strings.IndexByte(line, ':')
			if i < 0 {
				continue
			}
			head, value := line[:i], line[i+1:]
			name, params, _ := strings.Cut(head, ";")
			if _, seen := cur[name]; !seen {
				cur[name] = [2]string{params, value}
			}
		}
	}
	return out
}

func icalText(s string) string {
	return strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

func icalTime(p [2]string) (time.Time, bool, bool) {
	params, v := p[0], p[1]
	if v == "" {
		return time.Time{}, false, false
	}
	if strings.Contains(params, "VALUE=DATE") && !strings.Contains(params, "DATE-TIME") || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, time.Local)
		return t, true, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t.In(time.Local), false, err == nil
	}
	loc := time.Local
	for _, kv := range strings.Split(params, ";") {
		if strings.HasPrefix(kv, "TZID=") {
			if l, err := time.LoadLocation(strings.Trim(kv[5:], `"`)); err == nil {
				loc = l
			}
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	return t.In(time.Local), false, err == nil
}

func eventFrom(p map[string][2]string, day time.Time) (agendaEvent, bool) {
	if strings.EqualFold(p["STATUS"][1], "CANCELLED") {
		return agendaEvent{}, false
	}
	start, allDay, ok := icalTime(p["DTSTART"])
	if !ok {
		return agendaEvent{}, false
	}
	end, _, hasEnd := icalTime(p["DTEND"])
	if !hasEnd {
		end = start
		if allDay {
			end = start.AddDate(0, 0, 1)
		}
	}
	dayEnd := day.AddDate(0, 0, 1)
	if !start.Before(dayEnd) || (end.After(start) && !end.After(day)) || (!end.After(start) && start.Before(day)) {
		return agendaEvent{}, false
	}
	e := agendaEvent{Title: firstNonEmpty(icalText(p["SUMMARY"][1]), "(no title)"), AllDay: allDay, Location: icalText(p["LOCATION"][1])}
	if allDay {
		e.Start = start.Format("2006-01-02")
	} else {
		e.Start = start.Format(time.RFC3339)
		if hasEnd {
			e.End = end.Format(time.RFC3339)
		}
	}
	return e, true
}

// ---- habits (Pocket Habits) ----

// handleTodayHabits answers Pocket Habits' page for the day (habits,
// trackers, weather) as it is.
func (s *Server) handleTodayHabits(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	day, err := parseDay(r.URL.Query().Get("date"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	base, tok, err := s.appToken(cctx, uid, "habits", "habits:read")
	if err != nil {
		var nc errNotConnected
		if errors.As(err, &nc) {
			writeJSON(w, http.StatusOK, map[string]any{"connected": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "error": err.Error()})
		return
	}
	var page json.RawMessage
	if err := s.callJSON(cctx, "habits", http.MethodGet, base+"/api/v1/days/"+day.Format("2006-01-02"), map[string]string{"Authorization": "Bearer " + tok}, nil, &page); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "day": page, "url": s.appPublicURL(ctx, uid, "habits")})
}

// handleTodayHabit marks a habit done (1), missed (-1) or clears it (0) for a day.
func (s *Server) handleTodayHabit(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Date   string `json:"date"`
		Status int    `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	day, err := parseDay(in.Date)
	if err != nil || in.Status < -1 || in.Status > 1 {
		writeErr(w, http.StatusBadRequest, "a date and a status of 1, -1 or 0 are needed")
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := parseInt(id); err != nil {
		writeErr(w, http.StatusBadRequest, "bad habit id")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	base, tok, err := s.appToken(cctx, uid, "habits", "entries:write")
	if err != nil {
		toolErr(w, err)
		return
	}
	target := base + "/api/v1/habits/" + id + "/entries/" + day.Format("2006-01-02")
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	if in.Status == 0 {
		err = s.callJSON(cctx, "habits", http.MethodDelete, target, hdr, nil, nil)
	} else {
		err = s.callJSON(cctx, "habits", http.MethodPut, target, hdr, map[string]int{"status": in.Status}, nil)
	}
	if err != nil {
		toolErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func parseInt(s string) (int64, error) {
	var n int64
	if s == "" {
		return 0, errors.New("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
