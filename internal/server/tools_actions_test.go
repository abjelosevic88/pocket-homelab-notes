package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- fakes: Nextcloud (CalDAV + Deck) and Pocket Money ----

type actFakeNC struct {
	*httptest.Server
	mu        sync.Mutex
	noLists   bool // no task list at all: Deck is the way
	deckDown  bool
	auth      []string
	putPath   string
	putBody   string
	putHeader http.Header
	cardPath  string
	card      map[string]any
}

const actPropfindAnswer = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav" xmlns:x1="http://apple.com/ns/ical/">
 <d:response><d:href>/remote.php/dav/calendars/alice/</d:href>
  <d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/calendars/alice/personal/</d:href>
  <d:propstat><d:prop><d:displayname>Personal</d:displayname><d:resourcetype><d:collection/><cal:calendar/></d:resourcetype>
   <d:current-user-privilege-set><d:privilege><d:read/></d:privilege><d:privilege><d:write/></d:privilege></d:current-user-privilege-set>
   <cal:supported-calendar-component-set><cal:comp name="VEVENT"/><cal:comp name="VTODO"/></cal:supported-calendar-component-set>
   <x1:calendar-color>#0082c9ff</x1:calendar-color><x1:calendar-order>2</x1:calendar-order></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
  <d:propstat><d:prop><d:sync-token/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/calendars/alice/home/</d:href>
  <d:propstat><d:prop><d:displayname>Home</d:displayname><d:resourcetype><d:collection/><cal:calendar/></d:resourcetype>
   <d:current-user-privilege-set><d:privilege><d:write-content/></d:privilege></d:current-user-privilege-set>
   <cal:supported-calendar-component-set><cal:comp name="VTODO"/></cal:supported-calendar-component-set>
   <x1:calendar-order>1</x1:calendar-order></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/calendars/alice/contact_birthdays/</d:href>
  <d:propstat><d:prop><d:displayname>Birthdays</d:displayname><d:resourcetype><d:collection/><cal:calendar/></d:resourcetype>
   <d:current-user-privilege-set><d:privilege><d:write/></d:privilege></d:current-user-privilege-set>
   <cal:supported-calendar-component-set><cal:comp name="VEVENT"/></cal:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/calendars/alice/team_shared_by_bob/</d:href>
  <d:propstat><d:prop><d:displayname>Team</d:displayname><d:resourcetype><d:collection/><cal:calendar/></d:resourcetype>
   <d:current-user-privilege-set><d:privilege><d:read/></d:privilege></d:current-user-privilege-set>
   <cal:supported-calendar-component-set><cal:comp name="VTODO"/></cal:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/calendars/alice/old/</d:href>
  <d:propstat><d:prop><d:displayname>Old</d:displayname><d:resourcetype><d:collection/><cal:calendar/><x2:deleted-calendar xmlns:x2="http://nextcloud.com/ns"/></d:resourcetype>
   <d:current-user-privilege-set><d:privilege><d:write/></d:privilege></d:current-user-privilege-set>
   <cal:supported-calendar-component-set><cal:comp name="VTODO"/></cal:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`

func newActFakeNC(t *testing.T) *actFakeNC {
	f := &actFakeNC{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		user, pass, _ := r.BasicAuth()
		if user != "alice" || pass != "app-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		deck := "/index.php/apps/deck/api/v1.0"
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/remote.php/dav/calendars/alice/":
			body, _ := io.ReadAll(r.Body)
			if r.Header.Get("Depth") != "1" || !strings.Contains(string(body), "supported-calendar-component-set") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(207)
			if f.noLists {
				io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/remote.php/dav/calendars/alice/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
				return
			}
			io.WriteString(w, actPropfindAnswer)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/remote.php/dav/calendars/alice/"):
			body, _ := io.ReadAll(r.Body)
			f.putPath, f.putBody, f.putHeader = r.URL.Path, string(body), r.Header.Clone()
			w.WriteHeader(http.StatusCreated)
		case strings.HasPrefix(r.URL.Path, deck):
			if r.Header.Get("OCS-APIRequest") != "true" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if f.deckDown {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			p := strings.TrimPrefix(r.URL.Path, deck)
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && p == "/boards":
				if r.URL.Query().Get("details") != "true" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				io.WriteString(w, `[
					{"id":14,"title":"Read only","color":"aaaaaa","archived":false,"deletedAt":0,"permissions":{"PERMISSION_READ":true,"PERMISSION_EDIT":false}},
					{"id":12,"title":"Homelab","color":"0087c5","archived":false,"deletedAt":0,"permissions":{"PERMISSION_READ":true,"PERMISSION_EDIT":true}},
					{"id":13,"title":"Archived","color":"bbbbbb","archived":true,"deletedAt":0,"permissions":{"PERMISSION_EDIT":true}},
					{"id":15,"title":"Deleted","color":"cccccc","archived":false,"deletedAt":1700000000,"permissions":{"PERMISSION_EDIT":true}}]`)
			case r.Method == http.MethodGet && p == "/boards/12/stacks":
				io.WriteString(w, `[{"id":35,"title":"Done","order":2,"deletedAt":0,"cards":[]},{"id":34,"title":"To do","order":1,"deletedAt":0},{"id":36,"title":"Gone","order":0,"deletedAt":5}]`)
			case r.Method == http.MethodPost && p == "/boards/12/stacks/34/cards":
				f.cardPath = p
				_ = json.NewDecoder(r.Body).Decode(&f.card)
				io.WriteString(w, `{"id":56,"title":"x","stackId":34,"order":999}`)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

type actFakeMoney struct {
	*httptest.Server
	mu          sync.Mutex
	noTemplates bool // a link token: templates are outside its scope
	created     []map[string]any
}

func newActFakeMoney(t *testing.T) *actFakeMoney {
	f := &actFakeMoney{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer money-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/accounts":
			io.WriteString(w, `[
				{"id":1,"name":"Cash","kind":"cash","currency":"BAM","role":"owner","last_tx_date":"2026-10-01"},
				{"id":2,"name":"Visa","kind":"checking","currency":"BAM","role":"owner","last_tx_date":"2026-10-19"},
				{"id":3,"name":"Joint","kind":"checking","currency":"EUR","role":"viewer","last_tx_date":"2026-10-20"},
				{"id":4,"name":"Old","kind":"cash","currency":"BAM","role":"owner","archived":true}]`)
		case "GET /api/v1/categories":
			io.WriteString(w, `[
				{"id":10,"name":"Groceries","full_name":"Groceries","kind":"expense"},
				{"id":11,"name":"Kafa","full_name":"Food › Kafa","kind":"expense"},
				{"id":12,"name":"Salary","full_name":"Salary","kind":"income"},
				{"id":13,"name":"Gone","kind":"expense","archived":true}]`)
		case "GET /api/v1/templates":
			if f.noTemplates {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"error":"outside the link's scope"}`)
				return
			}
			io.WriteString(w, `[
				{"id":5,"name":"groceries","aliases":"namirnice, market","kind":"expense","account_id":1,"category_id":10,"tags":["food"],"description":"","payee":"Konzum"},
				{"id":6,"name":"parking","aliases":"","kind":"expense","amount":200,"currency":"","account_id":1,"category_id":null,"tags":[],"description":"Parking"}]`)
		case "POST /api/v1/transactions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.created = append(f.created, body)
			if body["amount"] == nil || body["amount"].(float64) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"error":"amount is required"}`)
				return
			}
			cur := "BAM"
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 77, "kind": body["kind"], "amount": body["amount"], "currency": cur,
				"date": body["date"], "description": body["description"], "source": "notes", "account_id": body["account_id"]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func actConnect(t *testing.T, h *harness, uid int64, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := h.st.SetUserSetting(context.Background(), uid, k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func actConnectNC(t *testing.T, h *harness, uid int64, base string) {
	actConnect(t, h, uid, map[string]string{"svc.nextcloud.mode": "direct", "svc.nextcloud.url": base, "svc.nextcloud.key": "app-pass", "svc.nextcloud.user": "alice"})
}

func actConnectMoney(t *testing.T, h *harness, uid int64, base string) {
	actConnect(t, h, uid, map[string]string{"app.money.mode": "direct", "app.money.url": base, "app.money.key": "money-key"})
}

// ---- tasks and cards ----

func TestActTaskTargets(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	nc := newActFakeNC(t)
	actConnectNC(t, h, u.ID, nc.URL)

	var out struct {
		Lists   []map[string]any  `json:"lists"`
		Boards  []actDeckBoard    `json:"boards"`
		Default string            `json:"default"`
		Errors  map[string]string `json:"errors"`
	}
	if st := h.do(tok, "GET", "/api/v1/tools/task/targets", nil, &out); st != 200 {
		t.Fatalf("status %d", st)
	}
	if len(out.Lists) != 2 || out.Lists[0]["id"] != "home" || out.Lists[1]["id"] != "personal" {
		t.Fatalf("lists: %+v", out.Lists)
	}
	if out.Lists[1]["name"] != "Personal" || out.Lists[1]["color"] != "#0082c9" {
		t.Fatalf("personal list: %+v", out.Lists[1])
	}
	if len(out.Boards) != 1 || out.Boards[0].ID != 12 || out.Boards[0].Title != "Homelab" {
		t.Fatalf("boards: %+v", out.Boards)
	}
	if st := out.Boards[0].Stacks; len(st) != 2 || st[0].ID != 34 || st[0].Title != "To do" || st[1].ID != 35 {
		t.Fatalf("stacks: %+v", st)
	}
	if out.Default != "tasks" || out.Errors != nil {
		t.Fatalf("default %q errors %v", out.Default, out.Errors)
	}
}

func TestActTaskTargetsDeckDown(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	nc := newActFakeNC(t)
	nc.deckDown = true
	actConnectNC(t, h, u.ID, nc.URL)

	var out struct {
		Lists  []map[string]any  `json:"lists"`
		Boards []actDeckBoard    `json:"boards"`
		Errors map[string]string `json:"errors"`
	}
	if st := h.do(tok, "GET", "/api/v1/tools/task/targets", nil, &out); st != 200 {
		t.Fatalf("status %d", st)
	}
	if len(out.Lists) != 2 || out.Boards == nil || len(out.Boards) != 0 || out.Errors["deck"] == "" || out.Errors["tasks"] != "" {
		t.Fatalf("got %+v", out)
	}
}

func TestActTaskToTasks(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	nc := newActFakeNC(t)
	actConnectNC(t, h, u.ID, nc.URL)

	var out map[string]string
	st := h.do(tok, "POST", "/api/v1/tools/task", map[string]any{"text": "Check the UPS tomorrow", "destination": "tasks", "list": "personal"}, &out)
	if st != 200 {
		t.Fatalf("status %d: %v", st, out)
	}
	if !strings.HasPrefix(nc.putPath, "/remote.php/dav/calendars/alice/personal/") || !strings.HasSuffix(nc.putPath, ".ics") {
		t.Fatalf("PUT to %q", nc.putPath)
	}
	uid := strings.TrimSuffix(strings.TrimPrefix(nc.putPath, "/remote.php/dav/calendars/alice/personal/"), ".ics")
	if nc.putHeader.Get("If-None-Match") != "*" || !strings.HasPrefix(nc.putHeader.Get("Content-Type"), "text/calendar") {
		t.Fatalf("headers %v", nc.putHeader)
	}
	tomorrow := time.Now().AddDate(0, 0, 1).Format("20060102")
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "BEGIN:VTODO\r\n", "UID:" + uid + "\r\n", "SUMMARY:Check the UPS\r\n",
		"DUE;VALUE=DATE:" + tomorrow + "\r\n", "STATUS:NEEDS-ACTION\r\n", "CREATED:", "DTSTAMP:", "END:VTODO\r\nEND:VCALENDAR\r\n"} {
		if !strings.Contains(nc.putBody, want) {
			t.Fatalf("VTODO lacks %q:\n%s", want, nc.putBody)
		}
	}
	link := nc.URL + "/apps/tasks/#/calendars/personal/tasks/" + uid + ".ics"
	if out["url"] != link {
		t.Fatalf("url %q, want %q", out["url"], link)
	}
	if want := "Check the UPS tomorrow · [→ Tasks](" + link + ")"; out["markdown"] != want {
		t.Fatalf("markdown %q, want %q", out["markdown"], want)
	}
	if out["title"] != "Check the UPS" || out["due"] != time.Now().AddDate(0, 0, 1).Format("2006-01-02") || out["destination"] != "tasks" {
		t.Fatalf("got %v", out)
	}
}

func TestActTaskFirstListAndEditedText(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	nc := newActFakeNC(t)
	actConnectNC(t, h, u.ID, nc.URL)

	var out map[string]string
	st := h.do(tok, "POST", "/api/v1/tools/task", map[string]any{"text": "Order fan, quiet one", "line": "- order a fan [noisy]"}, &out)
	if st != 200 {
		t.Fatalf("status %d: %v", st, out)
	}
	if !strings.HasPrefix(nc.putPath, "/remote.php/dav/calendars/alice/home/") {
		t.Fatalf("PUT to %q", nc.putPath)
	}
	if !strings.Contains(nc.putBody, `SUMMARY:Order fan\, quiet one`) || strings.Contains(nc.putBody, "DUE") {
		t.Fatalf("VTODO:\n%s", nc.putBody)
	}
	if !strings.HasPrefix(out["markdown"], `- order a fan \[noisy\] · [→ Tasks](`+nc.URL+"/apps/tasks/#/calendars/home/tasks/") {
		t.Fatalf("markdown %q", out["markdown"])
	}
}

func TestActTaskToDeck(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	nc := newActFakeNC(t)
	actConnectNC(t, h, u.ID, nc.URL)

	var out map[string]string
	st := h.do(tok, "POST", "/api/v1/tools/task", map[string]any{"text": "Replace [the] fan", "destination": "deck", "board": 12, "stack": 34, "due": "2026-11-02"}, &out)
	if st != 200 {
		t.Fatalf("status %d: %v", st, out)
	}
	if nc.card["title"] != "Replace [the] fan" || nc.card["type"] != "plain" || nc.card["order"] != float64(999) {
		t.Fatalf("card %v", nc.card)
	}
	if d, _ := nc.card["duedate"].(string); !strings.HasPrefix(d, "2026-11-02T23:59:00") {
		t.Fatalf("duedate %v", nc.card["duedate"])
	}
	link := nc.URL + "/index.php/apps/deck/#/board/12/card/56"
	if out["url"] != link || out["markdown"] != `Replace \[the\] fan · [→ Deck](`+link+")" {
		t.Fatalf("got %v", out)
	}
	if out["due"] != "2026-11-02" || out["destination"] != "deck" {
		t.Fatalf("got %v", out)
	}
}

func TestActTaskFallsToDeck(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	nc := newActFakeNC(t)
	nc.noLists = true
	actConnectNC(t, h, u.ID, nc.URL)

	var targets map[string]any
	h.do(tok, "GET", "/api/v1/tools/task/targets", nil, &targets)
	if targets["default"] != "deck" {
		t.Fatalf("default %v", targets["default"])
	}
	var out map[string]string
	if st := h.do(tok, "POST", "/api/v1/tools/task", map[string]any{"text": "Check backups"}, &out); st != 200 {
		t.Fatalf("status %d: %v", st, out)
	}
	if nc.cardPath != "/boards/12/stacks/34/cards" || nc.card["duedate"] != nil || nc.putPath != "" {
		t.Fatalf("card %s %v, put %q", nc.cardPath, nc.card, nc.putPath)
	}
	if out["destination"] != "deck" || !strings.HasSuffix(out["markdown"], "[→ Deck]("+nc.URL+"/index.php/apps/deck/#/board/12/card/56)") {
		t.Fatalf("got %v", out)
	}
}

func TestActTaskBadInput(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	nc := newActFakeNC(t)
	actConnectNC(t, h, u.ID, nc.URL)
	for _, body := range []map[string]any{{"text": "  "}, {"text": "x", "destination": "mail"}, {"text": "x", "due": "tomorrow"}} {
		var out map[string]string
		if st := h.do(tok, "POST", "/api/v1/tools/task", body, &out); st != 400 || out["error"] == "" {
			t.Fatalf("%v: status %d %v", body, st, out)
		}
	}
}

func TestActNotConnected(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("alice@example.com")
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/tools/task/targets", nil},
		{"POST", "/api/v1/tools/task", map[string]any{"text": "Check the UPS"}},
		{"GET", "/api/v1/tools/expense/options", nil},
		{"POST", "/api/v1/tools/expense", map[string]any{"text": "groceries 24.50 bread"}},
	} {
		var out map[string]any
		if st := h.do(tok, c.method, c.path, c.body, &out); st != http.StatusPreconditionFailed || out["not_connected"] == nil {
			t.Fatalf("%s %s: %d %v", c.method, c.path, st, out)
		}
	}
}

func TestActTaskDue(t *testing.T) {
	now := time.Date(2026, 10, 20, 15, 4, 0, 0, time.Local) // a Tuesday
	for _, c := range []struct{ in, title, due string }{
		{"Check the UPS tomorrow", "Check the UPS", "2026-10-21"},
		{"Call mom fri", "Call mom", "2026-10-23"},
		{"standup tuesday", "standup", "2026-10-27"},
		{"standup this tue", "standup", "2026-10-20"},
		{"pay rent next week", "pay rent", "2026-10-26"},
		{"renew the domain 12.11.", "renew the domain", "2026-11-12"},
		{"dentist on 2026-11-02 at 9", "dentist at 9", "2026-11-02"},
		{"trip planning in 2 weeks", "trip planning", "2026-11-03"},
		{"nov 5 meeting", "meeting", "2026-11-05"},
		{"report due end of month", "report", "2026-10-31"},
		{"birthday 3 jan", "birthday", "2027-01-03"},
		{"spend 3.50 on coffee", "spend 3.50 on coffee", ""},
		{"water plants every fri", "water plants every fri", ""},
		{"Check the UPS", "Check the UPS", ""},
	} {
		title, due := actTaskDue(c.in, now)
		got := ""
		if !due.IsZero() {
			got = due.Format("2006-01-02")
		}
		if title != c.title || got != c.due {
			t.Errorf("%q: got %q %q, want %q %q", c.in, title, got, c.title, c.due)
		}
	}
}

func TestActVTODOFolds(t *testing.T) {
	ics := actVTODO("ABC", strings.Repeat("ž", 60), time.Time{}, time.Now())
	for _, l := range strings.Split(ics, "\r\n") {
		if len(l) > 75 {
			t.Fatalf("line of %d octets: %q", len(l), l)
		}
	}
	if !strings.Contains(ics, "\r\n ") {
		t.Fatal("long SUMMARY not folded")
	}
}

// ---- transactions ----

var actTemplates = []actMoneyTemplate{
	{ID: 5, Name: "groceries", Aliases: "namirnice, market", Kind: "expense"},
	{ID: 6, Name: "parking", Amount: func() *int64 { n := int64(200); return &n }(), Description: "Parking"},
	{ID: 7, Name: "cg hrana"},
	{ID: 8, Name: "Hrana"},
	{ID: 9, Name: "Hrana extra"},
}

var actCategories = []actMoneyCategory{{ID: 10, Name: "Groceries"}, {ID: 11, Name: "Kafa", FullName: "Food › Kafa"}}

func TestActParseExpense(t *testing.T) {
	for _, c := range []struct {
		in       string
		tpl, cat int64
		amount   float64
		cur      string
		desc     string
		offset   int
	}{
		{"groceries 24.50 bread", 5, 0, 24.5, "", "bread", 0},
		{"Groceries 24,50 bread and milk", 5, 0, 24.5, "", "bread and milk", 0},
		{"market 10km", 5, 0, 10, "BAM", "groceries", 0},
		{"namirnice €12 fruit -1d", 5, 0, 12, "EUR", "fruit", -1},
		{"gro 2*1,80 eur rolls", 5, 0, 3.6, "EUR", "rolls", 0},
		{"groceries bread 3.50", 5, 0, 3.5, "", "bread", 0},
		{"parking", 6, 0, 0, "", "Parking", 0},
		{"cg h 20", 7, 0, 20, "", "cg hrana", 0},
		{"hrana 5", 8, 0, 5, "", "Hrana", 0},
		{"kafa 3,50", 0, 11, 3.5, "", "Kafa", 0},
		{"24.50 groceries bread", 0, 0, 24.5, "", "groceries bread", 0},
		{"Coffee 3.50 at the station +2d", 0, 0, 3.5, "", "Coffee at the station", 2},
	} {
		p, problem := actParseExpense(c.in, actTemplates, actCategories)
		if problem != "" {
			t.Errorf("%q: %s", c.in, problem)
			continue
		}
		var tpl, cat int64
		if p.Template != nil {
			tpl = p.Template.ID
		}
		if p.Category != nil {
			cat = p.Category.ID
		}
		if tpl != c.tpl || cat != c.cat || p.Amount != c.amount || p.Currency != c.cur || p.Description != c.desc || p.DayOffset != c.offset {
			t.Errorf("%q: got tpl %d cat %d %v %q %q %+d", c.in, tpl, cat, p.Amount, p.Currency, p.Description, p.DayOffset)
		}
	}
	for _, in := range []string{"", "just some words", "groceries bread", "Order 2 more cables", "groceries 3+", "groceries 0", "groceries 5/0"} {
		if _, problem := actParseExpense(in, actTemplates, actCategories); problem == "" {
			t.Errorf("%q: parsed", in)
		}
	}
}

func TestActParseAmount(t *testing.T) {
	for _, c := range []struct {
		in   string
		v    float64
		cur  string
		rest string
		bad  bool
	}{
		{in: "24.50 bread", v: 24.5, rest: "bread"},
		{in: "3,50", v: 3.5},
		{in: "(2+3)*2 eur x", v: 10, cur: "eur", rest: "x"},
		{in: "20chf lunch", v: 20, cur: "chf", rest: "lunch"},
		{in: "$5 tip", v: 5, cur: "$", rest: "tip"},
		{in: "$5eur", bad: true},
		{in: "5x", bad: true},
		{in: "1,234.50", bad: true},
		{in: "-3", bad: true},
	} {
		a, err := actParseAmount(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q: no error", c.in)
			}
			continue
		}
		if err != nil || a == nil || a.Value != c.v || a.Currency != c.cur || a.Rest != c.rest {
			t.Errorf("%q: %+v %v", c.in, a, err)
		}
	}
	if a, err := actParseAmount("bread 3"); a != nil || err != nil {
		t.Errorf("words first: %+v %v", a, err)
	}
}

func TestActExpense(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	m := newActFakeMoney(t)
	actConnectMoney(t, h, u.ID, m.URL)

	var out struct {
		URL         string         `json:"url"`
		Markdown    string         `json:"markdown"`
		Transaction map[string]any `json:"transaction"`
		Parsed      map[string]any `json:"parsed"`
	}
	st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": "groceries 24.50 bread", "date": "2026-10-20"}, &out)
	if st != 200 {
		t.Fatalf("status %d: %+v", st, out)
	}
	if len(m.created) != 1 {
		t.Fatalf("created %v", m.created)
	}
	b := m.created[0]
	if b["amount"] != float64(2450) || b["account_id"] != float64(1) || b["category_id"] != float64(10) || b["description"] != "bread" ||
		b["source"] != "notes" || b["kind"] != "expense" || b["date"] != "2026-10-20" || b["template_id"] != float64(5) || b["payee"] != "Konzum" {
		t.Fatalf("body %v", b)
	}
	if b["currency"] != nil {
		t.Fatalf("no currency was typed: %v", b["currency"])
	}
	if out.URL != m.URL+"/transactions/77" {
		t.Fatalf("url %q", out.URL)
	}
	if want := "groceries 24.50 bread · [→ Pocket Money: 24.50 BAM · 2026-10-20](" + m.URL + "/transactions/77)"; out.Markdown != want {
		t.Fatalf("markdown %q, want %q", out.Markdown, want)
	}
	if out.Transaction["id"] != float64(77) || out.Parsed["template"] != "groceries" || out.Parsed["category"] != "Groceries" {
		t.Fatalf("got %+v", out)
	}
}

func TestActExpenseCategoryAndOverrides(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	m := newActFakeMoney(t)
	m.noTemplates = true
	actConnectMoney(t, h, u.ID, m.URL)

	// no templates (a link token): the first word names a category; the
	// account is the one booked on most recently
	var out map[string]any
	if st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": "kafa 3,50 -1d", "date": "2026-10-20"}, &out); st != 200 {
		t.Fatalf("status %d: %v", st, out)
	}
	b := m.created[0]
	if b["amount"] != float64(350) || b["account_id"] != float64(2) || b["category_id"] != float64(11) || b["description"] != "Kafa" || b["date"] != "2026-10-19" {
		t.Fatalf("body %v", b)
	}
	if out["markdown"] != "kafa 3,50 -1d · [→ Pocket Money: 3.50 BAM · 2026-10-19]("+m.URL+"/transactions/77)" {
		t.Fatalf("markdown %v", out["markdown"])
	}

	// the person's picks win over the line's
	if st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": "kafa 2 eur", "account_id": 1, "category": 10}, &out); st != 200 {
		t.Fatalf("status %d: %v", st, out)
	}
	b = m.created[1]
	if b["account_id"] != float64(1) || b["category_id"] != float64(10) || b["currency"] != "EUR" || b["amount"] != float64(200) {
		t.Fatalf("body %v", b)
	}
}

func TestActExpensePreviewAndRefusals(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	m := newActFakeMoney(t)
	actConnectMoney(t, h, u.ID, m.URL)

	var prev map[string]map[string]any
	if st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": "parking", "preview": true, "date": "2026-10-20"}, &prev); st != 200 {
		t.Fatalf("status %d", st)
	}
	if p := prev["parsed"]; p["amount"] != "2.00" || p["currency"] != "BAM" || p["description"] != "Parking" || p["account"] != "Cash" || len(m.created) != 0 {
		t.Fatalf("preview %v, created %v", prev, m.created)
	}

	for _, text := range []string{"just some words", "groceries bread", ""} {
		var out map[string]string
		if st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": text}, &out); st != 400 || out["error"] != actExpenseHint {
			t.Fatalf("%q: %d %v", text, st, out)
		}
	}
	var out map[string]string
	if st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": "groceries 3+"}, &out); st != 400 || !strings.Contains(out["error"], "valid amount") {
		t.Fatalf("bad amount: %d %v", st, out)
	}
	if st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": "groceries 3", "date": "20.10."}, &out); st != 400 {
		t.Fatalf("bad date: %d %v", st, out)
	}
	if len(m.created) != 0 {
		t.Fatalf("created %v", m.created)
	}

	// a wrong key is the service's failure, not the person's
	actConnect(t, h, u.ID, map[string]string{"app.money.key": "wrong"})
	if st := h.do(tok, "POST", "/api/v1/tools/expense", map[string]any{"text": "groceries 3"}, &out); st != http.StatusBadGateway {
		t.Fatalf("wrong key: %d %v", st, out)
	}
}

func TestActExpenseOptions(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	m := newActFakeMoney(t)
	actConnectMoney(t, h, u.ID, m.URL)

	var out struct {
		Accounts []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Currency string `json:"currency"`
		} `json:"accounts"`
		Categories []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"categories"`
		Templates []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Category int64  `json:"category"`
			Account  int64  `json:"account"`
		} `json:"templates"`
		Currency       string `json:"currency"`
		DefaultAccount int64  `json:"default_account"`
	}
	if st := h.do(tok, "GET", "/api/v1/tools/expense/options", nil, &out); st != 200 {
		t.Fatalf("status %d", st)
	}
	if len(out.Accounts) != 2 || out.Accounts[0].Name != "Cash" || out.Accounts[1].ID != 2 {
		t.Fatalf("accounts %+v", out.Accounts)
	}
	if len(out.Categories) != 3 || out.Categories[1].Name != "Food › Kafa" {
		t.Fatalf("categories %+v", out.Categories)
	}
	if len(out.Templates) != 2 || out.Templates[0].Name != "groceries" || out.Templates[0].Category != 10 || out.Templates[0].Account != 1 {
		t.Fatalf("templates %+v", out.Templates)
	}
	if out.Currency != "BAM" || out.DefaultAccount != 2 {
		t.Fatalf("currency %q default %d", out.Currency, out.DefaultAccount)
	}

	m.noTemplates = true
	var raw map[string]any
	if st := h.do(tok, "GET", "/api/v1/tools/expense/options", nil, &raw); st != 200 {
		t.Fatalf("status %d", st)
	}
	if tpls, ok := raw["templates"].([]any); !ok || len(tpls) != 0 {
		t.Fatalf("templates without scope: %v", raw["templates"])
	}
}
