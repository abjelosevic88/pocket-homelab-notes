package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

const (
	photoA = "0b1c2d3e-aaaa-bbbb-cccc-111122223333"
	photoB = "0b1c2d3e-aaaa-bbbb-cccc-444455556666"
)

// fakeImmich answers the search, asset, thumbnail and original routes.
func fakeImmich(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	assets := map[string]string{
		photoA: `{"id":"` + photoA + `","type":"IMAGE","originalFileName":"IMG_1.HEIC","fileCreatedAt":"2026-10-01T08:00:00.000Z","localDateTime":"2026-10-01T10:00:00.000Z","isFavorite":true,"width":4032,"height":3024}`,
		photoB: `{"id":"` + photoB + `","type":"IMAGE","originalFileName":"IMG_2.jpg","fileCreatedAt":"2026-09-30T08:00:00.000Z","localDateTime":"2026-09-30T10:00:00.000Z","isFavorite":false,"width":null,"height":null,"exifInfo":{"exifImageWidth":1000,"exifImageHeight":800}}`,
	}
	video := `{"id":"0b1c2d3e-aaaa-bbbb-cccc-777777777777","type":"VIDEO","originalFileName":"MOV_1.mov","fileCreatedAt":"2026-09-29T08:00:00.000Z"}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "im-key" {
			w.WriteHeader(401)
			return
		}
		p := r.URL.Path
		switch {
		case r.Method == "POST" && (p == "/api/search/metadata" || p == "/api/search/smart"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			body["_path"] = p
			*bodies = append(*bodies, body)
			next := `"2"`
			if body["page"].(float64) >= 2 {
				next = "null"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"albums":{"total":0,"count":0,"items":[],"facets":[]},"assets":{"total":3,"count":3,"items":[`+assets[photoA]+`,`+video+`,`+assets[photoB]+`],"facets":[],"nextPage":`+next+`}}`)
		case r.Method == "GET" && strings.HasPrefix(p, "/api/assets/"):
			rest := strings.TrimPrefix(p, "/api/assets/")
			id, tail, _ := strings.Cut(rest, "/")
			a, ok := assets[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			switch tail {
			case "":
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, a)
			case "thumbnail":
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = io.WriteString(w, "JPEG-"+r.URL.Query().Get("size")+"-"+id)
			case "original":
				w.Header().Set("Content-Type", "image/heic")
				w.Header().Set("Content-Disposition", `attachment; filename="IMG_1.HEIC"`)
				_, _ = io.WriteString(w, "HEIC-"+id)
			default:
				w.WriteHeader(404)
			}
		default:
			t.Errorf("immich: unexpected %s %s", r.Method, p)
			w.WriteHeader(500)
		}
	}))
}

