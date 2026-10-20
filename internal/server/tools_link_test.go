package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// connectDirect points a person's integration at a fake, as Settings does.
func connectDirect(h *harness, uid int64, typ, base, key, user string) {
	h.t.Helper()
	ctx := context.Background()
	for k, v := range map[string]string{"mode": "direct", "url": base, "key": key, "user": user} {
		if err := h.st.SetUserSetting(ctx, uid, "svc."+typ+"."+k, v); err != nil {
			h.t.Fatal(err)
		}
	}
}

type linkAnswer struct {
	Sections []struct {
		Source string     `json:"source"`
		Title  string     `json:"title"`
		Items  []linkItem `json:"items"`
	} `json:"sections"`
	Errors map[string]string `json:"errors"`
	Note   string            `json:"note"`
}

func (a linkAnswer) section(src string) []linkItem {
	for _, s := range a.Sections {
		if s.Source == src {
			return s.Items
		}
	}
	return nil
}

// fakeNextcloud answers WebDAV (PROPFIND, SEARCH, GET) for user alice and,
// when fullText is set, the Full Text Search app.
func fakeNextcloud(t *testing.T, fullText bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "alice" || p != "app-pass" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/index.php/apps/fulltextsearch/v1/remote":
			if !fullText {
				w.WriteHeader(404)
				_, _ = io.WriteString(w, "<html>not found</html>")
				return
			}
			var req map[string]any
			_ = json.Unmarshal([]byte(r.URL.Query().Get("request")), &req)
			if req["search"] != "invoice" {
				t.Errorf("fts request %v", req)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":1,"result":[{"provider":{"id":"files","name":"Files"},"documents":[{"id":"812","providerId":"files","title":"Invoice March.pdf","info":{"path":"/Documents/Invoice March.pdf","file":"Invoice March.pdf","type":"file","size":1234,"mime":"application/pdf","mtime":1760000000},"excerpts":[{"source":"content","excerpt":"<em>invoice</em> total"}]}],"meta":{"timedOut":false,"total":1,"maxScore":1}}]}`)
		case r.Method == "SEARCH" && r.URL.Path == "/remote.php/dav/":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "%invoice%") || !strings.Contains(string(body), "/files/alice/") {
				t.Errorf("dav search body %s", body)
			}
			w.WriteHeader(207)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:response><d:href>/remote.php/dav/files/alice/Old/invoice%202019.pdf</d:href><d:propstat><d:prop><d:displayname>invoice 2019.pdf</d:displayname><d:getcontentlength>99</d:getcontentlength><d:getcontenttype>application/pdf</d:getcontenttype><d:resourcetype/><oc:fileid>77</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
		case r.Method == "PROPFIND" && r.URL.Path == "/remote.php/dav/files/alice/Documents":
			if r.Header.Get("Depth") != "1" {
				t.Errorf("depth %q", r.Header.Get("Depth"))
			}
			w.WriteHeader(207)
			_, _ = io.WriteString(w, `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:s="http://sabredav.org/ns" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.org/ns">
 <d:response><d:href>/remote.php/dav/files/alice/Documents/</d:href><d:propstat><d:prop><d:getlastmodified>Mon, 12 Oct 2026 10:00:00 GMT</d:getlastmodified><d:resourcetype><d:collection/></d:resourcetype><oc:fileid>10</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/files/alice/Documents/z%20report.pdf</d:href><d:propstat><d:prop><d:getlastmodified>Tue, 13 Oct 2026 09:30:00 GMT</d:getlastmodified><d:getcontentlength>123</d:getcontentlength><d:getcontenttype>application/pdf</d:getcontenttype><d:resourcetype/><oc:fileid>456</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat><d:propstat><d:prop><nc:missing/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/files/alice/Documents/Archive/</d:href><d:propstat><d:prop><d:getlastmodified>Tue, 13 Oct 2026 09:30:00 GMT</d:getlastmodified><d:resourcetype><d:collection/></d:resourcetype><oc:fileid>11</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/files/alice/Documents/a.png</d:href><d:propstat><d:prop><d:getcontentlength>4</d:getcontentlength><d:getcontenttype>image/png</d:getcontenttype><d:resourcetype/><oc:fileid>457</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
 <d:response><d:href>/remote.php/dav/files/alice/Documents/.hidden</d:href><d:propstat><d:prop><d:resourcetype/><oc:fileid>458</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`)
		case r.Method == "PROPFIND" && r.URL.Path == "/remote.php/dav/files/alice/Documents/z report.pdf":
			if r.Header.Get("Depth") != "0" {
				t.Errorf("depth %q", r.Header.Get("Depth"))
			}
			w.WriteHeader(207)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:response><d:href>/remote.php/dav/files/alice/Documents/z%20report.pdf</d:href><d:propstat><d:prop><d:getcontentlength>8</d:getcontentlength><d:getcontenttype>application/pdf</d:getcontenttype><d:resourcetype/><oc:fileid>456</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
		case r.Method == "GET" && r.URL.Path == "/remote.php/dav/files/alice/Documents/z report.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = io.WriteString(w, "%PDF-1.7")
		case r.Method == "PROPFIND":
			w.WriteHeader(404)
		default:
			t.Errorf("nextcloud: unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
}

func TestLinkSearchMergesAndReportsFailures(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")

	nc := fakeNextcloud(t, true)
	defer nc.Close()
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "database is locked")
	}))
	defer documents.Close()
	forgejo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token fj-key" || r.URL.Path != "/api/v1/repos/issues/search" || r.URL.Query().Get("q") != "invoice" {
			t.Errorf("forgejo: %s %v", r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("type") == "pulls" {
			_, _ = io.WriteString(w, `[{"id":9,"number":3,"title":"Fix (invoice) totals","state":"closed","html_url":"https://git.example.org/me/books/pulls/3","pull_request":{"merged":true},"repository":{"id":1,"name":"books","owner":"me","full_name":"me/books"}}]`)
			return
		}
		_, _ = io.WriteString(w, `[{"id":8,"number":2,"title":"Invoice [draft] layout","state":"open","html_url":"https://git.example.org/me/books/issues/2","pull_request":null,"repository":{"id":1,"name":"books","owner":"me","full_name":"me/books"}}]`)
	}))
	defer forgejo.Close()
	jellyfin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/System/Info/Public":
			_, _ = io.WriteString(w, `{"LocalAddress":"http://x","ServerName":"box","Version":"10.10.7","ProductName":"Jellyfin Server","Id":"srv123","StartupWizardCompleted":true}`)
		case "/Users":
			if r.Header.Get("X-Emby-Token") != "jf-key" {
				w.WriteHeader(401)
				return
			}
			_, _ = io.WriteString(w, `[{"Name":"kid","Id":"u-kid","Policy":{"IsAdministrator":false}},{"Name":"admin","Id":"u-admin","Policy":{"IsAdministrator":true}}]`)
		case "/Users/u-admin/Items":
			if r.URL.Query().Get("searchTerm") != "invoice" || r.URL.Query().Get("recursive") != "true" {
				t.Errorf("jellyfin query %v", r.URL.Query())
			}
			_, _ = io.WriteString(w, `{"Items":[{"Name":"The Invoice","Id":"abc","Type":"Movie","ProductionYear":2021},{"Name":"Pilot","Id":"def","Type":"Episode","SeriesName":"Invoices"}],"TotalRecordCount":2,"StartIndex":0}`)
		default:
			t.Errorf("jellyfin: unexpected %s", r.URL)
		}
	}))
	defer jellyfin.Close()

	connectDirect(h, alice.ID, "nextcloud", nc.URL, "app-pass", "alice")
	connectApp(t, h, alice.ID, "documents", documents.URL, "pd_alice")
	connectDirect(h, alice.ID, "forgejo", forgejo.URL, "fj-key", "")
	connectDirect(h, alice.ID, "jellyfin", jellyfin.URL, "jf-key", "")
	// Immich is not connected: it is left out, without an error.
	_ = h.st.SetUserSetting(context.Background(), alice.ID, "svc.nextcloud.public_url", "https://cloud.example.org")

	var a linkAnswer
	if c := h.do(tok, "GET", "/api/v1/tools/link?q=invoice", nil, &a); c != 200 {
		t.Fatalf("link: %d", c)
	}
	var order []string
	for _, s := range a.Sections {
		order = append(order, s.Source)
	}
	if strings.Join(order, ",") != "nextcloud,forgejo,jellyfin" {
		t.Fatalf("sections %v", order)
	}
	if e := a.Errors["documents"]; !strings.Contains(e, "Pocket Documents") || !strings.Contains(e, "database is locked") || strings.Contains(e, "pd_alice") {
		t.Errorf("documents error %q", e)
	}
	if _, ok := a.Errors["immich"]; ok || len(a.Errors) != 1 {
		t.Errorf("errors %v", a.Errors)
	}
	if a.Note != "" {
		t.Errorf("note %q with a working index", a.Note)
	}
	f := a.section("nextcloud")[0]
	if f.URL != "https://cloud.example.org/index.php/f/812" || f.Icon != "file" || f.Detail != "/Documents" || f.Markdown != "[Invoice March.pdf](https://cloud.example.org/index.php/f/812)" {
		t.Errorf("file %+v", f)
	}
	fj := a.section("forgejo")
	if len(fj) != 2 || fj[0].Icon != "issue" || fj[1].Icon != "pull" {
		t.Fatalf("forgejo %+v", fj)
	}
	if fj[0].Markdown != "[Invoice draft layout](https://git.example.org/me/books/issues/2)" || fj[0].Detail != "me/books · #2 · open" {
		t.Errorf("issue %+v", fj[0])
	}
	jf := a.section("jellyfin")
	if len(jf) != 2 || jf[0].URL != jellyfin.URL+"/web/#/details?id=abc&serverId=srv123" || jf[0].Icon != "movie" || jf[1].Icon != "episode" || jf[1].Detail != "Invoices · Episode" {
		t.Errorf("jellyfin %+v", jf)
	}

	// Short queries search nothing.
	var empty linkAnswer
	h.do(tok, "GET", "/api/v1/tools/link?q=i", nil, &empty)
	if len(empty.Sections) != 0 || len(empty.Errors) != 0 {
		t.Errorf("short query %+v", empty)
	}
}

