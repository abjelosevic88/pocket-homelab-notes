package server

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Links to what lives elsewhere (Nextcloud files, Pocket Documents, books in
// Pocket Bookshelf, podcast episodes, Forgejo issues, Jellyfin titles, Immich
// photos) and web bookmarks. The note gets a
// link, not a copy.

// webLinkMarkdown is a Markdown link to an absolute address: brackets and line
// breaks out of the text, spaces and parentheses escaped in the address.
func webLinkMarkdown(title, target string) string {
	title = strings.NewReplacer("[", "", "]", "").Replace(title)
	title = strings.Join(strings.Fields(title), " ")
	target = strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(target)
	return "[" + title + "](" + target + ")"
}

type linkItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	URL      string `json:"url"`
	Icon     string `json:"icon"`
	Markdown string `json:"markdown"`
}

type linkSection struct {
	Source string     `json:"source"`
	Title  string     `json:"title"`
	Items  []linkItem `json:"items"`
}

// linkSearcher finds things in one service; note is a footer to show, if any.
type linkSearcher func(s *Server, ctx context.Context, c *svcCred, q string) (items []linkItem, note string, err error)

// linkSources are searched in this order and shown in it. Kind is "svc" for a
// service (a Core service connection or a typed-in key) or "app" for a Pocket
// app (a Core link token with the scopes in linkUses, or a typed-in token).
var linkSources = []struct {
	Kind, Source, Title string
	Search              linkSearcher
}{
	{"svc", "nextcloud", "Nextcloud", linkNextcloud},
	{"app", "documents", "Pocket Documents", linkDocuments},
	{"app", "bookshelf", "Pocket Bookshelf", linkBooks},
	{"app", "podcasts", "Pocket Podcasts", linkEpisodes},
	{"svc", "forgejo", "Forgejo", linkForgejo},
	{"svc", "jellyfin", "Jellyfin", linkJellyfin},
	{"svc", "immich", "Immich", linkImmich},
}

// linkCred is the person's way into one link source.
func (s *Server) linkCred(ctx context.Context, uid int64, kind, name string) (*svcCred, error) {
	if kind == "app" {
		return s.appCred(ctx, uid, name, useScopes(name)...)
	}
	return s.service(ctx, uid, name)
}

const linkPerSource = 6

// handleToolLink searches every connected service at once. One that is not
// connected is left out; one that fails says so in "errors".
func (s *Server) handleToolLink(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	type result struct {
		items []linkItem
		note  string
		err   error
	}
	results := make([]result, len(linkSources))
	if utf8.RuneCountInString(q) >= 2 {
		var wg sync.WaitGroup
		for i, src := range linkSources {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				c, err := s.linkCred(cctx, uid, src.Kind, src.Source)
				if err == nil {
					results[i].items, results[i].note, err = src.Search(s, cctx, c, q)
				}
				results[i].err = err
			}()
		}
		wg.Wait()
	}
	sections := []linkSection{}
	errs := map[string]string{}
	var notes []string
	for i, src := range linkSources {
		res := results[i]
		if res.err != nil {
			var nc errNotConnected
			switch {
			case errors.As(res.err, &nc):
			case errors.Is(res.err, context.DeadlineExceeded):
				errs[src.Source] = serviceNames[src.Source] + " took too long to answer"
			default:
				errs[src.Source] = res.err.Error()
			}
			continue
		}
		if res.note != "" {
			notes = append(notes, res.note)
		}
		if len(res.items) > 0 {
			sections = append(sections, linkSection{Source: src.Source, Title: src.Title, Items: res.items})
		}
	}
	out := map[string]any{"sections": sections, "errors": errs}
	if len(notes) > 0 {
		out["note"] = strings.Join(notes, ". ")
	}
	writeJSON(w, http.StatusOK, out)
}

func newLinkItem(id, title, detail, target, icon string) linkItem {
	return linkItem{ID: id, Title: title, Detail: detail, URL: target, Icon: icon, Markdown: webLinkMarkdown(title, target)}
}

