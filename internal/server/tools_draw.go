package server

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Drawings from Pocket Draw in a note. The note holds a picture whose address
// is the drawing's own picture in Draw, below the drawing's page:
//
//	![Kitchen plan](https://draw.example.com/d/12/picture.svg "Kitchen plan")
//
// so the note reads well anywhere, and Draw finds it again with
// GET /api/v1/mentions?url=https://draw.example.com/d/12 (mentions.go: an
// address below the page counts). Draw's picture needs the person's sign-in
// there, so the editor shows it through this server instead
// (/api/v1/draw/{id}/picture.svg), fetched with the person's Core link: the
// note keeps Draw's address, the page shows the picture at once and the
// newest one each time.

// drawKinds are what a drawing is, in words people use.
var drawKinds = map[string]string{"sketch": "Sketch", "mermaid": "Text diagram", "drawio": "Diagram"}

// drawTitle is a drawing's name as it goes into the note: no brackets,
// quotes, parentheses or line breaks (they would end the Markdown early),
// and "Drawing 12" when it has none.
func drawTitle(title string, id int64) string {
	title = strings.NewReplacer("[", "", "]", "", `"`, "", "(", "", ")", "").Replace(title)
	title = strings.Join(strings.Fields(title), " ")
	return firstNonEmpty(title, "Drawing "+strconv.FormatInt(id, 10))
}

// drawEmbed is the Markdown for a drawing shown in a note: Draw's picture,
// with the drawing's name as its words and its title.
func drawEmbed(c *svcCred, id int64, title string) string {
	t := drawTitle(title, id)
	return "![" + t + "](" + c.Link("/d/"+strconv.FormatInt(id, 10)+"/picture.svg") + ` "` + t + `")`
}

type drawing struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	FolderName string `json:"folder_name"`
	EditedAt   string `json:"edited_at"`
	HasPreview bool   `json:"has_preview"`
	PreviewRev int64  `json:"preview_rev"`
	Rev        int64  `json:"rev"`
}

// detail is the line under a drawing's name: what it is and where it is kept.
func (d drawing) detail() string {
	var parts []string
	for _, p := range []string{drawKinds[d.Kind], strings.TrimSpace(d.FolderName)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// drawings asks Draw for the person's drawings, the last edited first.
func (s *Server) drawings(ctx context.Context, c *svcCred, q string, limit int) ([]drawing, error) {
	var res struct {
		Drawings []drawing `json:"drawings"`
	}
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}, "sort": {"edited"}}
	if err := s.callJSON(ctx, "draw", http.MethodGet, c.API("/api/v1/drawings?"+v.Encode()), map[string]string{"Authorization": "Bearer " + c.Key}, nil, &res); err != nil {
		return nil, err
	}
	return res.Drawings, nil
}

// linkDrawings: drawings for "Link to something"; picking one shows it in the
// note, as the Drawing tool does.
func linkDrawings(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	list, err := s.drawings(ctx, c, q, linkPerSource)
	if err != nil {
		return nil, "", err
	}
	items := []linkItem{}
	for _, d := range list {
		if len(items) == linkPerSource {
			break
		}
		id := strconv.FormatInt(d.ID, 10)
		items = append(items, linkItem{ID: "drawing-" + id, Title: drawTitle(d.Title, d.ID), Detail: d.detail(), URL: c.Link("/d/" + id), Icon: "drawing", Markdown: drawEmbed(c, d.ID, d.Title)})
	}
	return items, "", nil
}

type drawItem struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
	EditedAt string `json:"edited_at"`
	Picture  string `json:"picture"`
	URL      string `json:"url"`
	Markdown string `json:"markdown"`
}