func TestLinkSearchFallsBackToNames(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	nc := fakeNextcloud(t, false)
	defer nc.Close()
	documents := fakeDocuments(t)
	defer documents.Close()
	immich := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search/smart" || r.Header.Get("x-api-key") != "im-key" {
			t.Errorf("immich %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"albums":{"total":0,"count":0,"items":[],"facets":[]},"assets":{"total":1,"count":1,"items":[{"id":"0b1c2d3e-aaaa-bbbb-cccc-111122223333","type":"IMAGE","originalFileName":"IMG_1.jpg","fileCreatedAt":"2026-10-01T08:00:00.000Z","localDateTime":"2026-10-01T10:00:00.000Z"}],"facets":[],"nextPage":null}}`)
	}))
	defer immich.Close()
	connectDirect(h, alice.ID, "nextcloud", nc.URL, "app-pass", "alice")
	connectApp(t, h, alice.ID, "documents", documents.URL, "pd_alice")
	connectDirect(h, alice.ID, "immich", immich.URL, "im-key", "")

	var a linkAnswer
	if c := h.do(tok, "GET", "/api/v1/tools/link?q=invoice", nil, &a); c != 200 {
		t.Fatalf("link: %d", c)
	}
	if len(a.Errors) != 0 {
		t.Fatalf("errors %v", a.Errors)
	}
	if !strings.Contains(a.Note, "by name only") {
		t.Errorf("note %q", a.Note)
	}
	f := a.section("nextcloud")
	if len(f) != 1 || f[0].Title != "invoice 2019.pdf" || f[0].URL != nc.URL+"/index.php/f/77" || f[0].Detail != "/Old" {
		t.Errorf("by name %+v", f)
	}
	d := a.section("documents")
	if len(d) != 1 || d[0].URL != documents.URL+"/documents/5" || d[0].Title != "Invoice ACME" || d[0].Icon != "document" ||
		d[0].Detail != "1 Oct 2026 · ACME d.o.o. · Invoice · …the invoice total is 24.50…" || d[0].Markdown != "[Invoice ACME]("+documents.URL+"/documents/5)" {
		t.Errorf("documents %+v", d)
	}
	p := a.section("immich")
	if len(p) != 1 || p[0].URL != immich.URL+"/photos/0b1c2d3e-aaaa-bbbb-cccc-111122223333" || p[0].Icon != "photo" {
		t.Errorf("immich %+v", p)
	}

	// Nothing connected: no sections, no errors.
	_, bTok := h.user("bob@example.com")
	var b linkAnswer
	h.do(bTok, "GET", "/api/v1/tools/link?q=invoice", nil, &b)
	if len(b.Sections) != 0 || len(b.Errors) != 0 {
		t.Errorf("bob %+v", b)
	}
}

