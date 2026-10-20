package server

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Pictures from Immich and files from Nextcloud, into a note: copied next to
// the note (the note keeps working when the original moves), or linked.

// ---- raw requests ----

// remoteError is a service's answer that was not a success.
type remoteError struct {
	Name   string
	Status int
	Text   string
}

func (e *remoteError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("%s refused the key (HTTP %d)", serviceNames[e.Name], e.Status)
	case http.StatusNotFound:
		return serviceNames[e.Name] + " has no such item"
	}
	if e.Text != "" {
		return fmt.Sprintf("%s: HTTP %d: %s", serviceNames[e.Name], e.Status, e.Text)
	}
	return fmt.Sprintf("%s: HTTP %d", serviceNames[e.Name], e.Status)
}

// remoteStatus is the HTTP status of a failed answer, or 0.
func remoteStatus(err error) int {
	var re *remoteError
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

// fetchRaw sends a request and answers the body (at most limit bytes) with
// the answer's headers. A body over the limit is an error, not a cut file.
func (s *Server) fetchRaw(ctx context.Context, name, method, target string, hdr map[string]string, body string, limit int64) ([]byte, http.Header, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "PocketNotes/"+Version)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := s.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, context.DeadlineExceeded
		}
		return nil, nil, fmt.Errorf("%s does not answer: %w", serviceNames[name], err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		text := strings.TrimSpace(string(msg))
		if strings.HasPrefix(text, "<") || strings.HasPrefix(text, "{") {
			text = ""
		}
		return nil, res.Header, &remoteError{Name: name, Status: res.StatusCode, Text: text}
	}
	if res.ContentLength > limit {
		return nil, res.Header, fmt.Errorf("the file is larger than %d MB", limit>>20)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, res.Header, err
	}
	if int64(len(data)) > limit {
		return nil, res.Header, fmt.Errorf("the file is larger than %d MB", limit>>20)
	}
	return data, res.Header, nil
}

// ---- Immich ----

var assetIDRe = regexp.MustCompile(`^[0-9A-Za-z-]{8,64}$`)

func immichHeaders(c *svcCred) map[string]string {
	return map[string]string{"x-api-key": c.Key}
}

// immichAsset is the part of an Immich asset the tools read.
type immichAsset struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	OriginalFileName string `json:"originalFileName"`
	FileCreatedAt    string `json:"fileCreatedAt"`
	LocalDateTime    string `json:"localDateTime"`
	Width            *int   `json:"width"`
	Height           *int   `json:"height"`
	ExifInfo         *struct {
		ExifImageWidth  *float64 `json:"exifImageWidth"`
		ExifImageHeight *float64 `json:"exifImageHeight"`
	} `json:"exifInfo"`
}

// day is the day the picture was taken, as people say it: "1 October 2026".
func (a immichAsset) day() string {
	for _, v := range []string{a.LocalDateTime, a.FileCreatedAt} {
		if len(v) >= 10 {
			if t, err := time.Parse("2006-01-02", v[:10]); err == nil {
				return t.Format("2 January 2006")
			}
		}
	}
	return ""
}

func (a immichAsset) size() (int, int) {
	if a.Width != nil && a.Height != nil && *a.Width > 0 {
		return *a.Width, *a.Height
	}
	if a.ExifInfo != nil && a.ExifInfo.ExifImageWidth != nil && a.ExifInfo.ExifImageHeight != nil {
		return int(*a.ExifInfo.ExifImageWidth), int(*a.ExifInfo.ExifImageHeight)
	}
	return 0, 0
}

// immichSearch is the answer of both /api/search/metadata and /api/search/smart.
type immichSearch struct {
	Assets struct {
		Items    []immichAsset `json:"items"`
		NextPage any           `json:"nextPage"`
	} `json:"assets"`
}

const photoPageSize = 60

