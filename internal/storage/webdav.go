package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Credentials answers the username and password for one request. Asked every
// time, so a key that comes from Pocket Core (and may be switched off there)
// is never kept here.
type Credentials func(ctx context.Context) (user, pass string, err error)

// WebDAV keeps notes in a folder on a WebDAV server. With Nextcloud set, it
// also reads Nextcloud's file ids and favourite marks and releases the file
// locks Nextcloud's own editor leaves behind.
type WebDAV struct {
	// Base is the address of the notes folder itself.
	Base string
	// MkBase and MkPath, when set, say how the notes folder is made when it is
	// missing: MkPath's segments one by one under MkBase (a Nextcloud user's
	// files root and "Notes").
	MkBase    string
	MkPath    string
	Creds     Credentials
	Nextcloud bool
	// ServerURL is Nextcloud's own address (for the files_lock API).
	ServerURL string
	Label     string
	HTTP      *http.Client
	UserAgent string

	basePath string // decoded path of Base, for reading hrefs
}

// NewWebDAV returns a generic WebDAV backend for the folder at base.
func NewWebDAV(base string, creds Credentials, label string) (*WebDAV, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("not a WebDAV address: %q", base)
	}
	w := &WebDAV{Base: u.String(), Creds: creds, Label: label, HTTP: &http.Client{Timeout: 60 * time.Second}}
	w.basePath = strings.TrimRight(u.Path, "/")
	return w, nil
}

// NewNextcloud returns a backend for folder (a path inside the user's files,
// "Notes" or "Documents/Notes") on the Nextcloud at server.
func NewNextcloud(server, user, folder string, creds Credentials) (*WebDAV, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	folder, err := Clean(folder)
	if err != nil {
		return nil, err
	}
	filesRoot := server + "/remote.php/dav/files/" + url.PathEscape(user)
	base := filesRoot
	if folder != "" {
		base += "/" + escapePath(folder)
	}
	label := "Nextcloud"
	if folder != "" {
		label += " · " + folder
	}
	w, err := NewWebDAV(base, creds, label)
	if err != nil {
		return nil, err
	}
	w.Nextcloud, w.ServerURL, w.MkBase, w.MkPath = true, server, filesRoot, folder
	return w, nil
}

func (w *WebDAV) Kind() string {
	if w.Nextcloud {
		return "nextcloud"
	}
	return "webdav"
}

func (w *WebDAV) Describe() string { return w.Label }

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func (w *WebDAV) urlFor(p string) (string, error) {
	c, err := Clean(p)
	if err != nil {
		return "", err
	}
	if c == "" {
		return w.Base + "/", nil
	}
	return w.Base + "/" + escapePath(c), nil
}

func (w *WebDAV) do(ctx context.Context, method, target string, body []byte, hdr map[string]string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, err
	}
	if w.Creds != nil {
		user, pass, err := w.Creds(ctx)
		if err != nil {
			return nil, err
		}
		if user != "" || pass != "" {
			req.SetBasicAuth(user, pass)
		}
	}
	if w.UserAgent != "" {
		req.Header.Set("User-Agent", w.UserAgent)
	}
	if w.Nextcloud {
		req.Header.Set("OCS-APIRequest", "true")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	return w.HTTP.Do(req)
}