// fakeDocuments answers Pocket Documents' search for two people, each with
// their own pd_ token and their own documents.
func fakeDocuments(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/api/v1/documents" || q.Get("q") != "invoice" || q.Get("limit") != "6" || q.Get("no_facets") != "1" {
			t.Errorf("documents: unexpected %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer pd_alice":
			_, _ = io.WriteString(w, `{"count":1,"offset":0,"limit":6,"results":[{"id":5,"title":"Invoice ACME","created":"2026-10-01","correspondent":3,"correspondent_name":"ACME d.o.o.","document_type":2,"document_type_name":"Invoice","snippet":"…the [[[invoice]]]\n total is 24.50…","mime":"application/pdf","tags":[]}]}`)
		case "Bearer pd_bob":
			_, _ = io.WriteString(w, `{"count":1,"offset":0,"limit":6,"results":[{"id":9,"title":"","created":"2025-03-15T00:00:00Z","mime":"image/jpeg","tags":[]}]}`)
		default:
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
		}
	}))
}

// Each person searches Pocket Documents with their own token and gets their
// own documents; links point at the app's public address when one is set.
func TestLinkSearchDocumentsPerPerson(t *testing.T) {
	h := newHarness(t)
	alice, aTok := h.user("alice@example.com")
	bob, bTok := h.user("bob@example.com")
	carol, cTok := h.user("carol@example.com")
	_, dTok := h.user("dave@example.com")
	documents := fakeDocuments(t)
	defer documents.Close()
	connectApp(t, h, alice.ID, "documents", documents.URL, "pd_alice")
	connectApp(t, h, bob.ID, "documents", documents.URL, "pd_bob")
	_ = h.st.SetUserSetting(context.Background(), bob.ID, "app.documents.public_url", "https://documents.example.org/")
	connectApp(t, h, carol.ID, "documents", documents.URL, "pd_wrong")

	var a, b, c, d linkAnswer
	h.do(aTok, "GET", "/api/v1/tools/link?q=invoice", nil, &a)
	h.do(bTok, "GET", "/api/v1/tools/link?q=invoice", nil, &b)
	h.do(cTok, "GET", "/api/v1/tools/link?q=invoice", nil, &c)
	h.do(dTok, "GET", "/api/v1/tools/link?q=invoice", nil, &d)
	if s := a.section("documents"); len(s) != 1 || s[0].ID != "doc-5" || len(a.Errors) != 0 {
		t.Errorf("alice %+v", a)
	}
	if len(a.Sections) != 1 || a.Sections[0].Title != "Pocket Documents" {
		t.Errorf("alice sections %+v", a.Sections)
	}
	if s := b.section("documents"); len(s) != 1 || s[0].ID != "doc-9" || s[0].Title != "Document 9" || s[0].Detail != "15 Mar 2025" || s[0].URL != "https://documents.example.org/documents/9" {
		t.Errorf("bob %+v", b)
	}
	if len(c.Sections) != 0 || !strings.Contains(c.Errors["documents"], "refused the key") || strings.Contains(c.Errors["documents"], "pd_wrong") {
		t.Errorf("carol %+v", c)
	}
	// Not connected: left out, no error.
	if len(d.Sections) != 0 || len(d.Errors) != 0 {
		t.Errorf("dave %+v", d)
	}

	// The link tool is offered with Pocket Documents alone.
	var tools map[string]bool
	h.do(aTok, "GET", "/api/v1/tools", nil, &tools)
	if !tools["link"] {
		t.Errorf("tools %v", tools)
	}
	h.do(dTok, "GET", "/api/v1/tools", nil, &tools)
	if tools["link"] {
		t.Errorf("dave tools %v", tools)
	}
	// Off means off, even with a token typed in.
	_ = h.st.SetUserSetting(context.Background(), alice.ID, "app.documents.mode", "off")
	var off linkAnswer
	h.do(aTok, "GET", "/api/v1/tools/link?q=invoice", nil, &off)
	if len(off.Sections) != 0 || len(off.Errors) != 0 {
		t.Errorf("off %+v", off)
	}
}

