package server

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// Nextcloud Notes speaks a small REST API (apps/notes/api/v1, docs in the
// nextcloud/notes repository) that phone apps and the Pocket Homelab app use.
// This server answers it for the person's notes wherever they live, so those
// apps work against Pocket Notes by changing only the server address. Sign
// in with Basic auth: the email (or anything) and an API token of this app.

const ncAPIVersions = "0.2, 1.3"

// ncNote is a note in Nextcloud Notes' shape.
type ncNote struct {
	ID       int64  `json:"id"`
	ETag     string `json:"etag,omitempty"`
	Readonly bool   `json:"readonly"`
	Content  string `json:"content,omitempty"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category"`
	Favorite bool   `json:"favorite"`
	Modified int64  `json:"modified,omitempty"`
	// internalPath is what newer Nextcloud Notes answer; the Homelab app reads it.
	InternalPath string `json:"internalPath,omitempty"`
}

func (s *Server) toNC(ctx context.Context, uid int64, n *store.Note, exclude map[string]bool) ncNote {
	out := ncNote{ID: n.ID, ETag: n.Hash, Readonly: n.Readonly, Content: n.Content, Title: n.Title, Category: n.Category, Favorite: n.Favorite, Modified: n.Modified}
	if root := s.ncNotesPath(ctx, uid); root != "" {
		out.InternalPath = root + "/" + n.Path
	} else {
		out.InternalPath = n.Path
	}
	if exclude["content"] {
		out.Content = ""
	}
	if exclude["etag"] {
		out.ETag = ""
	}
	if exclude["title"] {
		out.Title = ""
	}
	return out
}

// ncNotesPath is the notes folder as Nextcloud would name it: the folder in
// the person's Nextcloud (or Pocket Files) when that is where the notes are,
// "Notes" otherwise.
func (s *Server) ncNotesPath(ctx context.Context, uid int64) string {
	st, _ := s.store.GetSettings(ctx, uid)
	if (st.StorageKind == "nextcloud" || st.StorageKind == "files") && st.StorageFolder != "" {
		return st.StorageFolder
	}
	return "Notes"
}

func (s *Server) nextcloudRoutes(r chi.Router) {
	notes := func(r chi.Router) {
		r.Use(s.ncAuth)
		for _, v := range []string{"v1", "v0.2"} {
			r.Get("/api/"+v+"/notes", s.ncList)
			r.Post("/api/"+v+"/notes", s.ncCreate)
			r.Get("/api/"+v+"/notes/{id}", s.ncGet)
			r.Put("/api/"+v+"/notes/{id}", s.ncUpdate)
			r.Delete("/api/"+v+"/notes/{id}", s.ncDelete)
			r.Get("/api/"+v+"/settings", s.ncSettings)
			r.Put("/api/"+v+"/settings", s.ncPutSettings)
			r.Get("/api/"+v+"/attachment/{id}", s.ncGetAttachment)
			r.Post("/api/"+v+"/attachment/{id}", s.ncUploadAttachment)
		}
		r.Get("/notes/{id}/attachment", s.ncGetAttachment)
		r.Post("/notes/{id}/attachment", s.ncUploadAttachment)
	}
	r.Route("/index.php/apps/notes", func(r chi.Router) {
		r.Get("/note/{id}", s.ncOpenInBrowser)
		r.Get("/", s.ncOpenInBrowser)
		r.Group(notes)
	})
	r.Route("/apps/notes", func(r chi.Router) {
		r.Get("/note/{id}", s.ncOpenInBrowser)
		r.Group(notes)
	})
	r.Get("/status.php", s.ncStatus)
	for _, ocs := range []string{"/ocs/v1.php", "/ocs/v2.php"} {
		r.Get(ocs+"/cloud/capabilities", s.ncCapabilities)
		r.Group(func(r chi.Router) {
			r.Use(s.ncAuth)
			r.Get(ocs+"/cloud/user", s.ncCurrentUser)
			r.Get(ocs+"/search/providers/notes/search", s.ncSearch)
			r.Delete(ocs+"/apps/files_lock/lock/{id}", s.ncUnlock)
		})
	}
	r.Group(func(r chi.Router) {
		r.Use(s.ncAuth)
		r.HandleFunc("/remote.php/dav/files/{user}", s.ncDAV)
		r.HandleFunc("/remote.php/dav/files/{user}/*", s.ncDAV)
		r.HandleFunc("/remote.php/webdav", s.ncDAV)
		r.HandleFunc("/remote.php/webdav/*", s.ncDAV)
	})
}