func statusErr(res *http.Response, what string) error {
	switch res.StatusCode {
	case http.StatusUnauthorized:
		return ErrAuth
	case http.StatusForbidden:
		return fmt.Errorf("%w (HTTP 403: no permission for %s)", ErrAuth, what)
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusPreconditionFailed:
		return ErrPrecondition
	case http.StatusLocked:
		return ErrLocked
	case http.StatusInsufficientStorage:
		return errors.New("the storage is full")
	}
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	text := strings.TrimSpace(string(msg))
	if i := strings.Index(text, "<s:message>"); i >= 0 {
		text = strings.TrimSuffix(text[i+11:], "</s:message>")
		if j := strings.Index(text, "<"); j >= 0 {
			text = text[:j]
		}
	} else if len(text) > 120 || strings.HasPrefix(text, "<") {
		text = ""
	}
	if text != "" {
		return fmt.Errorf("%s: HTTP %d: %s", what, res.StatusCode, text)
	}
	return fmt.Errorf("%s: HTTP %d", what, res.StatusCode)
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop><d:getetag/><d:getlastmodified/><d:getcontentlength/><d:resourcetype/><oc:fileid/><oc:favorite/></d:prop></d:propfind>`

type davMultistatus struct {
	Responses []struct {
		Href     string `xml:"DAV: href"`
		Propstat []struct {
			Status string `xml:"DAV: status"`
			Prop   struct {
				ETag         string `xml:"DAV: getetag"`
				LastModified string `xml:"DAV: getlastmodified"`
				Length       string `xml:"DAV: getcontentlength"`
				ResourceType struct {
					Collection *struct{} `xml:"DAV: collection"`
				} `xml:"DAV: resourcetype"`
				FileID   string `xml:"http://owncloud.org/ns fileid"`
				Favorite string `xml:"http://owncloud.org/ns favorite"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

// relFromHref turns a response's href into a path relative to the root.
func (w *WebDAV) relFromHref(href string) (string, bool) {
	if u, err := url.Parse(href); err == nil {
		href = u.EscapedPath()
	}
	dec, err := url.PathUnescape(href)
	if err != nil {
		dec = href
	}
	dec = strings.TrimRight(dec, "/")
	base := w.basePath
	if dec == base {
		return "", true
	}
	if !strings.HasPrefix(dec, base+"/") {
		return "", false
	}
	return strings.TrimPrefix(dec, base+"/"), true
}

func (w *WebDAV) propfind(ctx context.Context, p, depth string) ([]Entry, error) {
	target, err := w.urlFor(p)
	if err != nil {
		return nil, err
	}
	res, err := w.do(ctx, "PROPFIND", target, []byte(propfindBody), map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		return nil, statusErr(res, "list "+displayPath(p))
	}
	var ms davMultistatus
	if err := xml.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("list %s: unreadable answer: %w", displayPath(p), err)
	}
	var out []Entry
	for _, r := range ms.Responses {
		rel, ok := w.relFromHref(r.Href)
		if !ok {
			continue
		}
		e := Entry{Path: rel}
		found := false
		for _, ps := range r.Propstat {
			if !strings.Contains(ps.Status, " 200") {
				continue
			}
			found = true
			pr := ps.Prop
			if pr.ResourceType.Collection != nil {
				e.IsDir = true
			}
			if pr.ETag != "" {
				e.ETag = strings.Trim(pr.ETag, `"`)
			}
			if pr.LastModified != "" {
				if t, err := http.ParseTime(pr.LastModified); err == nil {
					e.Modified = t.UTC()
				}
			}
			if pr.Length != "" {
				e.Size, _ = strconv.ParseInt(pr.Length, 10, 64)
			}
			if pr.FileID != "" {
				e.FileID = strings.TrimLeft(pr.FileID, "0")
			}
			if pr.Favorite == "1" {
				e.Favorite = true
			}
		}
		if found {
			out = append(out, e)
		}
	}
	return out, nil
}

func displayPath(p string) string {
	if p == "" {
		return "the notes folder"
	}
	return p
}

func (w *WebDAV) Test(ctx context.Context) error {
	_, err := w.propfind(ctx, "", "0")
	if errors.Is(err, ErrNotFound) {
		return w.mkRoot(ctx)
	}
	return err
}

// mkRoot makes the notes folder itself, when the backend knows how.
func (w *WebDAV) mkRoot(ctx context.Context) error {
	if w.MkBase == "" {
		res, err := w.do(ctx, "MKCOL", w.Base+"/", nil, nil)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusMethodNotAllowed {
			return nil
		}
		return statusErr(res, "make the notes folder")
	}
	cur := w.MkBase
	for _, seg := range splitNonEmpty(w.MkPath) {
		cur += "/" + url.PathEscape(seg)
		res, err := w.do(ctx, "MKCOL", cur+"/", nil, nil)
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusMethodNotAllowed {
			return statusErr(res, "make the notes folder")
		}
	}
	return nil
}

func (w *WebDAV) RootETag(ctx context.Context) (string, error) {
	es, err := w.propfind(ctx, "", "0")
	if err != nil {
		return "", err
	}
	for _, e := range es {
		if e.Path == "" {
			return e.ETag, nil
		}
	}
	return "", nil
}

func (w *WebDAV) List(ctx context.Context) ([]Entry, error) {
	var out []Entry
	queue := []string{""}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		es, err := w.propfind(ctx, dir, "1")
		if err != nil {
			if dir == "" && errors.Is(err, ErrNotFound) {
				return []Entry{}, nil
			}
			return nil, err
		}
		for _, e := range es {
			if e.Path == dir || e.Path == "" {
				continue
			}
			if Hidden(e.Path) {
				continue
			}
			out = append(out, e)
			if e.IsDir {
				queue = append(queue, e.Path)
			}
		}
	}
	return out, nil
}