// handleToolDraw lists drawings for the Drawing picker: the last edited
// first, or those matching q. Not connected is an answer, not a failure:
// the picker says where to connect Draw.
func (s *Server) handleToolDraw(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := map[string]any{"items": []drawItem{}, "connected": false}
	c, err := s.appCred(cctx, uid, "draw", useScopes("draw")...)
	if err != nil {
		out["error"] = drawErr(err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["connected"] = true
	list, err := s.drawings(cctx, c, strings.TrimSpace(r.URL.Query().Get("q")), 24)
	if err != nil {
		out["error"] = drawErr(err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	items := []drawItem{}
	for _, d := range list {
		id := strconv.FormatInt(d.ID, 10)
		items = append(items, drawItem{ID: d.ID, Title: drawTitle(d.Title, d.ID), Kind: d.Kind, Detail: d.detail(), EditedAt: d.EditedAt,
			Picture: "/api/v1/draw/" + id + "/picture.svg?r=" + strconv.FormatInt(d.PreviewRev, 10), URL: c.Link("/d/" + id), Markdown: drawEmbed(c, d.ID, d.Title)})
	}
	out["items"] = items
	writeJSON(w, http.StatusOK, out)
}

// drawErr is a failure in words for the picker.
func drawErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Pocket Draw took too long to answer"
	}
	return err.Error()
}

// handleToolDrawCreate starts a drawing in Draw from a note: a sketch or a
// text diagram, with the name given. The answer is the Markdown that shows it
// and the drawing's page, which the page opens to draw in.
func (s *Server) handleToolDrawCreate(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Kind {
	case "":
		in.Kind = "sketch"
	case "sketch", "mermaid":
	default:
		writeErr(w, http.StatusBadRequest, "kind must be sketch or mermaid")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := s.appCred(cctx, uid, "draw", useScopes("draw")...)
	if err != nil {
		toolErr(w, err)
		return
	}
	var d drawing
	body := map[string]string{"kind": in.Kind, "title": strings.TrimSpace(in.Title)}
	if err := s.callJSON(cctx, "draw", http.MethodPost, c.API("/api/v1/drawings"), map[string]string{"Authorization": "Bearer " + c.Key}, body, &d); err != nil {
		toolErr(w, err)
		return
	}
	if d.ID <= 0 {
		writeErr(w, http.StatusBadGateway, "Pocket Draw did not say which drawing it made")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": d.ID, "title": drawTitle(d.Title, d.ID), "url": c.Link("/d/" + strconv.FormatInt(d.ID, 10)), "markdown": drawEmbed(c, d.ID, d.Title)})
}

// handleToolAddresses says where people open the Pocket apps whose things the
// editor shows (so far Draw: its pictures in a note are recognised by it).
// An app that is not set up is left out.
func (s *Server) handleToolAddresses(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out := map[string]string{}
	if u := s.appPublicURL(cctx, uid, "draw"); u != "" {
		out["draw"] = u
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- the picture ----

const drawPictureCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:"

// handleDrawPicture shows a drawing's picture in a note, fetched from Draw as
// the person. Whatever goes wrong, the note shows a quiet grey card instead
// of a broken picture.
func (s *Server) handleDrawPicture(w http.ResponseWriter, r *http.Request) {
	s.serveDrawPicture(w, r, me(r), idParam(r, "id"))
}

// drawInNoteRe finds a drawing's picture address in a note: …/d/12/picture.svg.
var drawInNoteRe = regexp.MustCompile(`https?://[^\s<>()\[\]"']+/d/(\d+)/picture\.svg`)

// handlePublicShareDrawPicture is the picture on a shared note's page: only
// drawings the note itself shows, fetched as the person who shared it.
func (s *Server) handlePublicShareDrawPicture(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := idParam(r, "id")
	sh, err := s.store.ShareByToken(ctx, chi_token(r), false)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	n, err := s.store.GetNote(ctx, sh.UserID, sh.NoteID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	shown := false
	for _, m := range drawInNoteRe.FindAllStringSubmatch(n.Content, -1) {
		if m[1] == strconv.FormatInt(id, 10) {
			shown = true
			break
		}
	}
	if !shown {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.serveDrawPicture(w, r, sh.UserID, id)
}

func (s *Server) serveDrawPicture(w http.ResponseWriter, r *http.Request, uid, id int64) {
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid drawing id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := s.appCred(ctx, uid, "draw", useScopes("draw")...)
	if err != nil {
		drawPlaceholder(w, "Drawing not available here")
		return
	}
	data, _, err := s.fetchRaw(ctx, "draw", http.MethodGet, c.API("/api/v1/drawings/"+strconv.FormatInt(id, 10)+"/preview.svg"), map[string]string{"Authorization": "Bearer " + c.Key, "Accept": "image/svg+xml"}, "", 10<<20)
	if err != nil {
		if remoteStatus(err) == http.StatusNotFound {
			drawPlaceholder(w, "No picture yet — open it in Pocket Draw once")
		} else {
			s.log.Debug("draw picture", "id", id, "err", err)
			drawPlaceholder(w, "Drawing not available here")
		}
		return
	}
	// Anything but a picture is not shown, whatever Draw said it was.
	if !strings.Contains(string(data[:min(len(data), 4096)]), "<svg") {
		drawPlaceholder(w, "Drawing not available here")
		return
	}
	drawHeaders(w)
	w.Header().Set("Cache-Control", "private, max-age=30")
	_, _ = w.Write(data)
}

// drawHeaders: an SVG from elsewhere may carry script or reach out; as a
// document of its own it may do neither.
func drawHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Security-Policy", drawPictureCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// drawPlaceholder is a light grey card in the drawing's place, with one line.
func drawPlaceholder(w http.ResponseWriter, line string) {
	drawHeaders(w)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="480" height="270" viewBox="0 0 480 270">` +
		`<rect x="1" y="1" width="478" height="268" rx="12" fill="#f4f4f5" stroke="#d4d4d8" stroke-width="2"/>` +
		`<g fill="none" stroke="#a1a1aa" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M226 104l28 28M214 144l40-40 12 12-40 40-16 4z"/></g>` +
		`<text x="240" y="196" text-anchor="middle" font-family="-apple-system, system-ui, sans-serif" font-size="16" fill="#71717a">` + html.EscapeString(line) + `</text></svg>`))
}