func TestPhotosListAndThumb(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	var bodies []map[string]any
	im := fakeImmich(t, &bodies)
	defer im.Close()

	// Not connected: 412 with the service named.
	var nc map[string]any
	if c := h.do(tok, "GET", "/api/v1/tools/photos", nil, &nc); c != 412 || nc["not_connected"] != "immich" {
		t.Fatalf("not connected: %d %v", c, nc)
	}
	connectDirect(h, alice.ID, "immich", im.URL, "im-key", "")

	var out struct {
		Items    []photoItem `json:"items"`
		NextPage int         `json:"next_page"`
		Query    string      `json:"query"`
	}
	if c := h.do(tok, "GET", "/api/v1/tools/photos", nil, &out); c != 200 {
		t.Fatalf("photos: %d", c)
	}
	if len(out.Items) != 2 || out.NextPage != 2 {
		t.Fatalf("photos %+v", out)
	}
	if a := out.Items[0]; a.ID != photoA || a.Filename != "IMG_1.HEIC" || a.Width != 4032 || a.Height != 3024 || a.Date != "2026-10-01T08:00:00.000Z" || a.Type != "IMAGE" {
		t.Errorf("first %+v", a)
	}
	if b := out.Items[1]; b.Width != 1000 || b.Height != 800 {
		t.Errorf("exif size %+v", b)
	}
	if b := bodies[0]; b["_path"] != "/api/search/metadata" || b["order"] != "desc" || b["type"] != "IMAGE" || b["size"].(float64) != 60 || b["page"].(float64) != 1 || b["isFavorite"] != nil {
		t.Errorf("metadata body %v", b)
	}

	h.do(tok, "GET", "/api/v1/tools/photos?q=beach&page=2&favorites=1", nil, &out)
	if out.NextPage != 0 || out.Query != "beach" {
		t.Errorf("page 2 %+v", out)
	}
	if b := bodies[1]; b["_path"] != "/api/search/smart" || b["query"] != "beach" || b["page"].(float64) != 2 || b["isFavorite"] != true {
		t.Errorf("smart body %v", b)
	}

	res, err := http.DefaultClient.Do(authed(t, tok, h.ts.URL+"/api/v1/tools/photos/"+photoA+"/thumb?size=preview"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(data) != "JPEG-preview-"+photoA || res.Header.Get("Content-Type") != "image/jpeg" || res.Header.Get("Cache-Control") != "private, max-age=86400" {
		t.Errorf("thumb %d %q %v", res.StatusCode, data, res.Header)
	}
	for path, want := range map[string]int{
		"/api/v1/tools/photos/0b1c2d3e-aaaa-bbbb-cccc-000000000000/thumb": 404,
		"/api/v1/tools/photos/..%2F..%2Fusers/thumb":                      400,
		"/api/v1/tools/photos/a$b/thumb":                                  400,
	} {
		if c := h.do(tok, "GET", path, nil, nil); c != want {
			t.Errorf("%s → %d, want %d", path, c, want)
		}
	}
}

func authed(t *testing.T, tok, u string) *http.Request {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return req
}

func TestPhotosAttach(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	bob, bTok := h.user("bob@example.com")
	var bodies []map[string]any
	im := fakeImmich(t, &bodies)
	defer im.Close()
	connectDirect(h, alice.ID, "immich", im.URL, "im-key", "")
	connectDirect(h, bob.ID, "immich", im.URL, "im-key", "")
	_ = h.st.SetUserSetting(t.Context(), alice.ID, "svc.immich.public_url", "https://photos.example.org")

	var n store.Note
	if c := h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "Trip", "content": "# Trip"}, &n); c != 201 {
		t.Fatalf("create: %d", c)
	}
	var out struct {
		Markdown string              `json:"markdown"`
		Failed   []map[string]string `json:"failed"`
	}
	if c := h.do(tok, "POST", "/api/v1/tools/photos/attach", map[string]any{"note_id": n.ID, "ids": []string{photoB, photoA}}, &out); c != 200 {
		t.Fatalf("attach: %d %+v", c, out)
	}
	if out.Markdown != "![IMG_2.jpg](attachments/IMG_2.jpg)\n\n![IMG_1.jpg](attachments/IMG_1.jpg)" {
		t.Errorf("markdown %q", out.Markdown)
	}
	got, err := os.ReadFile(filepath.Join(h.s.builtinDir(alice.ID), "attachments", "IMG_1.jpg"))
	if err != nil || string(got) != "JPEG-preview-"+photoA {
		t.Errorf("stored preview %q %v", got, err)
	}

	// The original, as the camera wrote it.
	out.Markdown = ""
	h.do(tok, "POST", "/api/v1/tools/photos/attach", map[string]any{"note_id": n.ID, "ids": []string{photoA}, "size": "original"}, &out)
	if out.Markdown != "![IMG_1.HEIC](attachments/IMG_1.HEIC)" {
		t.Errorf("original markdown %q", out.Markdown)
	}
	if got, _ := os.ReadFile(filepath.Join(h.s.builtinDir(alice.ID), "attachments", "IMG_1.HEIC")); string(got) != "HEIC-"+photoA {
		t.Errorf("stored original %q", got)
	}

	// Links instead of copies.
	h.do(tok, "POST", "/api/v1/tools/photos/attach", map[string]any{"note_id": n.ID, "ids": []string{photoA}, "mode": "link"}, &out)
	if out.Markdown != "[📷 IMG_1.HEIC, 1 October 2026](https://photos.example.org/photos/"+photoA+")" {
		t.Errorf("link markdown %q", out.Markdown)
	}

	// One missing photo among good ones is reported, the rest still go in.
	out.Failed = nil
	h.do(tok, "POST", "/api/v1/tools/photos/attach", map[string]any{"note_id": n.ID, "ids": []string{photoB, "0b1c2d3e-aaaa-bbbb-cccc-000000000000"}, "mode": "link"}, &out)
	if len(out.Failed) != 1 || !strings.HasPrefix(out.Markdown, "[📷 IMG_2.jpg") {
		t.Errorf("partial %+v", out)
	}

	// Bob cannot put anything into Alice's note.
	if c := h.do(bTok, "POST", "/api/v1/tools/photos/attach", map[string]any{"note_id": n.ID, "ids": []string{photoA}}, nil); c != 404 {
		t.Errorf("bob attach → %d, want 404", c)
	}
	if entries, _ := os.ReadDir(filepath.Join(h.s.builtinDir(bob.ID), "attachments")); len(entries) != 0 {
		t.Errorf("bob's folder got %d files", len(entries))
	}
}

