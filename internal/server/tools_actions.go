package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// A line of a note sent away, with a link left behind in its place: a task
// in Nextcloud Tasks (a VTODO in one of the person's CalDAV task lists), a
// card in Nextcloud Deck, or a transaction in Pocket Money. The Markdown that
// comes back is what the iPhone app's finish() writes: the line, " · ", and
// a link "[→ Tasks](…)".

// ---- the line with its link ----

// actMarkdown is the line again with a link to where it went.
func actMarkdown(line, dest, link string) string {
	return actPlain(line) + " · [→ " + actPlain(dest) + "](" + actLinkTarget(link) + ")"
}

// actPlain is text that stays text inside a link: brackets escaped, one line.
func actPlain(s string) string {
	s = strings.NewReplacer("[", `\[`, "]", `\]`).Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' })
	return strings.Join(parts, " ")
}

// actLinkTarget spells an address so a space or ")" does not end the link.
func actLinkTarget(u string) string {
	return strings.NewReplacer(" ", "%20", ")", "%29").Replace(u)
}

// ---- Nextcloud: task lists and Deck boards ----

const actDeckRoot = "/index.php/apps/deck/api/v1.0"

// actTaskList is a CalDAV calendar that holds tasks and can be written to.
type actTaskList struct {
	ID    string `json:"id"` // the calendar's last path component ("personal")
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	order int
}

type actDeckStack struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type actDeckBoard struct {
	ID     int64          `json:"id"`
	Title  string         `json:"title"`
	Color  string         `json:"color"`
	Stacks []actDeckStack `json:"stacks"`
}

func actNCHeaders(c *svcCred) map[string]string {
	return map[string]string{"Authorization": basicAuth(c.Field("USERNAME"), firstNonEmpty(c.Field("APP_PASSWORD"), c.Key))}
}