type photoItem struct {
	ID       string `json:"id"`
	Date     string `json:"date"`
	Filename string `json:"filename"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Type     string `json:"type"`
}

// handleToolPhotos lists pictures: newest first, favourites, or what a smart
// search finds. Videos are left out.
func (s *Server) handleToolPhotos(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := max(queryInt(r, "page"), 1)
	c, err := s.service(ctx, uid, "immich")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body := map[string]any{"size": photoPageSize, "page": page, "type": "IMAGE", "visibility": "timeline"}
	if queryBool(r, "favorites") {
		body["isFavorite"] = true
	}
	endpoint := "/api/search/metadata"
	if q != "" {
		endpoint = "/api/search/smart"
		body["query"] = q
	} else {
		body["order"] = "desc"
	}
	var res immichSearch
	if err := s.callJSON(cctx, "immich", http.MethodPost, c.API(endpoint), immichHeaders(c), body, &res); err != nil {
		toolErr(w, err)
		return
	}
	items := []photoItem{}
	for _, a := range res.Assets.Items {
		if a.Type != "" && a.Type != "IMAGE" {
			continue
		}
		wd, ht := a.size()
		items = append(items, photoItem{ID: a.ID, Date: a.FileCreatedAt, Filename: a.OriginalFileName, Width: wd, Height: ht, Type: firstNonEmpty(a.Type, "IMAGE")})
	}
	next := 0
	switch v := res.Assets.NextPage.(type) {
	case string:
		if n, err := strconv.Atoi(v); err == nil && n > page {
			next = n
		} else if v != "" {
			next = page + 1
		}
	case float64:
		if int(v) > page {
			next = int(v)
		} else {
			next = page + 1
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_page": next, "query": q})
}

// handleToolPhotoThumb passes a picture's thumbnail on, so the key stays here.
func (s *Server) handleToolPhotoThumb(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	id := chi.URLParam(r, "asset")
	if !assetIDRe.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "invalid photo id")
		return
	}
	size := r.URL.Query().Get("size")
	if size != "preview" {
		size = "thumbnail"
	}
	c, err := s.service(ctx, uid, "immich")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, hdr, err := s.fetchRaw(cctx, "immich", http.MethodGet, c.API("/api/assets/"+id+"/thumbnail?size="+size), immichHeaders(c), "", 50<<20)
	if err != nil {
		if remoteStatus(err) == http.StatusNotFound {
			writeErr(w, http.StatusNotFound, "no such photo")
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

// dispositionName is the file name in a Content-Disposition header.
func dispositionName(h string) string {
	if h == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(h); err == nil {
		return params["filename"]
	}
	return ""
}

// photoAttachment fetches one picture: a screen-size JPEG (preview), which
// every browser shows, or the original as the camera wrote it.
func (s *Server) photoAttachment(ctx context.Context, c *svcCred, id string, original bool) (name, ctype string, data []byte, err error) {
	var a immichAsset
	if err := s.callJSON(ctx, "immich", http.MethodGet, c.API("/api/assets/"+id), immichHeaders(c), nil, &a); err != nil {
		return "", "", nil, err
	}
	if original {
		data, h, err := s.fetchRaw(ctx, "immich", http.MethodGet, c.API("/api/assets/"+id+"/original"), immichHeaders(c), "", 200<<20)
		if err != nil {
			return "", "", nil, err
		}
		return firstNonEmpty(dispositionName(h.Get("Content-Disposition")), a.OriginalFileName, id+".jpg"), h.Get("Content-Type"), data, nil
	}
	data, h, err := s.fetchRaw(ctx, "immich", http.MethodGet, c.API("/api/assets/"+id+"/thumbnail?size=preview"), immichHeaders(c), "", 50<<20)
	if err != nil {
		return "", "", nil, err
	}
	ctype = strings.ToLower(h.Get("Content-Type"))
	ext := ".jpg"
	switch {
	case strings.Contains(ctype, "webp"):
		ext = ".webp"
	case strings.Contains(ctype, "png"):
		ext = ".png"
	}
	stem := strings.TrimSuffix(a.OriginalFileName, path.Ext(a.OriginalFileName))
	if stem == "" {
		stem = id
	}
	return stem + ext, firstNonEmpty(ctype, "image/jpeg"), data, nil
}

// handleToolPhotosAttach puts pictures into a note, in the order given:
// copies stored with the note, or links to the photos in Immich.
func (s *Server) handleToolPhotosAttach(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		NoteID int64    `json:"note_id"`
		IDs    []string `json:"ids"`
		Mode   string   `json:"mode"`
		Size   string   `json:"size"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := s.store.GetNote(ctx, uid, in.NoteID)
	if err != nil {
		s.fail(w, err, "photos")
		return
	}
	if len(in.IDs) == 0 || len(in.IDs) > 50 {
		writeErr(w, http.StatusBadRequest, "pick between 1 and 50 photos")
		return
	}
	for _, id := range in.IDs {
		if !assetIDRe.MatchString(id) {
			writeErr(w, http.StatusBadRequest, "invalid photo id")
			return
		}
	}
	c, err := s.service(ctx, uid, "immich")
	if err != nil {
		toolErr(w, err)
		return
	}
	one := func(id string) (string, error) {
		if in.Mode == "link" {
			cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			var a immichAsset
			if err := s.callJSON(cctx, "immich", http.MethodGet, c.API("/api/assets/"+id), immichHeaders(c), nil, &a); err != nil {
				return "", err
			}
			label := firstNonEmpty(a.OriginalFileName, "Photo")
			if d := a.day(); d != "" {
				label += ", " + d
			}
			return webLinkMarkdown("📷 "+label, c.Link("/photos/"+id)), nil
		}
		timeout := time.Minute
		if in.Size == "original" {
			timeout = 5 * time.Minute
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		name, ctype, data, err := s.photoAttachment(cctx, c, id, in.Size == "original")
		if err != nil {
			return "", err
		}
		rel, err := s.storeAttachment(ctx, uid, n, name, data)
		if err != nil {
			return "", err
		}
		return attachmentMarkdown(name, rel, ctype), nil
	}
	var blocks []string
	var failed []map[string]string
	var firstErr error
	for _, id := range in.IDs {
		md, err := one(id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, map[string]string{"id": id, "error": err.Error()})
			continue
		}
		blocks = append(blocks, md)
	}
	if len(blocks) == 0 {
		toolErr(w, firstErr)
		return
	}
	out := map[string]any{"markdown": strings.Join(blocks, "\n\n")}
	if len(failed) > 0 {
		out["failed"] = failed
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Nextcloud files ----

// cloudFile is one entry of a person's Nextcloud files.
type cloudFile struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Mime     string `json:"mime"`
	FileID   string `json:"file_id"`
}

// cloudUser is the Nextcloud account the files belong to.
func cloudUser(c *svcCred) (string, error) {
	u := c.Field("USERNAME")
	if u == "" {
		return "", errors.New("Nextcloud has no username: add it in Settings → Integrations")
	}
	return u, nil
}

func cloudHeaders(c *svcCred) map[string]string {
	return map[string]string{"Authorization": basicAuth(c.Field("USERNAME"), firstNonEmpty(c.Field("APP_PASSWORD"), c.Key)), "OCS-APIRequest": "true"}
}

// cloudPath cleans a path in a person's files: leading "/", no "." or ".."
// segments, no control characters. ok is false for a path that is not allowed.
func cloudPath(p string) (string, bool) {
	var parts []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "":
			continue
		case ".", "..":
			return "", false
		}
		for _, r := range seg {
			if r < 32 || r == 127 {
				return "", false
			}
		}
		parts = append(parts, seg)
	}
	return "/" + strings.Join(parts, "/"), true
}