// shortDay is a date as "1 Oct 2026" from an ISO date or time.
func shortDay(v string) string {
	if len(v) >= 10 {
		if t, err := time.Parse("2006-01-02", v[:10]); err == nil {
			return t.Format("2 Jan 2006")
		}
	}
	return ""
}

// linkNextcloud: the full-text index first (names, document text, OCR);
// files by name when the index does not answer.
func linkNextcloud(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	fctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	files, err := s.cloudFullText(fctx, c, q, linkPerSource)
	cancel()
	note := ""
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		if files, err = s.cloudNameSearch(ctx, c, q, linkPerSource); err != nil {
			return nil, "", err
		}
		note = "Nextcloud's full-text index did not answer, so files are searched by name only"
	}
	items := []linkItem{}
	for _, f := range files {
		icon := "file"
		if f.Dir {
			icon = "folder"
		}
		parent := strings.TrimSuffix(f.Path, "/"+f.Name)
		if parent == "" {
			parent = "/"
		}
		items = append(items, newLinkItem("file-"+firstNonEmpty(f.FileID, f.Path), f.Name, parent, cloudFileLink(c, f), icon))
	}
	return items, note, nil
}

// linkDocuments: Pocket Documents' search (full text and OCR, field syntax
// like from:ikea or created:2024, Cyrillic and Latin alike). The detail line
// is the date, correspondent and type, then the matching words.
func linkDocuments(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	var res struct {
		Results []struct {
			ID            int64  `json:"id"`
			Title         string `json:"title"`
			Created       string `json:"created"`
			Correspondent string `json:"correspondent_name"`
			DocumentType  string `json:"document_type_name"`
			Snippet       string `json:"snippet"`
		} `json:"results"`
	}
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(linkPerSource)}, "no_facets": {"1"}}
	if err := s.callJSON(ctx, "documents", http.MethodGet, c.API("/api/v1/documents?"+v.Encode()), map[string]string{"Authorization": "Bearer " + c.Key}, nil, &res); err != nil {
		return nil, "", err
	}
	items := []linkItem{}
	for _, d := range res.Results {
		if len(items) == linkPerSource {
			break
		}
		id := strconv.FormatInt(d.ID, 10)
		var detail []string
		for _, part := range []string{shortDay(d.Created), d.Correspondent, d.DocumentType} {
			if part = strings.TrimSpace(part); part != "" {
				detail = append(detail, part)
			}
		}
		if snip := plainSnippet(d.Snippet, 120); snip != "" {
			detail = append(detail, snip)
		}
		items = append(items, newLinkItem("doc-"+id, firstNonEmpty(strings.TrimSpace(d.Title), "Document "+id), strings.Join(detail, " · "), c.Link("/documents/"+id), "document"))
	}
	return items, "", nil
}

// plainSnippet is a search snippet without its [[[hit]]] marks, on one line,
// cut to limit characters.
func plainSnippet(v string, limit int) string {
	v = strings.NewReplacer("[[[", "", "]]]", "").Replace(v)
	v = strings.Join(strings.Fields(v), " ")
	if utf8.RuneCountInString(v) > limit {
		r := []rune(v)
		v = strings.TrimSpace(string(r[:limit])) + "…"
	}
	return v
}

