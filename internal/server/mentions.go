package server

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// Notes about things in other apps (docs/notes-from-apps.md): a book, an
// episode, a trip is its page's address, and a note is about it when it links
// there. Another Pocket app asks GET /api/v1/mentions?url=… (Core link scope
// notes:read) to show "Notes about this" on its own page, and sends people to
// /new?url=… to start one.

var noteURLRe = regexp.MustCompile(`https?://[^\s<>()\[\]"'` + "`" + `]+`)

// itemKey is an address as compared here: host in lower case, then path,
// then query; no scheme, fragment or trailing slash ("" if it is no web address).
func itemKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	k := strings.ToLower(u.Host) + strings.TrimRight(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		k += "?" + u.RawQuery
	}
	return k
}

// linksTo reports whether text links to the item: the same address, or one
// below it or with a query of its own (an episode at a time, ?t=95).
func linksTo(text string, keys []string) bool {
	for _, m := range noteURLRe.FindAllString(text, -1) {
		k := itemKey(strings.TrimRight(m, ".,;:!?"))
		if k == "" {
			continue
		}
		for _, want := range keys {
			if k == want || strings.HasPrefix(k, want+"?") || strings.HasPrefix(k, want+"/") {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleMentions(w http.ResponseWriter, r *http.Request) {
	var keys, needles []string
	for _, raw := range r.URL.Query()["url"] {
		k := itemKey(raw)
		if k == "" {
			continue
		}
		keys = append(keys, k)
		// The text is searched for host and path; the exact match is made above.
		needles = append(needles, strings.SplitN(k, "?", 2)[0])
	}
	if len(keys) == 0 || len(keys) > 20 {
		writeErr(w, http.StatusBadRequest, "give one to 20 web addresses as url=")
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v < limit {
		limit = v
	}
	cands, err := s.store.NotesContaining(r.Context(), me(r), needles, 500)
	if err != nil {
		s.fail(w, err, "mentions")
		return
	}
	out := []store.Note{}
	for _, n := range cands {
		if len(out) == limit {
			break
		}
		if linksTo(n.Content, keys) {
			n.Content = ""
			out = append(out, n)
		}
	}
	writeJSON(w, http.StatusOK, out)
}