// ncAuth resolves the user as requireAuth does, answering 401 the way a
// Nextcloud server does (Basic challenge) so clients prompt for a password.
func (s *Server) ncAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.resolveUser(r)
		if err != nil || u == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Pocket Notes", charset="UTF-8"`)
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func ncHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Notes-API-Versions", ncAPIVersions)
}

func (s *Server) ncOpenInBrowser(w http.ResponseWriter, r *http.Request) {
	if id := chi.URLParam(r, "id"); id != "" {
		http.Redirect(w, r, "/note/"+url.PathEscape(id), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) ncList(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	ncHeaders(w)
	s.fresh(ctx, uid, 15*time.Second)
	q := r.URL.Query()
	f := store.NoteFilter{Content: true, Sort: "plain"}
	if q.Has("category") {
		c := q.Get("category")
		f.Category = &c
	}
	notes, err := s.store.ListNotes(ctx, uid, f)
	if err != nil {
		s.fail(w, err, "list notes")
		return
	}
	exclude := map[string]bool{}
	for _, e := range strings.Split(q.Get("exclude"), ",") {
		if e = strings.TrimSpace(e); e != "" {
			exclude[e] = true
		}
	}
	prune, _ := strconv.ParseInt(q.Get("pruneBefore"), 10, 64)
	// Chunks: cursor "<modified>-<id>" of the last note sent, oldest first.
	chunk := queryInt(r, "chunkSize")
	if chunk > 0 {
		for i, j := 0, len(notes)-1; i < j; i, j = i+1, j-1 {
			notes[i], notes[j] = notes[j], notes[i]
		}
		if cur := q.Get("chunkCursor"); cur != "" {
			start := 0
			for i, n := range notes {
				if fmt.Sprintf("%d-%d", n.Modified, n.ID) == cur {
					start = i + 1
					break
				}
			}
			notes = notes[start:]
		}
		if len(notes) > chunk {
			last := notes[chunk-1]
			w.Header().Set("X-Notes-Chunk-Cursor", fmt.Sprintf("%d-%d", last.Modified, last.ID))
			w.Header().Set("X-Notes-Chunk-Pending", strconv.Itoa(len(notes)-chunk))
			notes = notes[:chunk]
		}
	}
	out := make([]any, 0, len(notes))
	var tag strings.Builder
	for i := range notes {
		n := &notes[i]
		tag.WriteString(strconv.FormatInt(n.ID, 10) + n.Hash + strconv.FormatBool(n.Favorite) + n.Category + ";")
		if prune > 0 && n.Modified < prune {
			out = append(out, map[string]int64{"id": n.ID})
			continue
		}
		out = append(out, s.toNC(ctx, uid, n, exclude))
	}
	etag := `"` + hashOf(tag.String()+q.Get("exclude")+q.Get("category")) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag && prune == 0 && chunk == 0 {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ncGet(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	ncHeaders(w)
	if _, err := s.store.GetNote(ctx, uid, id); err != nil {
		s.fail(w, err, "get note")
		return
	}
	s.refreshNote(ctx, uid, id)
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "get note")
		return
	}
	exclude := map[string]bool{}
	for _, e := range strings.Split(r.URL.Query().Get("exclude"), ",") {
		exclude[strings.TrimSpace(e)] = true
	}
	w.Header().Set("ETag", `"`+n.Hash+`"`)
	if r.Header.Get("If-None-Match") == `"`+n.Hash+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, s.toNC(ctx, uid, n, exclude))
}

type ncInput struct {
	Title    *string `json:"title"`
	Content  *string `json:"content"`
	Category *string `json:"category"`
	Modified *int64  `json:"modified"`
	Favorite *bool   `json:"favorite"`
}

func (s *Server) ncCreate(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	ncHeaders(w)
	var in ncInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Category == nil {
		empty := ""
		in.Category = &empty // Nextcloud Notes puts a note without a category at the top
	}
	n, err := s.createNote(ctx, uid, NoteInput{Title: in.Title, Content: in.Content, Category: in.Category, Favorite: in.Favorite, Modified: in.Modified}, "nextcloud-api")
	if err != nil {
		s.fail(w, err, "create note")
		return
	}
	w.Header().Set("ETag", `"`+n.Hash+`"`)
	writeJSON(w, http.StatusOK, s.toNC(ctx, uid, n, nil))
}

func (s *Server) ncUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	ncHeaders(w)
	var in ncInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := s.updateNote(ctx, uid, id, NoteInput{Title: in.Title, Content: in.Content, Category: in.Category, Favorite: in.Favorite, Modified: in.Modified}, r.Header.Get("If-Match"), "nextcloud-api")
	var ce *conflictError
	if errors.As(err, &ce) {
		// Nextcloud answers 412 with the note as it is now.
		w.Header().Set("ETag", `"`+ce.Note.Hash+`"`)
		writeJSON(w, http.StatusPreconditionFailed, s.toNC(ctx, uid, ce.Note, nil))
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "read-only") {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		s.fail(w, err, "save note")
		return
	}
	w.Header().Set("ETag", `"`+n.Hash+`"`)
	writeJSON(w, http.StatusOK, s.toNC(ctx, uid, n, nil))
}

