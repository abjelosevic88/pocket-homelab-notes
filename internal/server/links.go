package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it.

var linkOffers = []core.Offer{
	{Scope: "notes:read", Name: "Read your notes", Description: "List, search and read your notes, tags, tasks and daily notes (what Pocket Day shows in the journal)."},
	{Scope: "notes:write", Name: "Write in your notes", Description: "Create notes, add lines to them and to today's daily note, tick tasks."},
}

// linkUses are the Pocket apps whose data the editor's homelab tools put into a note.
var linkUses = []core.Use{
	{App: "habits", Scopes: []string{"habits:read"}, Reason: "Days of habits: the habits done and missed, written into a note"},
	{App: "money", Scopes: []string{"transactions:read", "transactions:write"}, Reason: "Spending this month in a note, and a line of a note made a transaction"},
	{App: "garage", Scopes: []string{"vehicles:read"}, Reason: "The vehicle log (fill-ups, services) written into a note"},
	{App: "podcasts", Scopes: []string{"history:read"}, Reason: "What you listened to this week, written into a note"},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(notes|search|suggest|resolve|folders|tags|tasks|daily|calendar|dashboard|templates)$`), "notes:read"},
	{"GET", regexp.MustCompile(`^/api/v1/notes/\d+(/backlinks|/attachment)?$`), "notes:read"},
	{"POST", regexp.MustCompile(`^/api/v1/(notes|capture|daily)$`), "notes:write"},
	{"POST", regexp.MustCompile(`^/api/v1/notes/\d+/(append|tasks|attachments)$`), "notes:write"},
	{"PUT", regexp.MustCompile(`^/api/v1/notes/\d+$`), "notes:write"},
}

var errLinkScope = errors.New("outside the link's scope")

func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token and maps it to the person it acts for.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" {
		return nil, http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, errLinkScope
	}
	return s.firstOrSystemUser(ctx, strings.ToLower(in.User.Email), in.User.Name)
}

// reportModes tells Core how each person is connected to the services and
// apps this one uses (Core → Links shows "Direct" for typed-in ones).
func (s *Server) reportModes(ctx context.Context) {
	if !s.core.Configured() {
		return
	}
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
			modes = append(modes, core.ConnMode{User: email, Target: u.App, Mode: s.integrationMode(ctx, uid, "app", u.App)})
		}
	}
	if err := s.core.ReportModes(ctx, modes); err != nil {
		s.log.Debug("pocket core: report modes", "err", err)
	}
}