// actNCUser is the Nextcloud username, asked of Nextcloud when the
// connection did not carry one.
func (s *Server) actNCUser(ctx context.Context, c *svcCred) (string, error) {
	if u := c.Field("USERNAME"); u != "" {
		return u, nil
	}
	var out struct {
		OCS struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"ocs"`
	}
	hdr := actNCHeaders(c)
	hdr["OCS-APIRequest"] = "true"
	if err := s.callJSON(ctx, "nextcloud", http.MethodGet, c.API("/ocs/v2.php/cloud/user?format=json"), hdr, nil, &out); err != nil {
		return "", err
	}
	if out.OCS.Data.ID == "" {
		return "", errors.New("Nextcloud did not say who this is; add the username in Settings → Integrations")
	}
	return out.OCS.Data.ID, nil
}

// actCalendarsRoot is where the person's calendars live on the DAV server.
func actCalendarsRoot(user string) string {
	return "/remote.php/dav/calendars/" + url.PathEscape(user) + "/"
}

const actListsBody = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:x1="http://apple.com/ns/ical/"><d:prop><d:displayname/><d:resourcetype/><d:current-user-privilege-set/><c:supported-calendar-component-set/><x1:calendar-color/><x1:calendar-order/></d:prop></d:propfind>`

// actTaskLists are the person's task lists they can write to, in their order.
func (s *Server) actTaskLists(ctx context.Context, c *svcCred) ([]actTaskList, error) {
	user, err := s.actNCUser(ctx, c)
	if err != nil {
		return nil, err
	}
	hdr := actNCHeaders(c)
	hdr["Depth"], hdr["Content-Type"] = "1", "application/xml; charset=utf-8"
	var raw []byte
	if err := s.callJSON(ctx, "nextcloud", "PROPFIND", c.API(actCalendarsRoot(user)), hdr, actListsBody, &raw); err != nil {
		return nil, err
	}
	responses, err := actParseMultistatus(raw)
	if err != nil {
		return nil, err
	}
	root := "/remote.php/dav/calendars/" + user + "/"
	out := []actTaskList{}
	for _, r := range responses {
		if !r.types["calendar"] || r.types["deleted-calendar"] || !r.comps["VTODO"] {
			continue
		}
		path, err := url.PathUnescape(r.href)
		if err != nil {
			path = r.href
		}
		i := strings.Index(path, root)
		if i < 0 {
			continue
		}
		id := strings.Trim(path[i+len(root):], "/")
		if id == "" || strings.Contains(id, "/") || strings.HasPrefix(id, "app-generated--") {
			continue
		}
		if !r.privs["write-content"] && !r.privs["write"] {
			continue
		}
		color := r.props["calendar-color"]
		if len(color) > 7 {
			color = color[:7]
		}
		order, _ := strconv.Atoi(r.props["calendar-order"])
		out = append(out, actTaskList{ID: id, Name: firstNonEmpty(r.props["displayname"], id), Color: color, order: order})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].order != out[j].order {
			return out[i].order < out[j].order
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// actDAVResponse is one <response> of a multistatus: its href and what its
// 200 propstats said.
type actDAVResponse struct {
	href                string
	props               map[string]string
	types, comps, privs map[string]bool
}

func newActDAVResponse() actDAVResponse {
	return actDAVResponse{props: map[string]string{}, types: map[string]bool{}, comps: map[string]bool{}, privs: map[string]bool{}}
}

// actParseMultistatus reads a CalDAV multistatus by local names: text props,
// resourcetype children, supported components and privileges.
func actParseMultistatus(raw []byte) ([]actDAVResponse, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var (
		out      []actDAVResponse
		cur      *actDAVResponse
		pending  = newActDAVResponse()
		okStat   bool
		stack    []string
		text     strings.Builder
		sawMulti bool
	)
	// up(n) is the element n levels up the stack (1 = the innermost open one).
	up := func(n int) string {
		if len(stack) >= n {
			return stack[len(stack)-n]
		}
		return ""
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Nextcloud answered something that is not CalDAV: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name, parent := t.Name.Local, up(1)
			stack = append(stack, name)
			text.Reset()
			switch name {
			case "multistatus":
				sawMulti = true
			case "response":
				r := newActDAVResponse()
				cur = &r
			case "propstat":
				pending, okStat = newActDAVResponse(), false
			case "comp":
				if parent == "supported-calendar-component-set" {
					for _, a := range t.Attr {
						if a.Name.Local == "name" {
							pending.comps[strings.ToUpper(a.Value)] = true
						}
					}
				}
			default:
				switch parent {
				case "resourcetype":
					pending.types[name] = true
				case "privilege":
					pending.privs[name] = true
				}
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			name, parent := t.Name.Local, up(2)
			value := strings.TrimSpace(text.String())
			switch name {
			case "href":
				if parent == "response" && cur != nil {
					cur.href = value
				}
			case "status":
				if parent == "propstat" {
					okStat = strings.Contains(value, " 200 ")
				}
			case "propstat":
				if okStat && cur != nil {
					for k, v := range pending.props {
						cur.props[k] = v
					}
					for k := range pending.types {
						cur.types[k] = true
					}
					for k := range pending.comps {
						cur.comps[k] = true
					}
					for k := range pending.privs {
						cur.privs[k] = true
					}
				}
			case "response":
				if cur != nil {
					out = append(out, *cur)
				}
				cur = nil
			default:
				if parent == "prop" && value != "" {
					pending.props[name] = value
				}
			}
			text.Reset()
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !sawMulti {
		return nil, errors.New("Nextcloud answered with a page instead of a CalDAV response")
	}
	return out, nil
}

// actDeckBoards are the Deck boards the person can add cards to, each with
// its stacks (the lists cards sit in), by title.
func (s *Server) actDeckBoards(ctx context.Context, c *svcCred) ([]actDeckBoard, error) {
	hdr := actNCHeaders(c)
	hdr["OCS-APIRequest"] = "true"
	var raw []struct {
		ID          int64           `json:"id"`
		Title       string          `json:"title"`
		Color       string          `json:"color"`
		Archived    bool            `json:"archived"`
		DeletedAt   int64           `json:"deletedAt"`
		Permissions map[string]bool `json:"permissions"`
	}
	if err := s.callJSON(ctx, "nextcloud", http.MethodGet, c.API(actDeckRoot+"/boards?details=true"), hdr, nil, &raw); err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return nil, errors.New("Deck is not enabled on this Nextcloud")
		}
		return nil, err
	}
	boards := []actDeckBoard{}
	for _, b := range raw {
		if b.Archived || b.DeletedAt != 0 || (b.Permissions != nil && !b.Permissions["PERMISSION_EDIT"]) {
			continue
		}
		boards = append(boards, actDeckBoard{ID: b.ID, Title: b.Title, Color: b.Color, Stacks: []actDeckStack{}})
	}
	sort.SliceStable(boards, func(i, j int) bool { return strings.ToLower(boards[i].Title) < strings.ToLower(boards[j].Title) })
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range boards {
		wg.Add(1)
		go func(b *actDeckBoard) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var stacks []struct {
				ID        int64  `json:"id"`
				Title     string `json:"title"`
				Order     int    `json:"order"`
				DeletedAt int64  `json:"deletedAt"`
			}
			if err := s.callJSON(ctx, "nextcloud", http.MethodGet, c.API(fmt.Sprintf("%s/boards/%d/stacks", actDeckRoot, b.ID)), hdr, nil, &stacks); err != nil {
				return
			}
			sort.SliceStable(stacks, func(i, j int) bool {
				if stacks[i].Order != stacks[j].Order {
					return stacks[i].Order < stacks[j].Order
				}
				return stacks[i].ID < stacks[j].ID
			})
			for _, st := range stacks {
				if st.DeletedAt == 0 {
					b.Stacks = append(b.Stacks, actDeckStack{ID: st.ID, Title: st.Title})
				}
			}
		}(&boards[i])
	}
	wg.Wait()
	return boards, nil
}

// actDefaultDestination is Tasks when there is a list to write to; with no
// list but a Deck board, the card is the way that works.
func actDefaultDestination(lists []actTaskList, boards []actDeckBoard) string {
	if len(lists) == 0 && len(boards) > 0 {
		return "deck"
	}
	return "tasks"
}

// handleToolTaskTargets lists where a line can go:
//
//	{"lists":[{"id":"personal","name":"Personal","color":"#0082c9"}],
//	 "boards":[{"id":12,"title":"Homelab","color":"0087c5","stacks":[{"id":34,"title":"To do"}]}],
//	 "default":"tasks|deck", "errors":{"tasks":"…","deck":"…"}}
//
// Only lists and boards the person can write to are listed. A service that
// fails leaves its array empty and says why under "errors".
func (s *Server) handleToolTaskTargets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("destination") == "pocket-tasks" {
		s.pocketTaskTargets(w, r)
		return
	}
	ctx, uid := r.Context(), me(r)
	c, err := s.service(ctx, uid, "nextcloud")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var (
		lists            []actTaskList
		boards           []actDeckBoard
		listErr, deckErr error
		wg               sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); lists, listErr = s.actTaskLists(cctx, c) }()
	go func() { defer wg.Done(); boards, deckErr = s.actDeckBoards(cctx, c) }()
	wg.Wait()
	if lists == nil {
		lists = []actTaskList{}
	}
	if boards == nil {
		boards = []actDeckBoard{}
	}
	out := map[string]any{"lists": lists, "boards": boards, "default": actDefaultDestination(lists, boards)}
	errs := map[string]string{}
	if listErr != nil {
		errs["tasks"] = listErr.Error()
	}
	if deckErr != nil {
		errs["deck"] = deckErr.Error()
	}
	if len(errs) > 0 {
		out["errors"] = errs
	}
	writeJSON(w, http.StatusOK, out)
}

// handleToolTask sends a line to Nextcloud Tasks or Deck.
//
// In: {"text":"Check the UPS tomorrow","line":"<the line as it is in the note, when text was edited>",
// "destination":"tasks|deck","list":"personal","board":12,"stack":34,"due":"YYYY-MM-DD"}.
// A date in the text ("tomorrow", "fri", "next week", "12.11.") becomes the
// due date and leaves the title; "due" wins over it. With no list (or board
// and stack) given, the first one there is used; Tasks with nothing to write
// to falls to Deck.
//
// Out: {"url":"…","markdown":"<line> · [→ Tasks](…)","title":"Check the UPS","destination":"tasks","due":"YYYY-MM-DD"}.
func (s *Server) handleToolTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Text        string `json:"text"`
		Line        string `json:"line"`
		Destination string `json:"destination"`
		List        string `json:"list"`
		Board       int64  `json:"board"`
		Stack       int64  `json:"stack"`
		Due         string `json:"due"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" {
		writeErr(w, http.StatusBadRequest, "Write the line first, then make it a task.")
		return
	}
	if in.Destination != "" && in.Destination != "tasks" && in.Destination != "deck" && in.Destination != "pocket-tasks" {
		writeErr(w, http.StatusBadRequest, "destination must be tasks, deck or pocket-tasks")
		return
	}
	now := time.Now()
	title, due := actTaskDue(text, now)
	if title == "" {
		title = text
	}
	if in.Due != "" {
		d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(in.Due), time.Local)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "due must be YYYY-MM-DD")
			return
		}
		due = d
	}
	if in.Destination == "pocket-tasks" {
		s.createPocketTask(w, r, title, firstNonEmpty(in.Line, text), in.List, due)
		return
	}
	c, err := s.service(ctx, uid, "nextcloud")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	dest := firstNonEmpty(in.Destination, "tasks")
	if dest == "tasks" && in.List == "" {
		lists, err := s.actTaskLists(cctx, c)
		if err != nil && in.Destination == "tasks" {
			toolErr(w, err)
			return
		}
		if len(lists) > 0 {
			in.List = lists[0].ID
		} else {
			dest = "deck"
		}
	}
	if dest == "deck" && (in.Board == 0 || in.Stack == 0) {
		boards, err := s.actDeckBoards(cctx, c)
		if err != nil {
			toolErr(w, err)
			return
		}
		for _, b := range boards {
			if (in.Board == 0 || b.ID == in.Board) && len(b.Stacks) > 0 {
				in.Board, in.Stack = b.ID, b.Stacks[0].ID
				break
			}
		}
		if in.Stack == 0 {
			if in.Destination == "deck" {
				writeErr(w, http.StatusBadRequest, "No Deck board you can write to.")
			} else {
				writeErr(w, http.StatusBadRequest, "No task list or Deck board you can write to.")
			}
			return
		}
	}

	line := firstNonEmpty(strings.TrimSpace(in.Line), text)
	out := map[string]any{"title": title, "destination": dest}
	if !due.IsZero() {
		out["due"] = due.Format("2006-01-02")
	}
	switch dest {
	case "tasks":
		user, err := s.actNCUser(cctx, c)
		if err != nil {
			toolErr(w, err)
			return
		}
		uidStr := actUUID()
		hdr := actNCHeaders(c)
		hdr["Content-Type"], hdr["If-None-Match"] = "text/calendar; charset=utf-8", "*"
		target := c.API(actCalendarsRoot(user) + url.PathEscape(in.List) + "/" + uidStr + ".ics")
		if err := s.callJSON(cctx, "nextcloud", http.MethodPut, target, hdr, actVTODO(uidStr, title, due, now), nil); err != nil {
			toolErr(w, err)
			return
		}
		link := c.Link("/apps/tasks/#/calendars/" + url.PathEscape(in.List) + "/tasks/" + uidStr + ".ics")
		out["url"], out["markdown"] = link, actMarkdown(line, "Tasks", link)
	case "deck":
		if utf8.RuneCountInString(title) > 255 {
			writeErr(w, http.StatusBadRequest, "A Deck card's title is at most 255 characters.")
			return
		}
		body := map[string]any{"title": title, "type": "plain", "order": 999, "description": "", "duedate": nil}
		if !due.IsZero() {
			// an all-day date is due at the end of that day, as the app sends it
			body["duedate"] = time.Date(due.Year(), due.Month(), due.Day(), 23, 59, 0, 0, time.Local).Format(time.RFC3339)
		}
		hdr := actNCHeaders(c)
		hdr["OCS-APIRequest"] = "true"
		var card struct {
			ID int64 `json:"id"`
		}
		target := c.API(fmt.Sprintf("%s/boards/%d/stacks/%d/cards", actDeckRoot, in.Board, in.Stack))
		if err := s.callJSON(cctx, "nextcloud", http.MethodPost, target, hdr, body, &card); err != nil {
			toolErr(w, err)
			return
		}
		if card.ID == 0 {
			writeErr(w, http.StatusBadGateway, "Deck gave the card no id")
			return
		}
		link := c.Link(fmt.Sprintf("/index.php/apps/deck/#/board/%d/card/%d", in.Board, card.ID))
		out["url"], out["markdown"] = link, actMarkdown(line, "Deck", link)
	}
	writeJSON(w, http.StatusOK, out)
}

// actUUID is a random UUID, upper case like the app's.
func actUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

// actVTODO is a fresh VCALENDAR with one open task, lines folded at 75 octets.
func actVTODO(uid, summary string, due time.Time, now time.Time) string {
	stamp := now.UTC().Format("20060102T150405Z")
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//PocketNotes//Tasks//EN", "CALSCALE:GREGORIAN", "BEGIN:VTODO",
		"UID:" + uid, "CREATED:" + stamp, "SUMMARY:" + actICalEscape(summary)}
	if !due.IsZero() {
		lines = append(lines, "DUE;VALUE=DATE:"+due.Format("20060102"))
	}
	lines = append(lines, "STATUS:NEEDS-ACTION", "DTSTAMP:"+stamp, "LAST-MODIFIED:"+stamp, "SEQUENCE:0", "END:VTODO", "END:VCALENDAR")
	for i, l := range lines {
		lines[i] = actICalFold(l)
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

func actICalEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(s)
}

// actICalFold folds a line at 75 octets (RFC 5545 §3.1), never inside a character.
func actICalFold(line string) string {
	var out, cur strings.Builder
	n := 0
	for _, r := range line {
		size := utf8.RuneLen(r)
		if n+size > 75 && cur.Len() > 0 {
			out.WriteString(cur.String() + "\r\n ")
			cur.Reset()
			n = 1
		}
		cur.WriteRune(r)
		n += size
	}
	return out.String() + cur.String()
}

// ---- dates in a task line ----

// The date part of the app's quick add (TaskQuickAdd): "today", "tomorrow",
// "fri", "next mon", "next week", "end of month", "in 3 days", "2026-11-02",
// "12.11.", "12 nov", "nov 12", "by the 15th". Times, repeats, priorities
// and labels stay in the title; "every fri" is a repeat, not a date.

var (
	actWeekdays = map[string]time.Weekday{
		"sunday": time.Sunday, "sun": time.Sunday, "nedelja": time.Sunday, "nedjelja": time.Sunday,
		"monday": time.Monday, "mon": time.Monday, "ponedeljak": time.Monday, "ponedjeljak": time.Monday,
		"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday, "utorak": time.Tuesday,
		"wednesday": time.Wednesday, "wed": time.Wednesday, "sreda": time.Wednesday, "srijeda": time.Wednesday,
		"thursday": time.Thursday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "četvrtak": time.Thursday, "cetvrtak": time.Thursday,
		"friday": time.Friday, "fri": time.Friday, "petak": time.Friday,
		"saturday": time.Saturday, "sat": time.Saturday, "subota": time.Saturday,
	}
	actMonths = map[string]time.Month{
		"january": 1, "jan": 1, "february": 2, "feb": 2, "march": 3, "mar": 3, "april": 4, "apr": 4, "may": 5,
		"june": 6, "jun": 6, "july": 7, "jul": 7, "august": 8, "aug": 8, "september": 9, "sep": 9, "sept": 9,
		"october": 10, "oct": 10, "november": 11, "nov": 11, "december": 12, "dec": 12,
	}
	actSmallNumbers = map[string]int{"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10}

	actDayAlt   = actAlternation(actWeekdays)
	actMonthAlt = actAlternation(actMonths)
	actNumAlt   = actAlternation(actSmallNumbers)
	actLead     = `(?:(?:on|by|for|until|due)\s+)?`

	actEveryDaysRX = regexp.MustCompile(`(?i)\b(?:every|svaki|svake|svakog)\s+((?:weekdays?|` + actDayAlt + `)(?:\s*(?:,|and|&|i)\s*(?:` + actDayAlt + `))*)\b`)
	actEveryUnitRX = regexp.MustCompile(`(?i)\b(?:every|svaki|svake|svakog)\s+(?:(other|\d+|` + actNumAlt + `)\s+)?(days?|weeks?|months?|years?|dan|dana|sedmic[ae]|nedelj[ae]|mesec[ai]|mjesec[ai]|godin[ae]u?)\b`)
	actRelativeRX  = regexp.MustCompile(`(?i)\b` + actLead + `(day after tomorrow|prekosutra|tonight|večeras|veceras|today|tod|tomorrow|tmr|tom|danas|sutra)\b`)
	actWeekdayRX   = regexp.MustCompile(`(?i)\b` + actLead + `(?:(next|this|u|sljedeći|sledeći|sledeci|sljedeci|idući|iduci)\s+)?(` + actDayAlt + `)\b`)
	actNextUnitRX  = regexp.MustCompile(`(?i)\b(?:(?:by|for|until|due)\s+)?next\s+(week|month|year)\b`)
	actEndOfRX     = regexp.MustCompile(`(?i)\b(?:(?:by|for|until|due)\s+)?end of (?:the\s+)?(week|month|year)\b`)
	actInRX        = regexp.MustCompile(`(?i)\bin\s+(\d+|` + actNumAlt + `)\s*(days?|weeks?|months?|years?)\b`)
	actISORX       = regexp.MustCompile(`(?i)\b` + actLead + `(\d{4})-(\d{2})-(\d{2})\b`)
	actDMYRX       = regexp.MustCompile(`(?i)\b` + actLead + `(\d{1,2})[./](\d{1,2})(?:[./](\d{4}|\d{2}))?\.?`)
	actDayMonthRX  = regexp.MustCompile(`(?i)\b` + actLead + `(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)?\.?\s+(?:of\s+)?(` + actMonthAlt + `)\b\.?(?:,?\s+(\d{4}))?`)
	actMonthDayRX  = regexp.MustCompile(`(?i)\b` + actLead + `(` + actMonthAlt + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?\b(?:,?\s+(\d{4}))?`)
	actOrdinalRX   = regexp.MustCompile(`(?i)\b(?:on|by|until|due)\s+the\s+(\d{1,2})(?:st|nd|rd|th)\b`)
)