func (s *Server) ncDelete(w http.ResponseWriter, r *http.Request) {
	ncHeaders(w)
	if err := s.deleteNote(r.Context(), me(r), idParam(r, "id"), "nextcloud-api"); err != nil {
		s.fail(w, err, "delete note")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) ncSettings(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	ncHeaders(w)
	st, _ := s.store.GetSettings(ctx, uid)
	writeJSON(w, http.StatusOK, map[string]string{"notesPath": s.ncNotesPath(ctx, uid), "fileSuffix": st.FileSuffix, "noteMode": map[bool]string{true: "rich", false: "edit"}[st.Editor == "rich"]})
}

// ncPutSettings takes the file suffix and editor mode; the notes folder is
// chosen in Settings → Storage here, not by a client.
func (s *Server) ncPutSettings(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		FileSuffix string `json:"fileSuffix"`
		NoteMode   string `json:"noteMode"`
	}
	_ = decode(r, &in)
	if in.FileSuffix == ".md" || in.FileSuffix == ".txt" {
		_ = s.store.SetUserSetting(ctx, uid, "file_suffix", in.FileSuffix)
	}
	switch in.NoteMode {
	case "rich":
		_ = s.store.SetUserSetting(ctx, uid, "editor", "rich")
	case "edit", "preview":
		_ = s.store.SetUserSetting(ctx, uid, "editor", "markdown")
	}
	s.ncSettings(w, r)
}

func (s *Server) ncGetAttachment(w http.ResponseWriter, r *http.Request) {
	s.handleGetAttachment(w, r)
}

func (s *Server) ncUploadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	n, err := s.store.GetNote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "a file is required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the file could not be read")
		return
	}
	rel, err := s.storeAttachment(ctx, uid, n, hdr.Filename, data)
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"filename": rel})
}

func (s *Server) ncStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"installed": true, "maintenance": false, "needsDbUpgrade": false, "version": "31.0.0.0", "versionstring": "31.0.0",
		"edition": "", "productname": "Pocket Notes", "extendedSupport": false})
}

func ocs(w http.ResponseWriter, r *http.Request, status int, data any) {
	meta := map[string]any{"status": "ok", "statuscode": 200, "message": "OK"}
	if strings.HasPrefix(r.URL.Path, "/ocs/v1.php") {
		meta["statuscode"] = 100
	}
	if status >= 300 {
		meta["status"], meta["statuscode"], meta["message"] = "failure", status, http.StatusText(status)
	}
	writeJSON(w, status, map[string]any{"ocs": map[string]any{"meta": meta, "data": data}})
}

