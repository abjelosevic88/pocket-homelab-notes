package server

// Templates: notes in the person's templates folder (Settings → Writing,
// "Templates" unless changed), so they stay plain files wherever the notes
// live. Beside the file a template keeps the title of the notes made from it
// ({{date}} and friends allowed), the folder they go to, and whether everyone
// on this server may use it (store.TemplateMeta).

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/notesmd"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// templateOut is a template as the app shows it.
type templateOut struct {
	store.Note
	Name         string            `json:"name"`
	TitlePattern string            `json:"title_pattern"` // its own, else from its first heading or its name
	Folder       string            `json:"folder"`        // "" = where the person is, else their default folder
	Everyone     bool              `json:"everyone"`      // everyone on this server may use it
	Owner        *store.SharedFrom `json:"owner,omitempty"`
	NextTitle    string            `json:"next_title"` // what a note made from it now is called
	Preview      []string          `json:"preview"`    // its first lines, as written
}

// previewLines are a template's first few non-empty lines, for its card.
func previewLines(content string) []string {
	out := []string{}
	for _, l := range strings.Split(content, "\n") {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		if r := []rune(l); len(r) > 90 {
			l = string(r[:90]) + "…"
		}
		out = append(out, l)
		if len(out) == 7 {
			break
		}
	}
	return out
}

func templatesFolder(st store.Settings) string { return notesmd.SafeFolder(st.TemplatesFolder) }

// defaultPattern is the title pattern of a template that has none of its
// own: its first heading when that has placeholders in it ("# Meeting,
// {{date}}"), else its name.
func defaultPattern(content string) string {
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if h := strings.TrimLeft(t, "#"); len(h) < len(t) && strings.HasPrefix(h, " ") && strings.Contains(h, "{{") {
			return strings.ReplaceAll(strings.TrimSpace(h), "{{title}}", "{{name}}")
		}
		break
	}
	return "{{name}}"
}

// newTitle is the title a note made from a template gets now.
func newTitle(meta store.TemplateMeta, name string, at time.Time, st store.Settings) string {
	pattern := strings.TrimSpace(meta.TitlePattern)
	if pattern == "" {
		return name
	}
	if t := strings.TrimSpace(fillPlaceholders(pattern, name, name, at, st)); t != "" {
		return t
	}
	return name
}

func (s *Server) dressTemplate(n *store.Note, meta store.TemplateMeta, owner *store.SharedFrom, st store.Settings) templateOut {
	if strings.TrimSpace(meta.TitlePattern) == "" {
		meta.TitlePattern = defaultPattern(n.Content)
	}
	preview := previewLines(n.Content)
	n.Content = ""
	return templateOut{Preview: preview, Note: *n, Name: n.Title, TitlePattern: meta.TitlePattern, Folder: meta.Folder, Everyone: meta.Shared, Owner: owner, NextTitle: newTitle(meta, n.Title, time.Now(), st)}
}

// templateFor finds template id for person uid: one of theirs (a note in
// their templates folder), or someone else's shared with everyone.
func (s *Server) templateFor(ctx context.Context, uid, id int64) (*store.Note, store.TemplateMeta, *store.SharedFrom, error) {
	var meta store.TemplateMeta
	owner, err := s.store.NoteOwner(ctx, id)
	if err != nil {
		return nil, meta, nil, err
	}
	n, err := s.store.GetNote(ctx, owner, id)
	if err != nil {
		return nil, meta, nil, err
	}
	ost, _ := s.store.GetSettings(ctx, owner)
	if !inFolder(n.Category, templatesFolder(ost)) {
		return nil, meta, nil, store.ErrNotFound
	}
	if meta, err = s.store.TemplateMeta(ctx, owner, id); err != nil {
		return nil, meta, nil, err
	}
	if owner == uid {
		return n, meta, nil, nil
	}
	if !meta.Shared {
		return nil, meta, nil, store.ErrNotFound
	}
	sf := &store.SharedFrom{OwnerID: owner}
	if u, err := s.store.GetUser(ctx, owner); err == nil {
		sf.Name, sf.Email = u.Name, u.Email
	}
	return n, meta, sf, nil
}