func actAlternation[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, regexp.QuoteMeta(k))
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return strings.Join(keys, "|")
}

// actDateCand is one date phrase found in a task line.
type actDateCand struct {
	start, end int
	day        func() (time.Time, bool)
}

// actTaskDue finds the leftmost date in a task line: the title without it,
// and the day (zero when the line names none).
func actTaskDue(text string, now time.Time) (string, time.Time) {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	addDays := func(n int) time.Time { return today.AddDate(0, 0, n) }
	valid := func(y int, m time.Month, d int) (time.Time, bool) {
		if m < 1 || m > 12 || d < 1 || d > 31 {
			return time.Time{}, false
		}
		t := time.Date(y, m, d, 0, 0, 0, 0, loc)
		return t, t.Day() == d
	}
	// a day (and month) without a year: the next one that has not passed
	upcoming := func(y int, m time.Month, d int) (time.Time, bool) {
		if y > 0 {
			return valid(y, m, d)
		}
		if m == 0 {
			if t, ok := valid(today.Year(), today.Month(), d); ok && !t.Before(today) {
				return t, true
			}
			next := today.AddDate(0, 1, 1-today.Day())
			return valid(next.Year(), next.Month(), d)
		}
		if t, ok := valid(today.Year(), m, d); ok && !t.Before(today) {
			return t, true
		}
		return valid(today.Year()+1, m, d)
	}
	number := func(s string) int {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		if n, ok := actSmallNumbers[strings.ToLower(s)]; ok {
			return n
		}
		return 1
	}

	// repeats are not dates
	var masked [][]int
	masked = append(masked, actEveryDaysRX.FindAllStringIndex(text, -1)...)
	masked = append(masked, actEveryUnitRX.FindAllStringIndex(text, -1)...)
	free := func(start, end int) bool {
		for _, m := range masked {
			if start < m[1] && m[0] < end {
				return false
			}
		}
		return true
	}
	var cands []actDateCand
	each := func(re *regexp.Regexp, f func(g []string, at []int) (func() (time.Time, bool), bool)) {
		for _, at := range re.FindAllStringSubmatchIndex(text, -1) {
			if !free(at[0], at[1]) {
				continue
			}
			g := make([]string, len(at)/2)
			for i := range g {
				if at[2*i] >= 0 {
					g[i] = text[at[2*i]:at[2*i+1]]
				}
			}
			if fn, ok := f(g, at); ok {
				cands = append(cands, actDateCand{at[0], at[1], fn})
			}
		}
	}
	each(actRelativeRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		w := strings.ToLower(g[1])
		return func() (time.Time, bool) {
			switch w {
			case "today", "tod", "danas", "tonight", "večeras", "veceras":
				return today, true
			case "tomorrow", "tmr", "tom", "sutra":
				return addDays(1), true
			}
			return addDays(2), true
		}, true
	})
	each(actWeekdayRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		wd, ok := actWeekdays[strings.ToLower(g[2])]
		mod := strings.ToLower(g[1])
		return func() (time.Time, bool) {
			delta := (int(wd) - int(today.Weekday()) + 7) % 7
			if delta == 0 && mod != "this" {
				delta = 7
			}
			return addDays(delta), true
		}, ok
	})
	each(actNextUnitRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		unit := strings.ToLower(g[1])
		return func() (time.Time, bool) {
			switch unit {
			case "week":
				delta := (int(time.Monday) - int(today.Weekday()) + 7) % 7
				if delta == 0 {
					delta = 7
				}
				return addDays(delta), true
			case "month":
				return today.AddDate(0, 1, 1-today.Day()), true
			}
			return time.Date(today.Year()+1, 1, 1, 0, 0, 0, 0, loc), true
		}, true
	})
	each(actEndOfRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		unit := strings.ToLower(g[1])
		return func() (time.Time, bool) {
			switch unit {
			case "week":
				return addDays((int(time.Sunday) - int(today.Weekday()) + 7) % 7), true
			case "month":
				return today.AddDate(0, 1, -today.Day()), true
			}
			return time.Date(today.Year(), 12, 31, 0, 0, 0, 0, loc), true
		}, true
	})
	each(actInRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		n, unit := number(g[1]), strings.ToLower(g[2])
		return func() (time.Time, bool) {
			switch unit[0] {
			case 'd':
				return addDays(n), true
			case 'w':
				return addDays(7 * n), true
			case 'm':
				return today.AddDate(0, n, 0), true
			}
			return today.AddDate(n, 0, 0), true
		}, true
	})
	each(actISORX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		y, _ := strconv.Atoi(g[1])
		m, _ := strconv.Atoi(g[2])
		d, _ := strconv.Atoi(g[3])
		return func() (time.Time, bool) { return valid(y, time.Month(m), d) }, true
	})
	each(actDMYRX, func(g []string, at []int) (func() (time.Time, bool), bool) {
		// not part of a longer number or a time ("3.505", "12.30:00")
		digitsEnd := at[5]
		if at[6] >= 0 {
			digitsEnd = at[7]
		}
		isDigit := func(i int) bool { return i < len(text) && text[i] >= '0' && text[i] <= '9' }
		if isDigit(digitsEnd) || (digitsEnd < len(text) && text[digitsEnd] == ':') || isDigit(at[1]) {
			return nil, false
		}
		d, _ := strconv.Atoi(g[1])
		m, _ := strconv.Atoi(g[2])
		y, _ := strconv.Atoi(g[3])
		if g[3] != "" && y < 100 {
			y += 2000
		}
		return func() (time.Time, bool) { return upcoming(y, time.Month(m), d) }, true
	})
	each(actDayMonthRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		d, _ := strconv.Atoi(g[1])
		y, _ := strconv.Atoi(g[3])
		m := actMonths[strings.ToLower(g[2])]
		return func() (time.Time, bool) { return upcoming(y, m, d) }, true
	})
	each(actMonthDayRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		d, _ := strconv.Atoi(g[2])
		y, _ := strconv.Atoi(g[3])
		m := actMonths[strings.ToLower(g[1])]
		return func() (time.Time, bool) { return upcoming(y, m, d) }, true
	})
	each(actOrdinalRX, func(g []string, _ []int) (func() (time.Time, bool), bool) {
		d, _ := strconv.Atoi(g[1])
		return func() (time.Time, bool) { return upcoming(0, 0, d) }, true
	})

	if len(cands) == 0 {
		return text, time.Time{}
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.start < best.start {
			best = c
		}
	}
	day, ok := best.day()
	if !ok {
		return text, time.Time{}
	}
	return strings.Join(strings.Fields(text[:best.start]+" "+text[best.end:]), " "), day
}