// Settings → Integrations lists Pocket Documents as an app; Paperless is gone.
func TestIntegrationsDocumentsReplacesPaperless(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("alice@example.com")
	var out struct {
		Items []integrationView `json:"items"`
	}
	h.do(tok, "GET", "/api/v1/integrations", nil, &out)
	found := false
	for _, i := range out.Items {
		if i.Name == "paperless" {
			t.Errorf("paperless still listed: %+v", i)
		}
		if i.Kind == "app" && i.Name == "documents" {
			found = i.Title == "Pocket Documents" && i.KeyLabel == "API token" && i.Mode == "off"
		}
	}
	if !found {
		t.Errorf("documents missing: %+v", out.Items)
	}
	if code := h.do(tok, "PUT", "/api/v1/integrations/svc/paperless", map[string]string{"mode": "off"}, nil); code != 404 {
		t.Errorf("svc/paperless PUT %d", code)
	}
	for _, u := range serviceUses {
		if u.Type == "paperless" {
			t.Error("paperless still in serviceUses")
		}
	}
	if sc := useScopes("documents"); len(sc) != 1 || sc[0] != "documents:read" {
		t.Errorf("documents scopes %v", sc)
	}

	// The test button reaches the app with the typed-in token.
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" || r.Header.Get("Authorization") != "Bearer pd_alice" {
			w.WriteHeader(401)
			return
		}
		_, _ = io.WriteString(w, `{"id":1,"email":"alice@example.com"}`)
	}))
	defer documents.Close()
	if code := h.do(tok, "PUT", "/api/v1/integrations/app/documents", map[string]string{"mode": "direct", "url": documents.URL, "key": "pd_alice"}, nil); code != 200 {
		t.Fatalf("PUT %d", code)
	}
	var res struct {
		OK  bool   `json:"ok"`
		Via string `json:"via"`
	}
	if code := h.do(tok, "POST", "/api/v1/integrations/app/documents/test", nil, &res); code != 200 || !res.OK || res.Via != "direct" {
		t.Errorf("test %d %+v", code, res)
	}
}