// cloudDavRoot is the WebDAV address of a person's files.
func cloudDavRoot(user string) string { return "/remote.php/dav/files/" + url.PathEscape(user) }

// cloudEscape percent-encodes each segment of a clean path.
func cloudEscape(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// cloudFileLink is where a person opens a file in Nextcloud's web page.
func cloudFileLink(c *svcCred, f cloudFile) string {
	if f.FileID != "" {
		return c.Link("/index.php/f/" + f.FileID)
	}
	dir := path.Dir(f.Path)
	if f.Dir {
		dir = f.Path
	}
	return c.Link("/index.php/apps/files/?dir=" + url.QueryEscape(dir) + "&scrollto=" + url.QueryEscape(path.Base(f.Path)))
}

const cloudProps = `<d:displayname/><d:getcontentlength/><d:getcontenttype/><d:getlastmodified/><d:resourcetype/><oc:fileid/>`

type cloudMultistatus struct {
	Responses []struct {
		Href     string `xml:"DAV: href"`
		Propstat []struct {
			Status string `xml:"DAV: status"`
			Prop   struct {
				LastModified string `xml:"DAV: getlastmodified"`
				Length       string `xml:"DAV: getcontentlength"`
				ContentType  string `xml:"DAV: getcontenttype"`
				ResourceType struct {
					Collection *struct{} `xml:"DAV: collection"`
				} `xml:"DAV: resourcetype"`
				FileID string `xml:"http://owncloud.org/ns fileid"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

// parseCloudDav reads a multistatus answer into files under the person's
// root (rootPath is the unescaped path of the WebDAV root). Other hrefs are
// left out.
func parseCloudDav(data []byte, rootPath string) ([]cloudFile, error) {
	var ms cloudMultistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, errors.New("Nextcloud answered something other than WebDAV")
	}
	rootPath = strings.TrimRight(rootPath, "/")
	var out []cloudFile
	for _, r := range ms.Responses {
		href := r.Href
		if u, err := url.Parse(href); err == nil {
			href = u.EscapedPath()
		}
		dec, err := url.PathUnescape(href)
		if err != nil {
			continue
		}
		dec = strings.TrimRight(dec, "/")
		var rel string
		switch {
		case dec == rootPath:
			rel = "/"
		case strings.HasPrefix(dec, rootPath+"/"):
			rel = strings.TrimPrefix(dec, rootPath)
		default:
			continue
		}
		p, ok := cloudPath(rel)
		if !ok {
			continue
		}
		f := cloudFile{Path: p, Name: path.Base(p)}
		if p == "/" {
			f.Name = ""
		}
		found := false
		for _, ps := range r.Propstat {
			if !strings.Contains(ps.Status, " 200") {
				continue
			}
			found = true
			pr := ps.Prop
			if pr.ResourceType.Collection != nil {
				f.Dir = true
			}
			if pr.LastModified != "" {
				if t, err := http.ParseTime(pr.LastModified); err == nil {
					f.Modified = t.UTC().Format(time.RFC3339)
				}
			}
			if pr.Length != "" {
				f.Size, _ = strconv.ParseInt(pr.Length, 10, 64)
			}
			if pr.ContentType != "" {
				f.Mime = pr.ContentType
			}
			if id := strings.TrimLeft(strings.TrimSpace(pr.FileID), "0"); id != "" && strings.Trim(id, "0123456789") == "" {
				f.FileID = id
			}
		}
		if found {
			out = append(out, f)
		}
	}
	return out, nil
}

// cloudPropfind lists a path in a person's files (depth "0" or "1").
func (s *Server) cloudPropfind(ctx context.Context, c *svcCred, p, depth string) ([]cloudFile, error) {
	user, err := cloudUser(c)
	if err != nil {
		return nil, err
	}
	hdr := cloudHeaders(c)
	hdr["Depth"] = depth
	hdr["Content-Type"] = "application/xml; charset=utf-8"
	body := `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop>` + cloudProps + `</d:prop></d:propfind>`
	data, _, err := s.fetchRaw(ctx, "nextcloud", "PROPFIND", c.API(cloudDavRoot(user)+cloudEscape(p)), hdr, body, 32<<20)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.API(cloudDavRoot(user)))
	if err != nil {
		return nil, err
	}
	return parseCloudDav(data, u.Path)
}

// handleToolFiles lists one folder of the person's Nextcloud files: folders
// first, then by name, hidden entries left out.
func (s *Server) handleToolFiles(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	p, ok := cloudPath(r.URL.Query().Get("path"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "that path is not allowed")
		return
	}
	c, err := s.service(ctx, uid, "nextcloud")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := s.cloudPropfind(cctx, c, p, "1")
	if err != nil {
		if remoteStatus(err) == http.StatusNotFound {
			writeErr(w, http.StatusNotFound, "no such folder")
			return
		}
		toolErr(w, err)
		return
	}
	items := []cloudFile{}
	for _, f := range list {
		if f.Path == p || strings.HasPrefix(f.Name, ".") {
			continue
		}
		items = append(items, f)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Dir != items[j].Dir {
			return items[i].Dir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	parent := ""
	if p != "/" {
		parent = path.Dir(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": p, "parent": parent, "items": items})
}

// handleToolFilesAttach puts a Nextcloud file into a note: a copy stored with
// the note, or a link to it in Nextcloud.
func (s *Server) handleToolFilesAttach(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		NoteID int64  `json:"note_id"`
		Path   string `json:"path"`
		Mode   string `json:"mode"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := s.store.GetNote(ctx, uid, in.NoteID)
	if err != nil {
		s.fail(w, err, "files")
		return
	}
	p, ok := cloudPath(in.Path)
	if !ok || p == "/" {
		writeErr(w, http.StatusBadRequest, "that path is not allowed")
		return
	}
	c, err := s.service(ctx, uid, "nextcloud")
	if err != nil {
		toolErr(w, err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := s.cloudPropfind(cctx, c, p, "0")
	if err == nil && len(list) == 0 {
		err = &remoteError{Name: "nextcloud", Status: http.StatusNotFound}
	}
	if err != nil {
		if remoteStatus(err) == http.StatusNotFound {
			writeErr(w, http.StatusNotFound, "no such file")
			return
		}
		toolErr(w, err)
		return
	}
	f := list[0]
	if in.Mode == "link" {
		writeJSON(w, http.StatusOK, map[string]any{"markdown": webLinkMarkdown(f.Name, cloudFileLink(c, f))})
		return
	}
	if f.Dir {
		writeErr(w, http.StatusBadRequest, "a folder cannot be copied into a note; link it instead")
		return
	}
	if f.Size > 200<<20 {
		writeErr(w, http.StatusRequestEntityTooLarge, "the file is larger than 200 MB; link it instead")
		return
	}
	user, _ := cloudUser(c)
	dctx, dcancel := context.WithTimeout(ctx, 10*time.Minute)
	defer dcancel()
	data, hdr, err := s.fetchRaw(dctx, "nextcloud", http.MethodGet, c.API(cloudDavRoot(user)+cloudEscape(p)), cloudHeaders(c), "", 200<<20)
	if err != nil {
		toolErr(w, err)
		return
	}
	rel, err := s.storeAttachment(ctx, uid, n, f.Name, data)
	if err != nil {
		s.fail(w, err, "files")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"markdown": attachmentMarkdown(f.Name, rel, firstNonEmpty(f.Mime, hdr.Get("Content-Type")))})
}

// ---- Nextcloud search, for the link tool ----

// cloudFullText asks Nextcloud's Full Text Search app (names, document text,
// OCR) for files.
func (s *Server) cloudFullText(ctx context.Context, c *svcCred, q string, limit int) ([]cloudFile, error) {
	req, _ := json.Marshal(map[string]any{"providers": []string{"files"}, "search": q, "page": 1, "size": limit, "options": map[string]any{}})
	var res struct {
		Status int `json:"status"`
		Result []struct {
			Provider struct {
				ID string `json:"id"`
			} `json:"provider"`
			Documents []struct {
				ID   any `json:"id"`
				Info struct {
					Path  string  `json:"path"`
					File  string  `json:"file"`
					Type  string  `json:"type"`
					Size  float64 `json:"size"`
					Mime  string  `json:"mime"`
					Mtime float64 `json:"mtime"`
				} `json:"info"`
			} `json:"documents"`
			Meta struct {
				TimedOut bool `json:"timedOut"`
			} `json:"meta"`
		} `json:"result"`
	}
	if err := s.callJSON(ctx, "nextcloud", http.MethodGet, c.API("/index.php/apps/fulltextsearch/v1/remote?request="+url.QueryEscape(string(req))), cloudHeaders(c), nil, &res); err != nil {
		return nil, err
	}
	if res.Status != 1 {
		return nil, errors.New("Nextcloud full-text search failed")
	}
	for _, r := range res.Result {
		if r.Provider.ID != "files" {
			continue
		}
		if r.Meta.TimedOut {
			return nil, errors.New("Nextcloud full-text search timed out")
		}
		out := []cloudFile{}
		for _, d := range r.Documents {
			p, ok := cloudPath(d.Info.Path)
			if !ok || p == "/" {
				continue
			}
			f := cloudFile{Path: p, Name: firstNonEmpty(d.Info.File, path.Base(p)), Dir: d.Info.Type == "dir", Size: int64(d.Info.Size), Mime: d.Info.Mime}
			switch id := d.ID.(type) {
			case float64:
				f.FileID = strconv.FormatInt(int64(id), 10)
			case string:
				if id != "" && strings.Trim(id, "0123456789") == "" {
					f.FileID = id
				}
			}
			if d.Info.Mtime > 0 {
				f.Modified = time.Unix(int64(d.Info.Mtime), 0).UTC().Format(time.RFC3339)
			}
			out = append(out, f)
			if len(out) == limit {
				break
			}
		}
		return out, nil
	}
	return nil, errors.New("Nextcloud's full-text search has no files provider")
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// cloudNameSearch finds files whose name contains q (a WebDAV SEARCH, newest
// first), for when the full-text index is not there.
func (s *Server) cloudNameSearch(ctx context.Context, c *svcCred, q string, limit int) ([]cloudFile, error) {
	user, err := cloudUser(c)
	if err != nil {
		return nil, err
	}
	var esc strings.Builder
	for _, r := range q {
		if strings.ContainsRune(`\%_`, r) {
			esc.WriteByte('\\')
		}
		esc.WriteRune(r)
	}
	body := `<?xml version="1.0"?><d:searchrequest xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:basicsearch>` +
		`<d:select><d:prop>` + cloudProps + `</d:prop></d:select>` +
		`<d:from><d:scope><d:href>` + xmlText("/files/"+url.PathEscape(user)+"/") + `</d:href><d:depth>infinity</d:depth></d:scope></d:from>` +
		`<d:where><d:and><d:not><d:is-collection/></d:not><d:like><d:prop><d:displayname/></d:prop><d:literal>` + xmlText("%"+esc.String()+"%") + `</d:literal></d:like></d:and></d:where>` +
		`<d:orderby><d:order><d:prop><d:getlastmodified/></d:prop><d:descending/></d:order></d:orderby>` +
		`<d:limit><d:nresults>` + strconv.Itoa(limit) + `</d:nresults></d:limit></d:basicsearch></d:searchrequest>`
	hdr := cloudHeaders(c)
	hdr["Content-Type"] = "application/xml; charset=utf-8"
	data, _, err := s.fetchRaw(ctx, "nextcloud", "SEARCH", c.API("/remote.php/dav/"), hdr, body, 16<<20)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.API(cloudDavRoot(user)))
	if err != nil {
		return nil, err
	}
	files, err := parseCloudDav(data, u.Path)
	if err != nil {
		return nil, err
	}
	if len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}