// ---- Pocket Money ----

// actMoneyAccount, actMoneyCategory and actMoneyTemplate are the parts of
// Pocket Money's rows the expense tool reads.
type actMoneyAccount struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Currency   string `json:"currency"`
	Archived   bool   `json:"archived"`
	Role       string `json:"role"`
	LastTxDate string `json:"last_tx_date"`
}

type actMoneyCategory struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Kind     string `json:"kind"`
	Archived bool   `json:"archived"`
}

type actMoneyTemplate struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Aliases     string   `json:"aliases"` // comma-separated
	Kind        string   `json:"kind"`
	Amount      *int64   `json:"amount"` // minor units of Currency (or the account's)
	Currency    string   `json:"currency"`
	AccountID   *int64   `json:"account_id"`
	ToAccountID *int64   `json:"to_account_id"`
	CategoryID  *int64   `json:"category_id"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	Payee       string   `json:"payee"`
}

// searchNames are the words a line may start with to name the template.
func (t *actMoneyTemplate) searchNames() []string {
	out := []string{}
	for _, n := range append([]string{t.Name, t.Description}, strings.Split(t.Aliases, ",")...) {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

type actMoneyData struct {
	Accounts   []actMoneyAccount
	Categories []actMoneyCategory
	Templates  []actMoneyTemplate
}

// defaultAccount is the account booked on most recently (savings aside), else the first.
func (d *actMoneyData) defaultAccount() *actMoneyAccount {
	var best *actMoneyAccount
	for i := range d.Accounts {
		a := &d.Accounts[i]
		if a.Kind != "savings" && a.LastTxDate != "" && (best == nil || a.LastTxDate > best.LastTxDate) {
			best = a
		}
	}
	if best == nil && len(d.Accounts) > 0 {
		best = &d.Accounts[0]
	}
	return best
}

func (d *actMoneyData) account(id int64) *actMoneyAccount {
	for i := range d.Accounts {
		if d.Accounts[i].ID == id {
			return &d.Accounts[i]
		}
	}
	return nil
}

func (d *actMoneyData) category(id int64) *actMoneyCategory {
	for i := range d.Categories {
		if d.Categories[i].ID == id {
			return &d.Categories[i]
		}
	}
	return nil
}

// actMoneyLoad reads the accounts the person can book on, their categories
// and templates. A link token may not read templates; then there are none
// and the line's first words name a category instead.
func (s *Server) actMoneyLoad(ctx context.Context, base, tok string) (*actMoneyData, error) {
	hdr := map[string]string{"Authorization": "Bearer " + tok}
	var (
		accts           []actMoneyAccount
		cats            []actMoneyCategory
		tpls            []actMoneyTemplate
		acctErr, catErr error
		wg              sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		acctErr = s.callJSON(ctx, "money", http.MethodGet, base+"/api/v1/accounts", hdr, nil, &accts)
	}()
	go func() {
		defer wg.Done()
		catErr = s.callJSON(ctx, "money", http.MethodGet, base+"/api/v1/categories", hdr, nil, &cats)
	}()
	go func() {
		defer wg.Done()
		if s.callJSON(ctx, "money", http.MethodGet, base+"/api/v1/templates", hdr, nil, &tpls) != nil {
			tpls = nil
		}
	}()
	wg.Wait()
	if acctErr != nil {
		return nil, acctErr
	}
	if catErr != nil {
		return nil, catErr
	}
	d := &actMoneyData{Accounts: []actMoneyAccount{}, Categories: []actMoneyCategory{}, Templates: []actMoneyTemplate{}}
	for _, a := range accts {
		if !a.Archived && a.Role != "viewer" {
			d.Accounts = append(d.Accounts, a)
		}
	}
	for _, c := range cats {
		if !c.Archived {
			d.Categories = append(d.Categories, c)
		}
	}
	d.Templates = append(d.Templates, tpls...)
	return d, nil
}

// handleToolExpenseOptions is what the confirm step offers:
//
//	{"accounts":[{"id":1,"name":"Cash","currency":"BAM","kind":"cash"}],
//	 "categories":[{"id":3,"name":"Food › Groceries","kind":"expense"}],
//	 "templates":[{"id":5,"name":"groceries","category":3,"account":1,"kind":"expense"}],
//	 "currency":"BAM", "default_account":1}
//
// Accounts are the ones the person can book on; "currency" is the default
// account's. Templates is empty when Money does not show them to a link token.
func (s *Server) handleToolExpenseOptions(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	base, tok, err := s.appToken(ctx, uid, "money", "transactions:read", "transactions:write")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	d, err := s.actMoneyLoad(cctx, base, tok)
	if err != nil {
		toolErr(w, err)
		return
	}
	type acct struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Currency string `json:"currency"`
		Kind     string `json:"kind"`
	}
	type cat struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	type tpl struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Category int64  `json:"category"`
		Account  int64  `json:"account"`
		Kind     string `json:"kind"`
	}
	out := struct {
		Accounts       []acct `json:"accounts"`
		Categories     []cat  `json:"categories"`
		Templates      []tpl  `json:"templates"`
		Currency       string `json:"currency"`
		DefaultAccount int64  `json:"default_account"`
	}{Accounts: []acct{}, Categories: []cat{}, Templates: []tpl{}}
	for _, a := range d.Accounts {
		out.Accounts = append(out.Accounts, acct{a.ID, a.Name, a.Currency, a.Kind})
	}
	for _, c := range d.Categories {
		out.Categories = append(out.Categories, cat{c.ID, firstNonEmpty(c.FullName, c.Name), c.Kind})
	}
	for _, t := range d.Templates {
		out.Templates = append(out.Templates, tpl{t.ID, t.Name, actDeref(t.CategoryID), actDeref(t.AccountID), firstNonEmpty(t.Kind, "expense")})
	}
	if a := d.defaultAccount(); a != nil {
		out.Currency, out.DefaultAccount = a.Currency, a.ID
	}
	writeJSON(w, http.StatusOK, out)
}

func actDeref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// actExpenseHint is what a line that cannot be read is told.
const actExpenseHint = "Write the line as template + amount + what for, e.g. groceries 24.50 bread"

// actExpenseLine is a line taken apart: what its first words name, the
// amount (with its currency when typed), the words left, a day shift.
type actExpenseLine struct {
	Template    *actMoneyTemplate
	Category    *actMoneyCategory
	Amount      float64 // in Currency, or the account's
	HasAmount   bool    // false: the template's own amount
	Currency    string  // ISO code as typed ("km" → BAM), "" = none typed
	Description string
	DayOffset   int
}

var actDayOffsetRX = regexp.MustCompile(`(?i)^([+-])([0-9]+)d$`)

// actParseExpense reads "[template] [amount][currency] [what for] [±Nd]" the
// way the app's assistant does: the longest run of leading words that names
// one template (or, failing that, one category) picks it; the amount may be
// a sum ("2*1,80"), with a comma or a dot. A line whose first words name
// nothing still goes when it has an amount: the first amount in it is taken
// and the other words describe it. The second value is what is wrong with
// the line, for the person.
func actParseExpense(text string, templates []actMoneyTemplate, categories []actMoneyCategory) (actExpenseLine, string) {
	var p actExpenseLine
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return p, actExpenseHint
	}
	if m := actDayOffsetRX.FindStringSubmatch(tokens[len(tokens)-1]); m != nil && len(tokens) > 1 {
		n, _ := strconv.Atoi(m[2])
		if m[1] == "-" {
			n = -n
		}
		p.DayOffset = n
		tokens = tokens[:len(tokens)-1]
	}

	consumed := 0
	tplNames := make([]actNamed, len(templates))
	for i := range templates {
		tplNames[i] = actNamed{name: templates[i].Name, names: templates[i].searchNames()}
	}
	if i, k, ok := actSpan(tokens, tplNames); ok {
		p.Template, consumed = &templates[i], k
	} else {
		catNames := make([]actNamed, len(categories))
		for i, c := range categories {
			names := []string{c.Name}
			if c.FullName != "" && c.FullName != c.Name {
				names = append(names, c.FullName)
			}
			catNames[i] = actNamed{name: c.Name, names: names}
		}
		if i, k, ok := actSpan(tokens, catNames); ok {
			p.Category, consumed = &categories[i], k
		}
	}

	rest := actAmountFirst(tokens[consumed:])
	amt, err := actParseAmount(strings.Join(rest, " "))
	if err != nil {
		return p, "Enter a valid amount using +, −, *, / or parentheses. The result must be above zero."
	}
	if amt != nil {
		p.Amount, p.HasAmount = amt.Value, true
		if amt.Currency != "" {
			p.Currency = firstNonEmpty(actCurrencyAliases[amt.Currency], strings.ToUpper(amt.Currency))
		}
		rest = strings.Fields(amt.Rest)
	}
	if !p.HasAmount && (p.Template == nil || p.Template.Amount == nil) {
		return p, actExpenseHint
	}
	p.Description = strings.Join(rest, " ")
	if p.Description == "" {
		switch {
		case p.Template != nil:
			p.Description = firstNonEmpty(p.Template.Description, p.Template.Name)
		case p.Category != nil:
			p.Description = p.Category.Name
		}
	}
	if p.Description == "" {
		return p, actExpenseHint
	}
	return p, ""
}

// actNamed is something a line's first words may name.
type actNamed struct {
	name  string   // its own name: an exact match always wins
	names []string // every name it answers to
}

// actSpan is which item the line's leading words name and how many words
// that took, longest run first; not ok when none does, or when a run names
// two at once.
func actSpan(tokens []string, items []actNamed) (int, int, bool) {
	for k := len(tokens); k >= 1; k-- {
		prefix := strings.ToLower(strings.Join(tokens[:k], " "))
		for i, it := range items {
			if strings.ToLower(it.name) == prefix {
				return i, k, true
			}
		}
		var cands, exact []int
		for i, it := range items {
			hit, same := false, false
			for _, n := range it.names {
				n = strings.ToLower(n)
				hit = hit || strings.HasPrefix(n, prefix)
				same = same || n == prefix
			}
			if hit {
				cands = append(cands, i)
			}
			if same {
				exact = append(exact, i)
			}
		}
		if len(exact) > 1 {
			return 0, 0, false
		}
		if len(exact) == 1 {
			return exact[0], k, true
		}
		if len(cands) == 1 {
			return cands[0], k, true
		}
	}
	return 0, 0, false
}

// actAmountFirst moves the first word that is an amount to the front. A bare
// count ("order 2 more cables") is not an amount unless it ends the line.
func actAmountFirst(tokens []string) []string {
	for i, tok := range tokens {
		r, _ := utf8.DecodeRuneInString(tok)
		if !unicode.IsDigit(r) && !strings.ContainsRune("$€.", r) {
			continue
		}
		if a, err := actParseAmount(tok); err != nil || a == nil {
			continue
		}
		marked := strings.ContainsAny(tok, ".,$€") || strings.IndexFunc(tok, unicode.IsLetter) >= 0
		if !marked && i != len(tokens)-1 {
			continue
		}
		if i == 0 {
			return tokens
		}
		out := append([]string{tok}, tokens[:i]...)
		return append(out, tokens[i+1:]...)
	}
	return tokens
}

// actCurrencyAliases are the currency words the app knows.
var actCurrencyAliases = map[string]string{
	"km": "BAM", "eur": "EUR", "euro": "EUR", "usd": "USD", "bam": "BAM", "rsd": "RSD", "$": "USD", "€": "EUR",
}

// actAmount is an amount read off the front of a line.
type actAmount struct {
	Value    float64
	Currency string // as typed, lower case ("km", "€", "chf")
	Rest     string
}

var (
	errActAmount   = errors.New("not a valid amount")
	actNumberRX    = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`)
	actAmountStart = "0123456789.,(+-−$€"
)