func (s *Server) ncCapabilities(w http.ResponseWriter, r *http.Request) {
	ocs(w, r, http.StatusOK, map[string]any{
		"version": map[string]any{"major": 31, "minor": 0, "micro": 0, "string": "31.0.0", "edition": "", "extendedSupport": false},
		"capabilities": map[string]any{
			"core":  map[string]any{"pollinterval": 60, "webdav-root": "remote.php/webdav"},
			"notes": map[string]any{"api_version": []string{"0.2", "1.3"}, "version": "4.12.0", "pocket_notes": Version},
		},
	})
}

func (s *Server) ncCurrentUser(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	ocs(w, r, http.StatusOK, map[string]any{"id": u.Email, "display-name": firstNonEmpty(u.Name, u.Email), "displayname": firstNonEmpty(u.Name, u.Email), "email": u.Email, "enabled": true})
}

// ncSearch is Nextcloud's unified search, Notes provider only.
func (s *Server) ncSearch(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	term := strings.TrimSpace(r.URL.Query().Get("term"))
	limit := clampInt(queryInt(r, "limit"), 1, 100)
	if queryInt(r, "limit") == 0 {
		limit = 10
	}
	entries := []map[string]any{}
	if term != "" {
		hits, err := s.store.Search(ctx, uid, term, store.NoteFilter{}, limit)
		if err != nil {
			ocs(w, r, http.StatusInternalServerError, nil)
			return
		}
		base := strings.TrimRight(firstNonEmpty(s.cfg.BaseURL, "http://"+r.Host), "/")
		for _, h := range hits {
			sub := strings.NewReplacer("[[[", "", "]]]", "").Replace(h.Snippet)
			entries = append(entries, map[string]any{"thumbnailUrl": "", "title": h.Title, "subline": sub, "resourceUrl": base + "/index.php/apps/notes/note/" + strconv.FormatInt(h.ID, 10),
				"icon": "icon-notes", "rounded": false, "attributes": map[string]any{}})
		}
	}
	ocs(w, r, http.StatusOK, map[string]any{"name": "Notes", "isPaginated": false, "entries": entries, "cursor": nil})
}

// ncUnlock: there are no file locks here (writes are serialised), so a
// release always succeeds.
func (s *Server) ncUnlock(w http.ResponseWriter, r *http.Request) {
	ocs(w, r, http.StatusOK, []any{})
}

// ---- WebDAV, the part Nextcloud Notes clients use ----

// davPath maps a WebDAV request path to a path inside the person's notes
// folder. above is set for a folder on the way down to it (it exists, but
// shows only the next step); ok is false for anything else.
func (s *Server) davPath(ctx context.Context, uid int64, r *http.Request) (rel string, isRoot, above, ok bool) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/remote.php/dav/files/"):
		rest := strings.TrimPrefix(p, "/remote.php/dav/files/")
		_, rest, _ = strings.Cut(rest, "/") // the user segment
		p = rest
	case strings.HasPrefix(p, "/remote.php/webdav"):
		p = strings.TrimPrefix(p, "/remote.php/webdav")
	}
	if dec, err := url.PathUnescape(p); err == nil {
		p = dec
	}
	clean, err := storage.Clean(p)
	if err != nil {
		return "", false, false, false
	}
	root, _ := storage.Clean(s.ncNotesPath(ctx, uid))
	switch {
	case clean == root:
		return "", true, false, true
	case root != "" && strings.HasPrefix(clean, root+"/"):
		return strings.TrimPrefix(clean, root+"/"), false, false, true
	case clean == "" || strings.HasPrefix(root+"/", clean+"/"):
		return clean, false, true, true
	}
	return "", false, false, false
}

