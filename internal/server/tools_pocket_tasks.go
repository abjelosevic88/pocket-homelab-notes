package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// pocketTaskTargets is an explicit Pocket Tasks destination. A refused Core
// link remains a refusal; it never falls back to a direct Nextcloud account.
func (s *Server) pocketTaskTargets(w http.ResponseWriter, r *http.Request) {
	c, err := s.appCred(r.Context(), me(r), "tasks", "tasks:read", "tasks:write")
	if err != nil {
		toolErr(w, err)
		return
	}
	var lists []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Color    string `json:"color"`
		Archived bool   `json:"archived"`
	}
	if err = s.callJSON(r.Context(), "tasks", http.MethodGet, c.API("/api/v1/lists"), map[string]string{"Authorization": "Bearer " + c.Key}, nil, &lists); err != nil {
		toolErr(w, err)
		return
	}
	out := []actTaskList{}
	for _, l := range lists {
		if !l.Archived {
			out = append(out, actTaskList{ID: strconv.FormatInt(l.ID, 10), Name: l.Name, Color: l.Color})
		}
	}
	writeJSON(w, 200, map[string]any{"lists": out, "boards": []actDeckBoard{}, "default": "pocket-tasks"})
}

func (s *Server) createPocketTask(w http.ResponseWriter, r *http.Request, title, line, list string, due time.Time) {
	c, err := s.appCred(r.Context(), me(r), "tasks", "tasks:write")
	if err != nil {
		toolErr(w, err)
		return
	}
	body := map[string]any{"title": title}
	if list != "" {
		id, err := strconv.ParseInt(list, 10, 64)
		if err != nil || id <= 0 {
			writeErr(w, 400, "list must be a Pocket Tasks list id")
			return
		}
		body["list_id"] = id
	}
	if !due.IsZero() {
		body["due"] = due.Format("2006-01-02")
	}
	var task struct {
		ID     int64  `json:"id"`
		ListID int64  `json:"list_id"`
		Title  string `json:"title"`
	}
	if err = s.callJSON(r.Context(), "tasks", http.MethodPost, c.API("/api/v1/tasks"), map[string]string{"Authorization": "Bearer " + c.Key}, body, &task); err != nil {
		toolErr(w, err)
		return
	}
	if task.ID <= 0 {
		writeErr(w, 502, "Pocket Tasks returned no task id")
		return
	}
	link := c.Link(fmt.Sprintf("/list/%d?task=%d", task.ListID, task.ID))
	out := map[string]any{"url": link, "markdown": actMarkdown(line, "Pocket Tasks", link), "title": task.Title, "destination": "pocket-tasks"}
	if !due.IsZero() {
		out["due"] = due.Format("2006-01-02")
	}
	writeJSON(w, 201, out)
}