func TestBookmarkTitles(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("alice@example.com")
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/og":
			_, _ = io.WriteString(w, `<html><head><title>Fallback</title><meta content="The real &amp; proper [title]" property="og:title"></head></html>`)
		case "/title":
			_, _ = io.WriteString(w, "<head>\n<title>\n  Pocket&#8217;s   &quot;guide&quot;\n  &#x2014; part 2\n</title>")
		case "/redirect":
			http.Redirect(w, r, "/og", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/broken":
			w.WriteHeader(500)
		default:
			_, _ = io.WriteString(w, "no title here")
		}
	}))
	defer site.Close()
	base := strings.Replace(site.URL, "127.0.0.1", "localhost", 1)
	for _, c := range []struct{ path, title string }{
		{"/og", "The real & proper [title]"},
		{"/title", "Pocket’s \"guide\" — part 2"},
		{"/redirect", "The real & proper [title]"},
		{"/loop", "localhost"},
		{"/broken", "localhost"},
		{"/none", "localhost"},
	} {
		var out struct{ Title, URL, Markdown string }
		if code := h.do(tok, "POST", "/api/v1/tools/bookmark", map[string]string{"url": base + c.path}, &out); code != 200 {
			t.Fatalf("%s: %d", c.path, code)
		}
		if out.Title != c.title || out.URL != base+c.path || out.Markdown != webLinkMarkdown(c.title, base+c.path) {
			t.Errorf("%s: %+v", c.path, out)
		}
	}
	var og struct{ Markdown string }
	h.do(tok, "POST", "/api/v1/tools/bookmark", map[string]string{"url": base + "/og"}, &og)
	if og.Markdown != "[The real & proper title]("+base+"/og)" {
		t.Errorf("brackets kept: %q", og.Markdown)
	}
	// The scheme is added; addresses of other kinds are not web pages.
	if u, ok := webAddress("example.com/x"); !ok || u.String() != "https://example.com/x" {
		t.Errorf("webAddress: %v %v", u, ok)
	}
	for _, bad := range []string{"mailto:me@example.com", "ftp://example.com/x", "javascript:alert(1)", "hello", "", "a b.com"} {
		if code := h.do(tok, "POST", "/api/v1/tools/bookmark", map[string]string{"url": bad}, nil); code != 400 {
			t.Errorf("%q → %d, want 400", bad, code)
		}
	}
	if got := webLinkMarkdown("A [b]\nc", "https://x.org/a (1)"); got != "[A b c](https://x.org/a%20%281%29)" {
		t.Errorf("markdown %q", got)
	}
}