// handleTemplates lists the person's templates, then those other people
// share with everyone (?mine=1: only theirs).
func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	out := []templateOut{}
	if folder := templatesFolder(st); folder != "" {
		notes, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Category: &folder, Recursive: true, Sort: "title", Content: true})
		if err != nil {
			s.fail(w, err, "templates")
			return
		}
		metas, _ := s.store.TemplateMetas(ctx, uid)
		for i := range notes {
			meta, ok := metas[notes[i].ID]
			if !ok {
				meta = store.TemplateMeta{NoteID: notes[i].ID, UserID: uid}
			}
			out = append(out, s.dressTemplate(&notes[i], meta, nil, st))
		}
	}
	if !queryBool(r, "mine") {
		shared, err := s.store.SharedTemplates(ctx, uid)
		if err != nil {
			s.fail(w, err, "templates")
			return
		}
		for _, t := range shared {
			n, meta, sf, err := s.templateFor(ctx, uid, t.NoteID)
			if err != nil {
				continue // moved out of its owner's templates folder
			}
			out = append(out, s.dressTemplate(n, meta, sf, st))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTemplate gives one template with its placeholders filled in for the
// note it goes into (?title=), as the daily note's template is.
func (s *Server) handleTemplate(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	n, _, _, err := s.templateFor(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "template")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	writeJSON(w, http.StatusOK, map[string]string{"title": n.Title, "content": fillPlaceholders(n.Content, r.URL.Query().Get("title"), n.Title, time.Now(), st)})
}

// handleTemplatePreview shows what a title pattern gives today.
func (s *Server) handleTemplatePreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Pattern string `json:"pattern"`
		Name    string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), me(r))
	writeJSON(w, http.StatusOK, map[string]string{"title": notesmd.SafeName(newTitle(store.TemplateMeta{TitlePattern: in.Pattern}, strings.TrimSpace(in.Name), time.Now(), st))})
}

type templateIn struct {
	Name         *string `json:"name"`
	Content      *string `json:"content"`
	FromNoteID   int64   `json:"from_note_id"` // copy this note (or someone's shared template)
	TitlePattern *string `json:"title_pattern"`
	Folder       *string `json:"folder"`
	Everyone     *bool   `json:"everyone"`
}

func (in templateIn) apply(meta *store.TemplateMeta) {
	if in.TitlePattern != nil {
		meta.TitlePattern = strings.TrimSpace(*in.TitlePattern)
	}
	if in.Folder != nil {
		meta.Folder = notesmd.SafeFolder(*in.Folder)
	}
	if in.Everyone != nil {
		meta.Shared = *in.Everyone
	}
}

// headingAsTitle turns a note's first heading, when it is the note's title,
// into {{title}}: a note made from the template is headed by its own title.
func headingAsTitle(content, title string) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if h := strings.TrimLeft(t, "#"); len(h) < len(t) && len(t)-len(h) <= 6 && strings.HasPrefix(h, " ") && strings.TrimSpace(h) == title {
			lines[i] = t[:len(t)-len(h)] + " {{title}}"
		}
		break
	}
	return strings.Join(lines, "\n")
}