// actParseAmount reads an amount at the start of text: a number or a sum
// with + − * / and parentheses, a currency before or after it. Nil when the
// text does not start like an amount; an error when it does but is not one.
// A port of the app's TransactionAmount.
func actParseAmount(text string) (*actAmount, error) {
	c := []rune(text)
	if len(c) == 0 || !strings.ContainsRune(actAmountStart, c[0]) {
		return nil, nil
	}
	if len(c) > 4096 {
		return nil, errActAmount
	}
	p := &actAmountParser{c: c}
	return p.parse()
}

type actAmountParser struct {
	c []rune
	i int
}

func (p *actAmountParser) at(set string) bool {
	return p.i < len(p.c) && strings.ContainsRune(set, p.c[p.i])
}

func (p *actAmountParser) spaces() {
	for p.i < len(p.c) && unicode.IsSpace(p.c[p.i]) {
		p.i++
	}
}

func (p *actAmountParser) parse() (*actAmount, error) {
	prefix := ""
	if p.at("$€") {
		prefix = string(p.c[p.i])
		p.i++
	}
	v, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	end := p.i
	p.spaces()
	spaced := p.i > end
	suffix := ""
	if p.i < len(p.c) {
		start := p.i
		if p.at("$€") {
			p.i++
		} else {
			for p.i < len(p.c) && p.c[p.i] < utf8.RuneSelf && unicode.IsLetter(p.c[p.i]) {
				p.i++
			}
		}
		cand := strings.ToLower(string(p.c[start:p.i]))
		_, known := actCurrencyAliases[cand]
		if n := utf8.RuneCountInString(cand); known || (!spaced && n >= 2 && n <= 4) {
			suffix = cand
		} else {
			p.i = start
			if !spaced {
				return nil, errActAmount
			}
		}
	}
	if prefix != "" && suffix != "" && actCurrencyAliases[prefix] != firstNonEmpty(actCurrencyAliases[suffix], strings.ToUpper(suffix)) {
		return nil, errActAmount
	}
	// an attached currency ends at a word boundary
	if suffix != "" && p.i < len(p.c) && !unicode.IsSpace(p.c[p.i]) {
		return nil, errActAmount
	}
	p.spaces()
	if p.at("+-−*/×÷)%") {
		return nil, errActAmount
	}
	amount, _ := v.Float64()
	if math.IsInf(amount, 0) || math.IsNaN(amount) || amount <= 0 {
		return nil, errActAmount
	}
	return &actAmount{Value: amount, Currency: firstNonEmpty(prefix, suffix), Rest: string(p.c[p.i:])}, nil
}