func (w *WebDAV) Stat(ctx context.Context, p string) (Entry, error) {
	c, err := Clean(p)
	if err != nil {
		return Entry{}, err
	}
	es, err := w.propfind(ctx, c, "0")
	if err != nil {
		return Entry{}, err
	}
	for _, e := range es {
		if e.Path == c {
			return e, nil
		}
	}
	if len(es) == 1 {
		es[0].Path = c
		return es[0], nil
	}
	return Entry{}, ErrNotFound
}

func (w *WebDAV) Read(ctx context.Context, p string) ([]byte, Entry, error) {
	target, err := w.urlFor(p)
	if err != nil {
		return nil, Entry{}, err
	}
	res, err := w.do(ctx, http.MethodGet, target, nil, nil)
	if err != nil {
		return nil, Entry{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, Entry{}, statusErr(res, "read "+p)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 256<<20))
	if err != nil {
		return nil, Entry{}, err
	}
	c, _ := Clean(p)
	e := Entry{Path: c, Size: int64(len(data)), ETag: strings.Trim(res.Header.Get("ETag"), `"`), FileID: strings.TrimLeft(res.Header.Get("OC-FileId"), "0")}
	if i := strings.Index(e.FileID, "oc"); i > 0 { // "00000123ocabc" → "123"
		e.FileID = e.FileID[:i]
	}
	if t, err := http.ParseTime(res.Header.Get("Last-Modified")); err == nil {
		e.Modified = t.UTC()
	}
	if e.ETag == "" {
		if st, err := w.Stat(ctx, c); err == nil {
			e = st
		}
	}
	return data, e, nil
}

// put sends the bytes, making missing folders once and releasing a
// Nextcloud lock once.
func (w *WebDAV) put(ctx context.Context, p string, data []byte, hdr map[string]string) error {
	target, err := w.urlFor(p)
	if err != nil {
		return err
	}
	madeDirs, unlocked := false, false
	for {
		res, err := w.do(ctx, http.MethodPut, target, data, hdr)
		if err != nil {
			return err
		}
		code := res.StatusCode
		if code >= 200 && code < 300 {
			res.Body.Close()
			return nil
		}
		if (code == http.StatusNotFound || code == http.StatusConflict) && !madeDirs {
			res.Body.Close()
			madeDirs = true
			if dir := parentDir(p); dir != "" {
				if err := w.Mkdir(ctx, dir); err != nil {
					return err
				}
			} else if err := w.mkRoot(ctx); err != nil {
				return err
			}
			continue
		}
		if code == http.StatusLocked && !unlocked && w.Nextcloud {
			res.Body.Close()
			unlocked = true
			if w.unlock(ctx, p) {
				continue
			}
			return ErrLocked
		}
		defer res.Body.Close()
		return statusErr(res, "save "+p)
	}
}

func parentDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// unlock releases the lock Nextcloud's rich editor keeps on a file for as long
// as its session lives (the files_lock app), which the owner may do.
func (w *WebDAV) unlock(ctx context.Context, p string) bool {
	e, err := w.Stat(ctx, p)
	if err != nil || e.FileID == "" || w.ServerURL == "" {
		return false
	}
	res, err := w.do(ctx, http.MethodDelete, w.ServerURL+"/ocs/v2.php/apps/files_lock/lock/"+e.FileID+"?format=json", nil, map[string]string{"Accept": "application/json"})
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK || res.StatusCode == http.StatusPreconditionFailed
}

func (w *WebDAV) Write(ctx context.Context, p string, data []byte, ifMatch string) (Entry, error) {
	hdr := map[string]string{"Content-Type": "text/markdown; charset=utf-8"}
	if ifMatch != "" {
		// If-Match is the real guard; servers that ignore it still get a look first.
		if cur, err := w.Stat(ctx, p); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Entry{}, ErrPrecondition
			}
			return Entry{}, err
		} else if cur.ETag != "" && cur.ETag != ifMatch {
			return Entry{}, ErrPrecondition
		}
		hdr["If-Match"] = `"` + ifMatch + `"`
	}
	if err := w.put(ctx, p, data, hdr); err != nil {
		return Entry{}, err
	}
	return w.Stat(ctx, p)
}

func (w *WebDAV) Create(ctx context.Context, p string, data []byte) (Entry, error) {
	return w.create(ctx, p, data, nil)
}

// CreateDated is Create with the modification time Nextcloud and ownCloud
// take from X-OC-Mtime; other servers ignore it and date the file now.
func (w *WebDAV) CreateDated(ctx context.Context, p string, data []byte, modified time.Time) (Entry, error) {
	return w.create(ctx, p, data, map[string]string{"X-OC-Mtime": strconv.FormatInt(modified.Unix(), 10)})
}