// handleCreateTemplate saves a template: a new one, or a copy of a note
// ("Save as template") or of someone's shared template.
func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in templateIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	content, name := "", ""
	var meta store.TemplateMeta
	if in.FromNoteID > 0 {
		if owner, _, err := s.noteAccess(ctx, uid, in.FromNoteID, false); err == nil {
			src, err := s.store.GetNote(ctx, owner, in.FromNoteID)
			if err != nil {
				s.fail(w, err, "save template")
				return
			}
			content, name = headingAsTitle(src.Content, src.Title), src.Title
			if t, m, _, err := s.templateFor(ctx, owner, in.FromNoteID); err == nil {
				content, meta = t.Content, m // a copy of a template stays one as it is
			}
		} else if src, m, _, err := s.templateFor(ctx, uid, in.FromNoteID); err == nil {
			content, name, meta = src.Content, src.Title, m
		} else {
			s.fail(w, err, "save template")
			return
		}
	}
	if in.Content != nil {
		content = *in.Content
	}
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		writeErr(w, http.StatusBadRequest, "give the template a name")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	folder := templatesFolder(st)
	if folder == "" {
		// Templates were switched off: they come back in the usual folder.
		st.TemplatesFolder, folder = "Templates", "Templates"
		if err := s.store.SaveSettings(ctx, uid, st); err != nil {
			s.fail(w, err, "save template")
			return
		}
	}
	meta.Shared = false // a copy is the person's own until they share it
	in.apply(&meta)
	n, err := s.createNote(ctx, uid, NoteInput{Title: &name, Category: &folder, Content: &content}, source(r))
	if err != nil {
		s.fail(w, err, "save template")
		return
	}
	meta.NoteID, meta.UserID = n.ID, uid
	if err := s.store.PutTemplateMeta(ctx, meta); err != nil {
		s.fail(w, err, "save template")
		return
	}
	writeJSON(w, http.StatusCreated, s.dressTemplate(n, meta, nil, st))
}

// ownTemplate finds one of the person's own templates.
func (s *Server) ownTemplate(ctx context.Context, uid, id int64) (*store.Note, store.TemplateMeta, error) {
	n, meta, sf, err := s.templateFor(ctx, uid, id)
	if err == nil && sf != nil {
		err = errOwnerOnly
	}
	return n, meta, err
}

// handleUpdateTemplate renames a template or changes its settings; its
// text is changed in the editor like any note's.
func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	var in templateIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, meta, err := s.ownTemplate(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "template")
		return
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			writeErr(w, http.StatusBadRequest, "give the template a name")
			return
		}
		if notesmd.SafeName(name) != n.Title {
			if n, err = s.updateNote(ctx, uid, id, NoteInput{Title: &name}, "", source(r)); err != nil {
				s.fail(w, err, "rename template")
				return
			}
		}
	}
	in.apply(&meta)
	if err := s.store.PutTemplateMeta(ctx, meta); err != nil {
		s.fail(w, err, "template")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	writeJSON(w, http.StatusOK, s.dressTemplate(n, meta, nil, st))
}

// handleDeleteTemplate moves a template to the trash; its settings stay, so
// restoring it brings them back.
func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	if _, _, err := s.ownTemplate(ctx, uid, id); err != nil {
		s.fail(w, err, "delete template")
		return
	}
	if err := s.deleteNote(ctx, uid, id, source(r)); err != nil {
		s.fail(w, err, "delete template")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUseTemplate makes a new note from a template in one step: its title
// from the template's title pattern (or ?title), its text with the
// placeholders filled in, in the template's folder — else where the person
// is (here), else their default folder.
func (s *Server) handleUseTemplate(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Title  string  `json:"title"`
		Folder *string `json:"folder"`
		Here   *string `json:"here"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	n, meta, _, err := s.templateFor(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "template")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	now := time.Now()
	title := strings.TrimSpace(in.Title)
	if title == "" {
		if strings.TrimSpace(meta.TitlePattern) == "" {
			meta.TitlePattern = defaultPattern(n.Content)
		}
		title = newTitle(meta, n.Title, now, st)
	}
	folder := in.Folder
	if folder == nil && meta.Folder != "" {
		folder = &meta.Folder
	}
	if folder == nil {
		folder = in.Here
	}
	content := fillPlaceholders(n.Content, notesmd.SafeName(title), n.Title, now, st)
	out, err := s.createNote(ctx, uid, NoteInput{Title: &title, Category: folder, Content: &content}, "template")
	if err != nil {
		s.fail(w, err, "new note from template")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