func (p *actAmountParser) expr(depth int) (*big.Rat, error) {
	v, err := p.term(depth)
	if err != nil {
		return nil, err
	}
	for {
		end := p.i
		p.spaces()
		if !p.at("+-−") {
			p.i = end
			return v, nil
		}
		op := p.c[p.i]
		p.i++
		rhs, err := p.term(depth)
		if err != nil {
			return nil, err
		}
		if op == '+' {
			v = new(big.Rat).Add(v, rhs)
		} else {
			v = new(big.Rat).Sub(v, rhs)
		}
	}
}

func (p *actAmountParser) term(depth int) (*big.Rat, error) {
	v, err := p.factor(depth)
	if err != nil {
		return nil, err
	}
	for {
		end := p.i
		p.spaces()
		if !p.at("*/×÷") {
			p.i = end
			return v, nil
		}
		op := p.c[p.i]
		p.i++
		rhs, err := p.factor(depth)
		if err != nil {
			return nil, err
		}
		if op == '*' || op == '×' {
			v = new(big.Rat).Mul(v, rhs)
		} else {
			if rhs.Sign() == 0 {
				return nil, errActAmount
			}
			v = new(big.Rat).Quo(v, rhs)
		}
	}
}

func (p *actAmountParser) factor(depth int) (*big.Rat, error) {
	if depth >= 32 || p.i >= 512 {
		return nil, errActAmount
	}
	p.spaces()
	if p.i >= len(p.c) {
		return nil, errActAmount
	}
	if p.at("+-−") {
		neg := p.c[p.i] != '+'
		p.i++
		v, err := p.factor(depth + 1)
		if err != nil {
			return nil, err
		}
		if neg {
			v = new(big.Rat).Neg(v)
		}
		return v, nil
	}
	if p.c[p.i] == '(' {
		p.i++
		v, err := p.expr(depth + 1)
		if err != nil {
			return nil, err
		}
		p.spaces()
		if !p.at(")") {
			return nil, errActAmount
		}
		p.i++
		return v, nil
	}
	start := p.i
	for p.at("0123456789.,") {
		p.i++
	}
	num := strings.ReplaceAll(string(p.c[start:p.i]), ",", ".")
	if !actNumberRX.MatchString(num) || len(num) > 38 {
		return nil, errActAmount
	}
	v, ok := new(big.Rat).SetString(num)
	if !ok {
		return nil, errActAmount
	}
	return v, nil
}

