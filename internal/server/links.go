package server

import (
	"context"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-core/sdk/appkit"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it.

var linkOffers = []core.Offer{
	{Scope: "notes:read", Name: "Read your notes", Description: "List, search and read your notes, tags, tasks and daily notes (what Pocket Day shows in the journal), and the notes about a book, an episode or a trip (shown on its page in that app)."},
	{Scope: "notes:write", Name: "Write in your notes", Description: "Create notes, add lines to them and to today's daily note, tick tasks."},
}

// portable is what Pocket Move may copy out and bring in (docs/move.md in
// pocket-homelab-core), for the person the link acts for.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.zip", File: "notes.zip", Kind: core.KindPocket, Title: "Every note as a Markdown file, with its folders and pictures"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, Form: "file", MaxBytes: importMaxBytes, Title: "Adds the notes from another box; notes already here are left alone"},
		{Path: "/api/v1/import", Kind: core.KindMarkdownZip, Form: "file", MaxBytes: importMaxBytes, Title: "Adds Markdown files as notes, folders kept; notes already here are left alone"},
	},
}

// capabilities are what Pocket Search, Pocket Day and Pocket Hub may ask
// here for a person (docs/capabilities.md in pocket-homelab-core): one value
// for the manifest, the /pocket routes and linkAllows.
var capabilities = core.Capabilities{
	Search:  &core.Searchable{What: "Your notes, by title and text", Kinds: []string{"note"}, Bangs: []string{"notes"}},
	Day:     &core.DayFeed{Label: "Notes", One: "note", Many: "notes", Unit: "count", What: "The notes you wrote or changed that day, and that day's daily note"},
	Widget:  &core.Widget{Title: "Recent notes", What: "The notes you changed last, pinned ones first"},
	Capture: &core.QuickCapture{Kinds: []core.CaptureKind{{Kind: "note", Label: "Note", What: "A new note in the folder your new notes go to", Fields: []string{"title"}}}},
}

// linkUses are the Pocket apps whose data the editor's homelab tools put into
// a note, and Pocket Files, which can hold the notes folder itself.
var linkUses = []core.Use{
	{App: "files", Scopes: []string{"files:read", "files:write"}, Reason: "Keep your notes as Markdown files in Pocket Files"},
	{App: "tasks", Scopes: []string{"tasks:read", "tasks:write"}, Reason: "Turn a line of a note into a Pocket Tasks task"},
	{App: "habits", Scopes: []string{"habits:read", "entries:write"}, Reason: "Days of habits written into a note, and today's habits ticked on the Today page"},
	{App: "money", Scopes: []string{"transactions:read", "transactions:write"}, Reason: "Spending this month in a note, and a line of a note made a transaction"},
	{App: "garage", Scopes: []string{"vehicles:read"}, Reason: "The vehicle log (fill-ups, services) written into a note"},
	{App: "podcasts", Scopes: []string{"history:read"}, Reason: "What you listened to this week written into a note, and episodes linked in a note"},
	{App: "watch", Scopes: []string{"watching:read"}, Reason: "What you watched this week written into a note"},
	{App: "monitoring", Scopes: []string{"monitoring:read"}, Reason: "The server's CPU, memory, disk and busiest processes written into a note"},
	{App: "documents", Scopes: []string{"documents:read"}, Reason: "Find documents and link them in a note"},
	{App: "bookshelf", Scopes: []string{"books:read"}, Reason: "Find your books and link them in a note"},
	{App: "contacts", Scopes: []string{"contacts:read"}, Reason: "Link people in a note (type @ and a name), so the note shows on their page in Pocket Contacts"},
	{App: "draw", Scopes: []string{"drawings:read", "drawings:write"}, Reason: "Show your sketches and diagrams from Pocket Draw inside a note, and start a new one from a note"},
}

// linkRoutes are the requests a linked app may make with its scopes; every
// other route stays closed to it (appkit.Routes, with the Portable and
// Capabilities routes added by linkAllows).
var linkRoutes = appkit.Routes{
	appkit.R("GET", `^/api/v1/auth/me$`, ""),
	appkit.R("GET", `^/api/v1/(notes|search|suggest|resolve|mentions|folders|tags|tasks|daily|calendar|dashboard|templates)$`, "notes:read"),
	appkit.R("GET", `^/api/v1/notes/\d+(/backlinks|/attachment)?$`, "notes:read"),
	appkit.R("POST", `^/api/v1/(notes|capture|daily)$`, "notes:write"),
	appkit.R("POST", `^/api/v1/notes/\d+/(append|tasks|attachments)$`, "notes:write"),
	appkit.R("PUT", `^/api/v1/notes/\d+$`, "notes:write"),
}

// linkAllows reports whether a link token with these scopes may make a
// request: linkRoutes plus the Portable and Capabilities routes.
var linkAllows = linkRoutes.Allow(portable, capabilities)

// connModes is how each person is connected to the services and apps this
// one uses (Core → Links shows "Direct" for typed-in ones); s.modes
// (appkit.ModeReporter) sends them after each registration and when a
// person changes a connection.
func (s *Server) connModes(ctx context.Context) ([]core.ConnMode, error) {
	var modes []core.ConnMode
	for _, uid := range s.store.UserIDs(ctx) {
		email := s.userEmail(ctx, uid)
		if email == "" {
			continue
		}
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil {
			continue
		}
		storageMode := "off"
		if st.StorageKind == "nextcloud" {
			storageMode = "direct"
			if st.StorageViaCore {
				storageMode = "core"
			}
		}
		for _, u := range serviceUses {
			mode := s.integrationMode(ctx, uid, "svc", u.Type)
			if u.Type == "nextcloud" && storageMode != "off" && mode == "off" {
				mode = storageMode
			}
			modes = append(modes, core.ConnMode{User: email, Target: "service:" + u.Type, Mode: mode})
		}
		for _, u := range linkUses {
			var mode string
			if libraryOnlyApps[u.App] {
				mode = "off"
				if st.StorageKind == u.App {
					mode = "core"
				}
			} else {
				mode = s.integrationMode(ctx, uid, "app", u.App)
			}
			modes = append(modes, core.ConnMode{User: email, Target: u.App, Mode: mode})
		}
	}
	return modes, nil
}