type forgejoIssue struct {
	ID          int64  `json:"id"`
	Number      int64  `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	PullRequest *struct {
		Merged bool `json:"merged"`
	} `json:"pull_request"`
	Repository *struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// linkForgejo: issues and pull requests across every repository the key sees.
func linkForgejo(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	hdr := map[string]string{"Authorization": "token " + c.Key}
	var found [2][]forgejoIssue
	var errs [2]error
	var wg sync.WaitGroup
	for i, typ := range []string{"issues", "pulls"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := url.Values{"type": {typ}, "state": {"all"}, "sort": {"recentupdate"}, "limit": {"4"}, "q": {q}}
			errs[i] = s.callJSON(ctx, "forgejo", http.MethodGet, c.API("/api/v1/repos/issues/search?"+v.Encode()), hdr, nil, &found[i])
		}()
	}
	wg.Wait()
	if errs[0] != nil && errs[1] != nil {
		return nil, "", errs[0]
	}
	items := []linkItem{}
	for _, list := range found {
		for _, is := range list {
			if is.HTMLURL == "" {
				continue
			}
			icon, kind := "issue", "issue"
			if is.PullRequest != nil {
				icon, kind = "pull", "pull"
			}
			var detail []string
			if is.Repository != nil && is.Repository.FullName != "" {
				detail = append(detail, is.Repository.FullName)
			}
			detail = append(detail, "#"+strconv.FormatInt(is.Number, 10))
			if is.State != "" {
				detail = append(detail, is.State)
			}
			items = append(items, newLinkItem(kind+"-"+strconv.FormatInt(is.ID, 10), is.Title, strings.Join(detail, " · "), is.HTMLURL, icon))
		}
	}
	return items, "", nil
}

// jellyfinUser is the account whose library is searched: the one set for
// this person, or else the first administrator (as the app does).
func (s *Server) jellyfinUser(ctx context.Context, c *svcCred) (string, error) {
	if id := c.Field("USER_ID"); id != "" {
		return id, nil
	}
	var users []struct {
		ID     string `json:"Id"`
		Policy *struct {
			IsAdministrator bool `json:"IsAdministrator"`
		} `json:"Policy"`
	}
	if err := s.callJSON(ctx, "jellyfin", http.MethodGet, c.API("/Users"), map[string]string{"X-Emby-Token": c.Key}, nil, &users); err != nil {
		return "", err
	}
	for _, u := range users {
		if u.Policy != nil && u.Policy.IsAdministrator {
			return u.ID, nil
		}
	}
	if len(users) > 0 {
		return users[0].ID, nil
	}
	return "", errors.New("Jellyfin has no users")
}

// linkJellyfin: films, series and episodes across every library.
func linkJellyfin(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	hdr := map[string]string{"X-Emby-Token": c.Key}
	var serverID string
	done := make(chan struct{})
	go func() {
		defer close(done)
		var info struct {
			ID string `json:"Id"`
		}
		if s.callJSON(ctx, "jellyfin", http.MethodGet, c.API("/System/Info/Public"), hdr, nil, &info) == nil {
			serverID = info.ID
		}
	}()
	uid, err := s.jellyfinUser(ctx, c)
	if err != nil {
		<-done
		return nil, "", err
	}
	var page struct {
		Items []struct {
			ID             string `json:"Id"`
			Name           string `json:"Name"`
			Type           string `json:"Type"`
			SeriesName     string `json:"SeriesName"`
			ProductionYear int    `json:"ProductionYear"`
		} `json:"Items"`
	}
	v := url.Values{"searchTerm": {q}, "includeItemTypes": {"Movie,Series,Episode"}, "recursive": {"true"}, "limit": {strconv.Itoa(linkPerSource)}, "fields": {"ProductionYear"}}
	err = s.callJSON(ctx, "jellyfin", http.MethodGet, c.API("/Users/"+url.PathEscape(uid)+"/Items?"+v.Encode()), hdr, nil, &page)
	<-done
	if err != nil {
		return nil, "", err
	}
	items := []linkItem{}
	for _, it := range page.Items {
		var detail []string
		if it.SeriesName != "" {
			detail = append(detail, it.SeriesName)
		}
		if it.ProductionYear > 0 {
			detail = append(detail, strconv.Itoa(it.ProductionYear))
		}
		if it.Type != "" {
			detail = append(detail, it.Type)
		}
		icon := "movie"
		switch it.Type {
		case "Series":
			icon = "series"
		case "Episode":
			icon = "episode"
		}
		target := "/web/#/details?id=" + url.QueryEscape(it.ID)
		if serverID != "" {
			target += "&serverId=" + url.QueryEscape(serverID)
		}
		items = append(items, newLinkItem("jf-"+it.ID, it.Name, strings.Join(detail, " · "), c.Link(target), icon))
	}
	return items, "", nil
}

// linkImmich: photos by what is in them (smart search).
func linkImmich(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	var res immichSearch
	if err := s.callJSON(ctx, "immich", http.MethodPost, c.API("/api/search/smart"), immichHeaders(c), map[string]any{"query": q, "size": linkPerSource, "page": 1}, &res); err != nil {
		return nil, "", err
	}
	items := []linkItem{}
	for _, a := range res.Assets.Items {
		if len(items) == linkPerSource {
			break
		}
		items = append(items, newLinkItem("photo-"+a.ID, firstNonEmpty(a.OriginalFileName, "Photo"), a.day(), c.Link("/photos/"+url.PathEscape(a.ID)), "photo"))
	}
	return items, "", nil
}

// ---- bookmarks ----

// webAddress is an address as typed, with "https://" added when the scheme
// was left off. "mailto:…" or "ftp://…" is not a web page and answers false.
func webAddress(raw string) (*url.URL, bool) {
	text := strings.TrimSpace(raw)
	if text == "" || strings.ContainsFunc(text, unicode.IsSpace) {
		return nil, false
	}
	if i := strings.Index(text, ":"); i > 1 && !strings.Contains(text, "://") {
		letters := true
		for _, r := range text[:i] {
			if !unicode.IsLetter(r) {
				letters = false
				break
			}
		}
		if letters {
			return nil, false
		}
	}
	if !strings.Contains(text, "://") {
		text = "https://" + text
	}
	u, err := url.Parse(text)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, false
	}
	host := u.Hostname()
	if host == "" || (!strings.Contains(host, ".") && !strings.Contains(host, ":") && host != "localhost") {
		return nil, false
	}
	return u, true
}

var (
	metaTagRe  = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	attrRe     = regexp.MustCompile(`(?is)([a-z_:.-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	titleTagRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// pageTitle is what a page calls itself: its og:title, else its <title>.
func pageTitle(page string) string {
	for _, tag := range metaTagRe.FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = m[2] + m[3] + m[4]
		}
		if strings.EqualFold(attrs["property"], "og:title") || strings.EqualFold(attrs["name"], "og:title") {
			if t := cleanTitle(attrs["content"]); t != "" {
				return t
			}
		}
	}
	if m := titleTagRe.FindStringSubmatch(page); m != nil {
		return cleanTitle(m[1])
	}
	return ""
}

// cleanTitle decodes entities and folds whitespace.
func cleanTitle(v string) string {
	return strings.Join(strings.Fields(html.UnescapeString(v)), " ")
}

// hostTitle is the host without "www.", when the page says nothing.
func hostTitle(u *url.URL) string {
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// fetchPageTitle reads at most 1 MB of a web page for its title.
func (s *Server) fetchPageTitle(ctx context.Context, u *url.URL) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := &http.Client{Transport: s.http.Transport, Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirect to a non-web address")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PocketNotes/"+Version+"; +https://github.com/abjelosevic88/pocket-homelab-notes)")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil && len(data) == 0 {
		return "", err
	}
	return pageTitle(strings.ToValidUTF8(string(data), "")), nil
}

// handleToolBookmark turns an address into a link with the page's title.
func (s *Server) handleToolBookmark(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, ok := webAddress(in.URL)
	if !ok {
		writeErr(w, http.StatusBadRequest, "That is not a web address.")
		return
	}
	title, err := s.fetchPageTitle(r.Context(), u)
	if err != nil || title == "" {
		title = hostTitle(u)
	}
	target := u.String()
	writeJSON(w, http.StatusOK, map[string]string{"title": title, "url": target, "markdown": webLinkMarkdown(title, target)})
}
