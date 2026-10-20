package server

import (
	"context"
	"time"
)

// seedDemo fills an empty box with made-up notes so the UI can be tried
// without a storage to connect (DEMO_DATA=true).
func (s *Server) seedDemo(ctx context.Context) error {
	if n := s.store.Counts(ctx)["notes"]; n > 0 {
		return nil
	}
	email, name := "demo@localhost", "Demo"
	if s.cfg.AuthMode == "none" {
		email, name = "admin@localhost", "Admin" // the account AUTH_MODE=none signs everyone in as
	}
	u, err := s.firstOrSystemUser(ctx, email, name)
	if err != nil {
		return err
	}
	today := time.Now()
	day := func(d int) string { return today.AddDate(0, 0, d).Format("2006-01-02") }
	notes := []struct{ cat, title, body string }{
		{"", "Welcome", "# Welcome\n\nThis is **Pocket Notes**: Markdown notes kept as plain files where you choose.\n\n- Type `/` in the editor for blocks, diagrams, templates and homelab tools\n- Link notes with [[Server rebuild]] or [[Reading list]]\n- Tag anything with #ideas or #homelab\n\n::: info\nSettings → Storage moves your notes to Nextcloud, WebDAV or a folder on the server.\n:::\n"},
		{"Projects", "Server rebuild", "# Server rebuild\n\nPlan for the new box. #homelab\n\n## To do\n\n- [x] Order the case\n- [ ] Move Immich to the NVMe 📅 " + day(2) + "\n- [ ] Re-check the backup plan due:" + day(-1) + "\n- [ ] Write the runbook\n\n## Notes\n\n```mermaid\nflowchart LR\n  NAS --> Server --> Backups\n```\n\nSee also [[Reading list]].\n"},
		{"Projects", "Garden", "# Garden\n\n| Bed | Plant | Sown |\n| --- | --- | --- |\n| A | Tomatoes | " + day(-30) + " |\n| B | Basil | " + day(-21) + " |\n\n- [ ] Water on Sunday\n- [ ] Order compost #home\n"},
		{"Reading", "Reading list", "# Reading list\n\n- [ ] *The Pragmatic Programmer*\n- [x] *Designing Data-Intensive Applications*\n- [ ] *A Philosophy of Software Design* #ideas\n"},
		{"Quick Notes", "Ideas", "# Ideas\n\n- A Raycast command that appends to today's note #ideas\n- Weekly review template\n"},
		{"Daily", day(-1), "# " + day(-1) + "\n\n- 9:12 Standup moved to 10\n- 14:30 Called the plumber\n"},
		{"Daily", day(0), "# " + day(0) + "\n\n- 8:05 Coffee, then the server rebuild #homelab\n"},
		{"Templates", "Meeting", "# Meeting, {{date}}\n\n**With:** \n\n## Agenda\n\n- \n\n## Notes\n\n\n## To do\n\n- [ ] \n"},
	}
	for _, n := range notes {
		cat, title, body := n.cat, n.title, n.body
		if _, err := s.createNote(ctx, u.ID, NoteInput{Title: &title, Category: &cat, Content: &body}, "import"); err != nil {
			return err
		}
	}
	fav := true
	if n, err := s.store.FindNoteByTitle(ctx, u.ID, "Server rebuild", "Projects"); err == nil {
		_, _ = s.updateNote(ctx, u.ID, n.ID, NoteInput{Favorite: &fav, Pinned: &fav}, "", "import")
	}
	s.log.Info("demo notes seeded")
	return nil
}