func (s *Server) ncDAV(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	rel, isRoot, above, ok := s.davPath(ctx, uid, r)
	if !ok {
		http.Error(w, "Only the notes folder is here.", http.StatusNotFound)
		return
	}
	if above {
		switch r.Method {
		case "PROPFIND":
			items := []davItem{{href: r.URL.Path, dir: true}}
			if r.Header.Get("Depth") == "1" {
				root, _ := storage.Clean(s.ncNotesPath(ctx, uid))
				next := strings.TrimPrefix(root, rel)
				next = strings.Split(strings.TrimPrefix(next, "/"), "/")[0]
				items = append(items, davItem{href: strings.TrimRight(r.URL.Path, "/") + "/" + escapeDAV(next), dir: true})
			}
			s.davMultistatus(w, r, items)
		case "MKCOL":
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodOptions:
			w.Header().Set("DAV", "1, 3")
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "Only the notes folder is here.", http.StatusForbidden)
		}
		return
	}
	l, err := s.lib(ctx, uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	reindex := func() {
		if _, err := s.syncUser(ctx, uid, true); err != nil {
			s.log.Warn("sync after a WebDAV change", "err", err)
		}
	}
	switch r.Method {
	case "PROPFIND":
		depth := r.Header.Get("Depth")
		var items []davItem
		l.mu.Lock()
		var e storage.Entry
		if isRoot || rel == "" {
			e = storage.Entry{IsDir: true}
		} else {
			e, err = l.backend.Stat(ctx, rel)
		}
		if err == nil {
			items = append(items, davItem{href: r.URL.Path, entry: e, dir: e.IsDir})
			if e.IsDir && depth == "1" {
				var all []storage.Entry
				all, err = l.backend.List(ctx)
				prefix := ""
				if rel != "" {
					prefix = rel + "/"
				}
				for _, c := range all {
					if strings.HasPrefix(c.Path, prefix) && !strings.Contains(strings.TrimPrefix(c.Path, prefix), "/") {
						items = append(items, davItem{href: strings.TrimRight(r.URL.Path, "/") + "/" + escapeDAV(path.Base(c.Path)), entry: c, dir: c.IsDir})
					}
				}
			}
		}
		l.mu.Unlock()
		if err != nil {
			davErr(w, err)
			return
		}
		s.davMultistatus(w, r, items)
	case http.MethodGet, http.MethodHead:
		l.mu.Lock()
		data, e, err := l.backend.Read(ctx, rel)
		l.mu.Unlock()
		if err != nil {
			davErr(w, err)
			return
		}
		w.Header().Set("ETag", `"`+e.ETag+`"`)
		serveFile(w, rel, data, e.ETag)
	case http.MethodPut:
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 200<<20))
		if err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		l.mu.Lock()
		var e storage.Entry
		if r.Header.Get("If-None-Match") == "*" {
			e, err = l.backend.Create(ctx, rel, data)
			if errors.Is(err, storage.ErrExists) {
				err = storage.ErrPrecondition
			}
		} else {
			e, err = l.backend.Write(ctx, rel, data, strings.Trim(r.Header.Get("If-Match"), `"`))
		}
		l.mu.Unlock()
		if err != nil {
			davErr(w, err)
			return
		}
		if isNoteFile(rel) {
			reindex()
		}
		w.Header().Set("ETag", `"`+e.ETag+`"`)
		w.WriteHeader(http.StatusCreated)
	case "MKCOL":
		l.mu.Lock()
		_, statErr := l.backend.Stat(ctx, rel)
		if statErr == nil {
			l.mu.Unlock()
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		err := l.backend.Mkdir(ctx, rel)
		l.mu.Unlock()
		if err != nil {
			davErr(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		if isRoot || rel == "" {
			http.Error(w, "The notes folder itself cannot be deleted.", http.StatusForbidden)
			return
		}
		l.mu.Lock()
		err := l.backend.Delete(ctx, rel)
		l.mu.Unlock()
		if err != nil {
			davErr(w, err)
			return
		}
		reindex()
		w.WriteHeader(http.StatusNoContent)
	case "MOVE":
		dest, err := url.Parse(r.Header.Get("Destination"))
		if err != nil {
			http.Error(w, "bad destination", http.StatusBadRequest)
			return
		}
		dr := r.Clone(ctx)
		dr.URL = &url.URL{Path: dest.Path}
		to, toRoot, toAbove, ok := s.davPath(ctx, uid, dr)
		if !ok || toRoot || toAbove || to == "" {
			http.Error(w, "the destination is outside the notes folder", http.StatusForbidden)
			return
		}
		l.mu.Lock()
		_, err = l.backend.Move(ctx, rel, to)
		l.mu.Unlock()
		if err != nil {
			davErr(w, err)
			return
		}
		reindex()
		w.WriteHeader(http.StatusCreated)
	case "PROPPATCH":
		// Favourite marks are set through the Notes API here; accept and ignore.
		s.davMultistatus(w, r, []davItem{{href: r.URL.Path}})
	case http.MethodOptions:
		w.Header().Set("DAV", "1, 3")
		w.Header().Set("Allow", "OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, MOVE, PROPFIND, PROPPATCH")
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func escapeDAV(name string) string { return url.PathEscape(name) }

func davErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, storage.ErrPrecondition):
		w.WriteHeader(http.StatusPreconditionFailed)
	case errors.Is(err, storage.ErrExists):
		w.WriteHeader(http.StatusPreconditionFailed)
	case errors.Is(err, storage.ErrInvalidPath):
		w.WriteHeader(http.StatusForbidden)
	case errors.Is(err, storage.ErrLocked):
		w.WriteHeader(http.StatusLocked)
	default:
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

type davItem struct {
	href  string
	entry storage.Entry
	dir   bool
}

func (s *Server) davMultistatus(w http.ResponseWriter, r *http.Request, items []davItem) {
	type prop struct {
		ETag     string `xml:"d:getetag,omitempty"`
		Modified string `xml:"d:getlastmodified,omitempty"`
		Length   *int64 `xml:"d:getcontentlength,omitempty"`
		Type     *struct {
			Collection *struct{} `xml:"d:collection"`
		} `xml:"d:resourcetype"`
		FileID   string `xml:"oc:fileid,omitempty"`
		Favorite int    `xml:"oc:favorite"`
		Lock     int    `xml:"nc:lock"`
	}
	type response struct {
		Href     string `xml:"d:href"`
		Propstat struct {
			Prop   prop   `xml:"d:prop"`
			Status string `xml:"d:status"`
		} `xml:"d:propstat"`
	}
	type multistatus struct {
		XMLName   xml.Name   `xml:"d:multistatus"`
		D         string     `xml:"xmlns:d,attr"`
		OC        string     `xml:"xmlns:oc,attr"`
		NC        string     `xml:"xmlns:nc,attr"`
		Responses []response `xml:"d:response"`
	}
	ms := multistatus{D: "DAV:", OC: "http://owncloud.org/ns", NC: "http://nextcloud.org/ns"}
	for _, it := range items {
		var resp response
		resp.Href = it.href
		if it.dir && !strings.HasSuffix(resp.Href, "/") {
			resp.Href += "/"
		}
		p := &resp.Propstat.Prop
		p.Type = &struct {
			Collection *struct{} `xml:"d:collection"`
		}{}
		if it.dir {
			p.Type.Collection = &struct{}{}
		} else {
			size := it.entry.Size
			p.Length = &size
		}
		if it.entry.ETag != "" {
			p.ETag = `"` + it.entry.ETag + `"`
		}
		if !it.entry.Modified.IsZero() {
			p.Modified = it.entry.Modified.UTC().Format(http.TimeFormat)
		}
		p.FileID = it.entry.FileID
		if it.entry.Favorite {
			p.Favorite = 1
		}
		resp.Propstat.Status = "HTTP/1.1 200 OK"
		ms.Responses = append(ms.Responses, resp)
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(ms)
}

func init() {
	// chi answers 405 to methods it does not know.
	for _, m := range []string{"PROPFIND", "PROPPATCH", "MKCOL", "MOVE", "COPY", "LOCK", "UNLOCK", "REPORT"} {
		chi.RegisterMethod(m)
	}
}