func TestFilesListAndAttach(t *testing.T) {
	h := newHarness(t)
	alice, tok := h.user("alice@example.com")
	_, bTok := h.user("bob@example.com")
	nc := fakeNextcloud(t, true)
	defer nc.Close()
	connectDirect(h, alice.ID, "nextcloud", nc.URL, "app-pass", "alice")

	var list struct {
		Path   string      `json:"path"`
		Parent string      `json:"parent"`
		Items  []cloudFile `json:"items"`
	}
	if c := h.do(tok, "GET", "/api/v1/tools/files?path=/Documents", nil, &list); c != 200 {
		t.Fatalf("files: %d", c)
	}
	if list.Path != "/Documents" || list.Parent != "/" || len(list.Items) != 3 {
		t.Fatalf("list %+v", list)
	}
	if a := list.Items[0]; a.Name != "Archive" || !a.Dir || a.Path != "/Documents/Archive" {
		t.Errorf("folder first %+v", a)
	}
	if p := list.Items[2]; p.Name != "z report.pdf" || p.Path != "/Documents/z report.pdf" || p.Dir || p.Size != 123 || p.Mime != "application/pdf" || p.FileID != "456" || p.Modified != "2026-10-13T09:30:00Z" {
		t.Errorf("file %+v", p)
	}
	if list.Items[1].Name != "a.png" {
		t.Errorf("order %+v", list.Items)
	}
	if c := h.do(tok, "GET", "/api/v1/tools/files?path=/Documents/../../bob", nil, nil); c != 400 {
		t.Errorf(".. → %d", c)
	}
	if c := h.do(tok, "GET", "/api/v1/tools/files?path=/Nope", nil, nil); c != 404 {
		t.Errorf("missing folder → %d", c)
	}

	var n store.Note
	h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": "Taxes", "content": "x"}, &n)
	var out struct{ Markdown string }
	if c := h.do(tok, "POST", "/api/v1/tools/files/attach", map[string]any{"note_id": n.ID, "path": "/Documents/z report.pdf", "mode": "link"}, &out); c != 200 {
		t.Fatalf("link: %d", c)
	}
	if out.Markdown != "[z report.pdf]("+nc.URL+"/index.php/f/456)" {
		t.Errorf("link %q", out.Markdown)
	}
	if c := h.do(tok, "POST", "/api/v1/tools/files/attach", map[string]any{"note_id": n.ID, "path": "/Documents/z report.pdf"}, &out); c != 200 {
		t.Fatalf("copy: %d", c)
	}
	if out.Markdown != "[z report.pdf](attachments/z%20report.pdf)" {
		t.Errorf("copy %q", out.Markdown)
	}
	if got, _ := os.ReadFile(filepath.Join(h.s.builtinDir(alice.ID), "attachments", "z report.pdf")); string(got) != "%PDF-1.7" {
		t.Errorf("stored %q", got)
	}
	if c := h.do(tok, "POST", "/api/v1/tools/files/attach", map[string]any{"note_id": n.ID, "path": "/../x"}, nil); c != 400 {
		t.Errorf(".. attach → %d", c)
	}
	if c := h.do(bTok, "POST", "/api/v1/tools/files/attach", map[string]any{"note_id": n.ID, "path": "/Documents/z report.pdf"}, nil); c != 404 {
		t.Errorf("bob attach → %d, want 404", c)
	}
	// Bob has no Nextcloud: the tool says so.
	if c := h.do(bTok, "GET", "/api/v1/tools/files?path=/", nil, nil); c != 412 {
		t.Errorf("bob files → %d, want 412", c)
	}
}
