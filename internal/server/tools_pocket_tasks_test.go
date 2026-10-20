package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPocketTaskDestination(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("alice@example.com")
	calls := 0
	tasks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tasks-key" {
			t.Error("wrong credential")
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode([]map[string]any{{"id": 3, "name": "Home"}, {"id": 4, "name": "Old", "archived": true}})
			return
		}
		calls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["title"] != "Fix the shelf" || body["list_id"] != float64(3) || body["due"] != "2026-10-21" {
			t.Errorf("task body: %+v", body)
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": 7, "list_id": 3, "title": body["title"]})
	}))
	defer tasks.Close()
	actConnect(t, h, u.ID, map[string]string{"app.tasks.mode": "direct", "app.tasks.url": tasks.URL, "app.tasks.key": "tasks-key"})
	var targets struct {
		Lists   []actTaskList
		Default string
	}
	if code := h.do(tok, "GET", "/api/v1/tools/task/targets?destination=pocket-tasks", nil, &targets); code != 200 || len(targets.Lists) != 1 || targets.Default != "pocket-tasks" {
		t.Fatalf("targets %d %+v", code, targets)
	}
	var out map[string]any
	code := h.do(tok, "POST", "/api/v1/tools/task", map[string]any{"text": "Fix the shelf", "list": "3", "due": "2026-10-21", "destination": "pocket-tasks"}, &out)
	if code != 201 || calls != 1 || !strings.Contains(out["markdown"].(string), "/list/3?task=7") {
		t.Fatalf("create %d %+v calls=%d", code, out, calls)
	}
	actConnect(t, h, u.ID, map[string]string{"app.tasks.mode": "off"})
	if code := h.do(tok, "POST", "/api/v1/tools/task", map[string]any{"text": "Fix the shelf", "destination": "pocket-tasks"}, &out); code == 201 || calls != 1 {
		t.Fatal("disabled destination still wrote", code, calls)
	}
}
