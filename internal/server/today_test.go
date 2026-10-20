package server

import (
	"testing"
	"time"
)

func TestParseVEvents(t *testing.T) {
	day := time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local)
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Standup\\, team\r\nDTSTART:" + day.Add(9*time.Hour).UTC().Format("20060102T150405Z") +
		"\r\nDTEND:" + day.Add(9*time.Hour+15*time.Minute).UTC().Format("20060102T150405Z") + "\r\nLOCATION:Room \r\n 4\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Birthday\r\nDTSTART;VALUE=DATE:20261020\r\nDTEND;VALUE=DATE:20261021\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Yesterday\r\nDTSTART;VALUE=DATE:20261019\r\nDTEND;VALUE=DATE:20261020\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Cancelled\r\nSTATUS:CANCELLED\r\nDTSTART;VALUE=DATE:20261020\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	evs := parseVEvents(ics, day)
	if len(evs) != 2 || evs[0].Title != "Standup, team" || evs[0].Location != "Room 4" || evs[0].AllDay || !evs[1].AllDay || evs[1].Title != "Birthday" {
		t.Fatalf("%+v", evs)
	}
}

func TestTodayPage(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("me@example.com")
	h.do(tok, "POST", "/api/v1/capture", map[string]any{"text": "first thought"}, nil)
	h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "Plan", "content": "- [ ] overdue thing due:2020-01-01\n- [ ] later"}, nil)
	var out struct {
		Note  *struct{ Content string } `json:"note"`
		Week  []todayDay                `json:"week"`
		Tasks struct {
			Overdue []TaskLine `json:"overdue"`
			Open    int        `json:"open"`
		} `json:"tasks"`
		Edited []todayNote `json:"edited"`
	}
	if c := h.do(tok, "GET", "/api/v1/today", nil, &out); c != 200 {
		t.Fatalf("today: %d", c)
	}
	if out.Note == nil || len(out.Week) != 7 || len(out.Tasks.Overdue) != 1 || out.Tasks.Open != 2 || len(out.Edited) != 1 || !out.Edited[0].New {
		t.Fatalf("%+v", out)
	}
	var ag map[string]any
	h.do(tok, "GET", "/api/v1/today/agenda", nil, &ag)
	if ag["connected"] != false {
		t.Fatalf("agenda without Nextcloud: %v", ag)
	}
}