// actDecimals is a currency's minor-unit exponent (2 for most).
func actDecimals(cur string) int {
	switch strings.ToUpper(cur) {
	case "BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "PYG", "RWF", "UGX", "UYI", "VND", "VUV", "XAF", "XOF", "XPF":
		return 0
	case "BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND":
		return 3
	}
	return 2
}

// actMinor turns an amount into minor units of a currency.
func actMinor(v float64, cur string) int64 {
	return int64(math.Round(v * math.Pow10(actDecimals(cur))))
}

// actMajor writes minor units as the amount people read ("24.50").
func actMajor(minor int64, cur string) string {
	return strconv.FormatFloat(float64(minor)/math.Pow10(actDecimals(cur)), 'f', actDecimals(cur), 64)
}

// handleToolExpense makes a line a Pocket Money transaction.
//
// In: {"text":"groceries 24.50 bread","line":"<the line as it is in the note, when text was edited>",
// "account_id":1,"category":3,"date":"YYYY-MM-DD","preview":false}. A non-zero
// account_id or category wins over what the line names; without them the
// template's are used, else the account booked on most recently. "preview":
// true answers {"parsed":{…}} and books nothing.
//
// Out: {"url":"<money>/transactions/77","markdown":"<line> · [→ Pocket Money: 24.50 BAM · 2026-10-20](…)",
// "transaction":{…as Money answered…},"parsed":{"kind","date","account_id","account","amount":"24.50",
// "currency","category_id","category","description","template"}}. A line that
// cannot be read is a 400 that says how to write it.
func (s *Server) handleToolExpense(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Text       string `json:"text"`
		Line       string `json:"line"`
		AccountID  int64  `json:"account_id"`
		Category   int64  `json:"category"`
		CategoryID int64  `json:"category_id"`
		Date       string `json:"date"`
		Preview    bool   `json:"preview"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" {
		writeErr(w, http.StatusBadRequest, actExpenseHint)
		return
	}
	day := time.Now()
	if in.Date != "" {
		d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(in.Date), time.Local)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		day = d
	}
	base, tok, err := s.appToken(ctx, uid, "money", "transactions:read", "transactions:write")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	d, err := s.actMoneyLoad(cctx, base, tok)
	if err != nil {
		toolErr(w, err)
		return
	}
	p, problem := actParseExpense(text, d.Templates, d.Categories)
	if problem != "" {
		writeErr(w, http.StatusBadRequest, problem)
		return
	}

	kind := "expense"
	var acctID, toAcctID, catID, tplID int64
	var payee string
	tags := []string{}
	if t := p.Template; t != nil {
		kind = firstNonEmpty(t.Kind, kind)
		acctID, toAcctID, catID, tplID = actDeref(t.AccountID), actDeref(t.ToAccountID), actDeref(t.CategoryID), t.ID
		payee = t.Payee
		tags = append(tags, t.Tags...)
	}
	if c := p.Category; c != nil {
		catID = c.ID
		if c.Kind == "income" {
			kind = "income"
		}
	}
	if in.AccountID > 0 {
		acctID = in.AccountID
	}
	if in.Category > 0 {
		catID = in.Category
	} else if in.CategoryID > 0 {
		catID = in.CategoryID
	}
	if acctID == 0 {
		if a := d.defaultAccount(); a != nil {
			acctID = a.ID
		}
	}
	if acctID == 0 {
		writeErr(w, http.StatusBadRequest, "Pocket Money has no account you can book on.")
		return
	}
	acctCur := ""
	if a := d.account(acctID); a != nil {
		acctCur = a.Currency
	}
	cur := p.Currency
	var minor int64
	if p.HasAmount {
		if cur == "" && p.Template != nil {
			cur = p.Template.Currency
		}
		minor = actMinor(p.Amount, firstNonEmpty(cur, acctCur))
	} else {
		minor, cur = *p.Template.Amount, p.Template.Currency
	}
	date := day.AddDate(0, 0, p.DayOffset).Format("2006-01-02")

	body := map[string]any{"kind": kind, "date": date, "account_id": acctID, "amount": minor, "description": p.Description, "source": "notes", "tags": tags}
	if cur != "" {
		body["currency"] = cur
	}
	if catID > 0 {
		body["category_id"] = catID
	}
	if toAcctID > 0 {
		body["to_account_id"] = toAcctID
	}
	if payee != "" {
		body["payee"] = payee
	}
	if tplID > 0 {
		body["template_id"] = tplID
	}
	parsed := map[string]any{"kind": kind, "date": date, "account_id": acctID, "amount": actMajor(minor, firstNonEmpty(cur, acctCur)),
		"currency": firstNonEmpty(cur, acctCur), "category_id": catID, "description": p.Description}
	if a := d.account(acctID); a != nil {
		parsed["account"] = a.Name
	}
	if c := d.category(catID); c != nil {
		parsed["category"] = firstNonEmpty(c.FullName, c.Name)
	}
	if p.Template != nil {
		parsed["template"] = p.Template.Name
	}
	if in.Preview {
		writeJSON(w, http.StatusOK, map[string]any{"parsed": parsed})
		return
	}

	tx, status, msg, err := s.actMoneyCreate(cctx, base, tok, body)
	if err != nil {
		toolErr(w, err)
		return
	}
	if status != 0 {
		writeErr(w, status, msg)
		return
	}
	var t struct {
		ID       int64  `json:"id"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Date     string `json:"date"`
	}
	_ = json.Unmarshal(tx, &t)
	if t.ID == 0 {
		writeErr(w, http.StatusBadGateway, "Pocket Money gave the transaction no id")
		return
	}
	var raw map[string]any
	_ = json.Unmarshal(tx, &raw)
	link := firstNonEmpty(s.appPublicURL(ctx, uid, "money"), base) + "/transactions/" + strconv.FormatInt(t.ID, 10)
	summary := strings.TrimSpace(actMajor(t.Amount, t.Currency)+" "+t.Currency) + " · " + firstNonEmpty(t.Date, date)
	line := firstNonEmpty(strings.TrimSpace(in.Line), text)
	writeJSON(w, http.StatusOK, map[string]any{"url": link, "markdown": actMarkdown(line, "Pocket Money: "+summary, link), "transaction": raw, "parsed": parsed})
}

// actMoneyCreate posts a transaction to Pocket Money. Money refusing the
// transaction itself (its 400) comes back as a status and message for the
// person; not reaching Money, or Money refusing the key, as err.
func (s *Server) actMoneyCreate(ctx context.Context, base, tok string, body map[string]any) (json.RawMessage, int, string, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/transactions", bytes.NewReader(raw))
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PocketNotes/"+Version)
	res, err := s.http.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("Pocket Money does not answer: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode < 300 {
		return data, 0, "", nil
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &e)
	switch {
	case res.StatusCode == http.StatusBadRequest && e.Error != "":
		return nil, http.StatusBadRequest, "Pocket Money: " + e.Error, nil
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, 0, "", fmt.Errorf("Pocket Money refused the key (HTTP %d)", res.StatusCode)
	case e.Error != "":
		return nil, 0, "", fmt.Errorf("Pocket Money: HTTP %d: %s", res.StatusCode, e.Error)
	}
	return nil, 0, "", fmt.Errorf("Pocket Money: HTTP %d", res.StatusCode)
}