func (w *WebDAV) create(ctx context.Context, p string, data []byte, extra map[string]string) (Entry, error) {
	// If-None-Match is the real guard; not every server honours it, so look first.
	if _, err := w.Stat(ctx, p); err == nil {
		return Entry{}, ErrExists
	}
	hdr := map[string]string{"If-None-Match": "*", "Content-Type": contentType(p)}
	for k, v := range extra {
		hdr[k] = v
	}
	err := w.put(ctx, p, data, hdr)
	if errors.Is(err, ErrPrecondition) {
		return Entry{}, ErrExists
	}
	if err != nil {
		return Entry{}, err
	}
	return w.Stat(ctx, p)
}

func contentType(p string) string {
	switch {
	case strings.HasSuffix(strings.ToLower(p), ".md"):
		return "text/markdown; charset=utf-8"
	case strings.HasSuffix(strings.ToLower(p), ".txt"):
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func (w *WebDAV) Move(ctx context.Context, from, to string) (Entry, error) {
	src, err := w.urlFor(from)
	if err != nil {
		return Entry{}, err
	}
	dst, err := w.urlFor(to)
	if err != nil {
		return Entry{}, err
	}
	if src == dst {
		return w.Stat(ctx, to)
	}
	madeDirs, unlocked := false, false
	for {
		res, err := w.do(ctx, "MOVE", src, nil, map[string]string{"Destination": dst, "Overwrite": "F"})
		if err != nil {
			return Entry{}, err
		}
		code := res.StatusCode
		res.Body.Close()
		switch {
		case code >= 200 && code < 300:
			return w.Stat(ctx, to)
		case code == http.StatusPreconditionFailed:
			return Entry{}, ErrExists
		case (code == http.StatusConflict || code == http.StatusForbidden) && !madeDirs && parentDir(to) != "":
			madeDirs = true
			if err := w.Mkdir(ctx, parentDir(to)); err != nil {
				return Entry{}, err
			}
			continue
		case code == http.StatusLocked && !unlocked && w.Nextcloud:
			unlocked = true
			if w.unlock(ctx, from) {
				continue
			}
			return Entry{}, ErrLocked
		}
		return Entry{}, statusErr(res, "move "+from)
	}
}

func (w *WebDAV) Delete(ctx context.Context, p string) error {
	c, err := Clean(p)
	if err != nil {
		return err
	}
	if c == "" {
		return ErrInvalidPath
	}
	target, _ := w.urlFor(c)
	res, err := w.do(ctx, http.MethodDelete, target, nil, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusLocked && w.Nextcloud && w.unlock(ctx, c) {
		res2, err := w.do(ctx, http.MethodDelete, target, nil, nil)
		if err != nil {
			return err
		}
		defer res2.Body.Close()
		res = res2
	}
	if res.StatusCode < 300 || res.StatusCode == http.StatusNotFound {
		return nil
	}
	return statusErr(res, "delete "+c)
}

func (w *WebDAV) Mkdir(ctx context.Context, p string) error {
	c, err := Clean(p)
	if err != nil {
		return err
	}
	if c == "" {
		return w.mkRoot(ctx)
	}
	segs := strings.Split(c, "/")
	triedRoot := false
	for i := 1; i <= len(segs); i++ {
		target, _ := w.urlFor(strings.Join(segs[:i], "/"))
		res, err := w.do(ctx, "MKCOL", target+"/", nil, nil)
		if err != nil {
			return err
		}
		res.Body.Close()
		switch {
		case res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusMethodNotAllowed:
			continue
		case res.StatusCode == http.StatusConflict && i == 1 && !triedRoot:
			triedRoot = true
			if err := w.mkRoot(ctx); err != nil {
				return err
			}
			i--
			continue
		}
		return statusErr(res, "make folder "+c)
	}
	return nil
}

// SetFavorite sets Nextcloud's favourite mark, which Nextcloud Notes shows as
// the note's star.
func (w *WebDAV) SetFavorite(ctx context.Context, p string, on bool) error {
	if !w.Nextcloud {
		return nil
	}
	target, err := w.urlFor(p)
	if err != nil {
		return err
	}
	v := "0"
	if on {
		v = "1"
	}
	body := `<?xml version="1.0"?><d:propertyupdate xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:set><d:prop><oc:favorite>` + v + `</oc:favorite></d:prop></d:set></d:propertyupdate>`
	res, err := w.do(ctx, "PROPPATCH", target, []byte(body), map[string]string{"Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus && res.StatusCode >= 300 {
		return statusErr(res, "mark favourite")
	}
	return nil
}
