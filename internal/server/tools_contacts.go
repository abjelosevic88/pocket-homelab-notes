package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

// People from Pocket Contacts (Core link scope contacts:read): "Link to
// something" finds them, and typing @ and a name in a note offers them. Either
// way the note gets a link to the person's page in Contacts,
// "[Marko Petrović](https://contacts…/contacts/12)", which is what that page's
// Notes box looks for (GET /api/v1/mentions?url=…).

// contactSummary is a row of Pocket Contacts' search (docs/api.md there).
type contactSummary struct {
	ID       int64    `json:"id"`
	FN       string   `json:"fn"`
	Org      string   `json:"org"`
	Title    string   `json:"title"`
	Emails   []string `json:"emails"`
	Phones   []string `json:"phones"`
	PhotoURL string   `json:"photo_url"`
	Initials string   `json:"initials"`
	Color    string   `json:"color"`
}

// personItem is a person as the editor's @ list and the link picker show one.
type personItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Detail   string `json:"detail"`
	Initials string `json:"initials"`
	Color    string `json:"color"`
	Photo    string `json:"photo"` // this server's copy of the picture, "" when none
	URL      string `json:"url"`   // the person's page in Contacts
	Markdown string `json:"markdown"`
}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

// searchContacts asks Contacts for the person's best matches.
func (s *Server) searchContacts(ctx context.Context, c *svcCred, q string, limit int) ([]contactSummary, error) {
	var res struct {
		Contacts []contactSummary `json:"contacts"`
	}
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}
	if err := s.callJSON(ctx, "contacts", http.MethodGet, c.API("/api/v1/search?"+v.Encode()), map[string]string{"Authorization": "Bearer " + c.Key}, nil, &res); err != nil {
		return nil, err
	}
	if len(res.Contacts) > limit {
		res.Contacts = res.Contacts[:limit]
	}
	return res.Contacts, nil
}

// person is a contact with its link: the name, then the organisation and
// the first address to reach them by.
func person(c *svcCred, sm contactSummary) personItem {
	id := strconv.FormatInt(sm.ID, 10)
	name := firstNonEmpty(strings.Join(strings.Fields(sm.FN), " "), strings.TrimSpace(sm.Org), "Contact "+id)
	var detail []string
	if org := strings.TrimSpace(sm.Org); org != "" && org != name {
		detail = append(detail, org)
	}
	if len(sm.Emails) > 0 && strings.TrimSpace(sm.Emails[0]) != "" {
		detail = append(detail, strings.TrimSpace(sm.Emails[0]))
	} else if len(sm.Phones) > 0 && strings.TrimSpace(sm.Phones[0]) != "" {
		detail = append(detail, strings.TrimSpace(sm.Phones[0]))
	}
	initials := strings.TrimSpace(sm.Initials)
	if initials == "" {
		n := 0
		for _, w := range strings.Fields(name) {
			if i := strings.IndexFunc(w, unicode.IsLetter); i >= 0 && n < 2 {
				r, _ := utf8.DecodeRuneInString(w[i:])
				initials += string(unicode.ToUpper(r))
				n++
			}
		}
	}
	color := sm.Color
	if !hexColorRe.MatchString(color) {
		color = "#64748b"
	}
	photo := ""
	if sm.PhotoURL != "" {
		photo = "/api/v1/tools/contacts/" + id + "/photo?s=64"
		if u, err := url.Parse(sm.PhotoURL); err == nil && u.Query().Get("v") != "" {
			photo += "&v=" + url.QueryEscape(u.Query().Get("v"))
		}
	}
	target := c.Link("/contacts/" + id)
	return personItem{ID: id, Name: name, Detail: strings.Join(detail, " · "), Initials: initials, Color: color, Photo: photo, URL: target, Markdown: webLinkMarkdown(name, target)}
}

// linkContacts is Pocket Contacts in "Link to something".
func linkContacts(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	found, err := s.searchContacts(ctx, c, q, linkPerSource)
	if err != nil {
		return nil, "", err
	}
	items := []linkItem{}
	for _, sm := range found {
		p := person(c, sm)
		items = append(items, linkItem{ID: "contact-" + p.ID, Title: p.Name, Detail: p.Detail, URL: p.URL, Icon: "person", Markdown: p.Markdown,
			Photo: p.Photo, Initials: p.Initials, Color: p.Color})
	}
	return items, "", nil
}

// handleToolContacts is the editor's @ list: the person's contacts matching
// what was typed after the @. Contacts switched off or not linked is no
// error here, only "enabled": false and nobody, so plain @ text goes on.
func (s *Server) handleToolContacts(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 8
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, 20)
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	c, err := s.appCred(cctx, uid, "contacts", useScopes("contacts")...)
	if err != nil {
		var nc errNotConnected
		if errors.As(err, &nc) {
			writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "contacts": []personItem{}, "message": err.Error()})
			return
		}
		toolErr(w, err)
		return
	}
	out := []personItem{}
	if q != "" {
		found, err := s.searchContacts(cctx, c, q, limit)
		if err != nil {
			toolErr(w, err)
			return
		}
		for _, sm := range found {
			out = append(out, person(c, sm))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "contacts": out})
}

// handleToolContactPhoto passes a contact's picture on, so the token stays here.
func (s *Server) handleToolContactPhoto(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	id := chi.URLParam(r, "id")
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid contact id")
		return
	}
	size := 64
	if v, err := strconv.Atoi(r.URL.Query().Get("s")); err == nil {
		size = min(max(v, 32), 512)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := s.appCred(cctx, uid, "contacts", useScopes("contacts")...)
	if err != nil {
		toolErr(w, err)
		return
	}
	data, hdr, err := s.fetchRaw(cctx, "contacts", http.MethodGet, c.API("/api/v1/contacts/"+id+"/photo?s="+strconv.Itoa(size)), map[string]string{"Authorization": "Bearer " + c.Key}, "", 10<<20)
	if err != nil {
		if remoteStatus(err) == http.StatusNotFound {
			writeErr(w, http.StatusNotFound, "no picture")
			return
		}
		toolErr(w, err)
		return
	}
	ctype := hdr.Get("Content-Type")
	if !strings.HasPrefix(ctype, "image/") || strings.Contains(ctype, "svg") {
		ctype = "image/jpeg"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
