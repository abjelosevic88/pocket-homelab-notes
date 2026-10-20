package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// LocalIDBase is where ids of notes without a storage file id start, far
// above Nextcloud's own file ids so the two never meet.
const LocalIDBase = 1_000_000_000

const noteCols = `id, user_id, path, category, title, hash, remote_etag, file_id, size, modified, favorite, pinned, readonly, excerpt, words, tasks_open, tasks_done, opened_at, created_at, updated_at`

func scanNote(row interface{ Scan(...any) error }, withContent bool) (*Note, error) {
	var n Note
	var fav, pin, ro int
	dest := []any{&n.ID, &n.UserID, &n.Path, &n.Category, &n.Title, &n.Hash, &n.RemoteETag, &n.FileID, &n.Size, &n.Modified, &fav, &pin, &ro, &n.Excerpt, &n.Words, &n.TasksOpen, &n.TasksDone, &n.OpenedAt, &n.CreatedAt, &n.UpdatedAt}
	if withContent {
		dest = append(dest, &n.Content)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, notFound(err)
	}
	n.Favorite, n.Pinned, n.Readonly = fav == 1, pin == 1, ro == 1
	n.Tags = []string{}
	return &n, nil
}

// attachTags fills Tags for a list of notes in one query.
func (s *Store) attachTags(ctx context.Context, notes []*Note) {
	if len(notes) == 0 {
		return
	}
	byID := map[int64]*Note{}
	ids := make([]string, 0, len(notes))
	for _, n := range notes {
		byID[n.ID] = n
		ids = append(ids, strconv.FormatInt(n.ID, 10))
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT note_id, tag FROM note_tags WHERE note_id IN (`+strings.Join(ids, ",")+`) ORDER BY tag`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var tag string
		if rows.Scan(&id, &tag) == nil {
			if n := byID[id]; n != nil {
				n.Tags = append(n.Tags, tag)
			}
		}
	}
}

// ListNotes returns one person's notes, newest first unless asked otherwise.
func (s *Store) ListNotes(ctx context.Context, userID int64, f NoteFilter) ([]Note, error) {
	q := `SELECT ` + noteCols
	if f.Content {
		q += `, content`
	}
	q += ` FROM notes WHERE user_id = ?`
	args := []any{userID}
	if f.Category != nil {
		if f.Recursive && *f.Category != "" {
			q += ` AND (category = ? OR category LIKE ? ESCAPE '\')`
			args = append(args, *f.Category, likeEscape(*f.Category)+"/%")
		} else if !f.Recursive {
			q += ` AND category = ?`
			args = append(args, *f.Category)
		}
	}
	if f.Tag != "" {
		q += ` AND id IN (SELECT note_id FROM note_tags WHERE user_id = ? AND (tag = ? OR tag LIKE ? ESCAPE '\'))`
		args = append(args, userID, strings.ToLower(f.Tag), likeEscape(strings.ToLower(f.Tag))+"/%")
	}
	if f.Favorite {
		q += ` AND favorite = 1`
	}
	if f.Pinned {
		q += ` AND pinned = 1`
	}
	if f.HasTasks {
		q += ` AND tasks_open > 0`
	}
	if f.Since > 0 {
		q += ` AND modified > ?`
		args = append(args, f.Since)
	}
	switch f.Sort {
	case "title":
		q += ` ORDER BY pinned DESC, title COLLATE NOCASE`
	case "created":
		q += ` ORDER BY pinned DESC, created_at DESC`
	case "opened":
		q += ` ORDER BY opened_at DESC, modified DESC`
	case "plain":
		q += ` ORDER BY modified DESC`
	default:
		q += ` ORDER BY pinned DESC, modified DESC`
	}
	if f.Limit > 0 {
		q += ` LIMIT ` + strconv.Itoa(f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ptrs []*Note
	for rows.Next() {
		n, err := scanNote(rows, f.Content)
		if err != nil {
			return nil, err
		}
		ptrs = append(ptrs, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	s.attachTags(ctx, ptrs)
	out := make([]Note, 0, len(ptrs))
	for _, n := range ptrs {
		out = append(out, *n)
	}
	return out, nil
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// GetNote returns one of the person's notes with its content.
func (s *Store) GetNote(ctx context.Context, userID, id int64) (*Note, error) {
	n, err := scanNote(s.DB.QueryRowContext(ctx, `SELECT `+noteCols+`, content FROM notes WHERE id = ? AND user_id = ?`, id, userID), true)
	if err != nil {
		return nil, err
	}
	s.attachTags(ctx, []*Note{n})
	return n, nil
}

// GetNoteByPath finds a note by its file.
func (s *Store) GetNoteByPath(ctx context.Context, userID int64, path string) (*Note, error) {
	n, err := scanNote(s.DB.QueryRowContext(ctx, `SELECT `+noteCols+`, content FROM notes WHERE user_id = ? AND path = ?`, userID, path), true)
	if err != nil {
		return nil, err
	}
	s.attachTags(ctx, []*Note{n})
	return n, nil
}

// FindNoteByTitle finds a note by its title (case-insensitive), the way a
// [[wiki link]] names it. With several, the one in folder wins, then the newest.
func (s *Store) FindNoteByTitle(ctx context.Context, userID int64, title, folder string) (*Note, error) {
	n, err := scanNote(s.DB.QueryRowContext(ctx, `SELECT `+noteCols+`, content FROM notes WHERE user_id = ? AND lower(title) = lower(?)
		ORDER BY (category = ?) DESC, modified DESC LIMIT 1`, userID, strings.TrimSpace(title), folder), true)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// PathTaken reports whether a person has a note at path.
func (s *Store) PathTaken(ctx context.Context, userID int64, path string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM notes WHERE user_id = ? AND lower(path) = lower(?)`, userID, path).Scan(&n)
	return n > 0
}

// InsertNote adds a note to the index. preferID (a storage file id) is used
// as the note's id when no other note has it.
func (s *Store) InsertNote(ctx context.Context, n *Note, preferID int64) error {
	if preferID > 0 && preferID < LocalIDBase {
		var taken int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM notes WHERE id = ?`, preferID).Scan(&taken)
		if taken > 0 {
			preferID = 0
		}
	} else {
		preferID = 0
	}
	if preferID == 0 {
		var max sql.NullInt64
		_ = s.DB.QueryRowContext(ctx, `SELECT MAX(id) FROM notes WHERE id >= ?`, LocalIDBase).Scan(&max)
		var trashMax sql.NullInt64
		_ = s.DB.QueryRowContext(ctx, `SELECT MAX(note_id) FROM trash WHERE note_id >= ?`, LocalIDBase).Scan(&trashMax)
		preferID = LocalIDBase
		if max.Valid && max.Int64 >= preferID {
			preferID = max.Int64 + 1
		}
		if trashMax.Valid && trashMax.Int64 >= preferID {
			preferID = trashMax.Int64 + 1
		}
	}
	now := Now()
	if n.CreatedAt == "" {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `INSERT INTO notes (id, user_id, path, category, title, content, hash, remote_etag, file_id, size, modified, favorite, pinned, readonly, excerpt, words, tasks_open, tasks_done, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		preferID, n.UserID, n.Path, n.Category, n.Title, n.Content, n.Hash, n.RemoteETag, n.FileID, n.Size, n.Modified,
		boolInt(n.Favorite), boolInt(n.Pinned), boolInt(n.Readonly), n.Excerpt, n.Words, n.TasksOpen, n.TasksDone, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return conflict(err)
	}
	n.ID = preferID
	return nil
}

// UpdateNote writes every indexed field of a note.
func (s *Store) UpdateNote(ctx context.Context, n *Note) error {
	n.UpdatedAt = Now()
	res, err := s.DB.ExecContext(ctx, `UPDATE notes SET path = ?, category = ?, title = ?, content = ?, hash = ?, remote_etag = ?, file_id = ?, size = ?, modified = ?,
		favorite = ?, pinned = ?, readonly = ?, excerpt = ?, words = ?, tasks_open = ?, tasks_done = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		n.Path, n.Category, n.Title, n.Content, n.Hash, n.RemoteETag, n.FileID, n.Size, n.Modified,
		boolInt(n.Favorite), boolInt(n.Pinned), boolInt(n.Readonly), n.Excerpt, n.Words, n.TasksOpen, n.TasksDone, n.UpdatedAt, n.ID, n.UserID)
	if err != nil {
		return conflict(err)
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRemote records the storage's ETag after a read or write that changed nothing else.
func (s *Store) SetRemote(ctx context.Context, userID, id int64, etag, fileID string, modified int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE notes SET remote_etag = ?, file_id = CASE WHEN ? <> '' THEN ? ELSE file_id END, modified = CASE WHEN ? > 0 THEN ? ELSE modified END WHERE id = ? AND user_id = ?`,
		etag, fileID, fileID, modified, modified, id, userID)
	return err
}

// SetFlag sets favorite or pinned.
func (s *Store) SetFlag(ctx context.Context, userID, id int64, flag string, on bool) error {
	if flag != "favorite" && flag != "pinned" {
		return ErrNotFound
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE notes SET `+flag+` = ? WHERE id = ? AND user_id = ?`, boolInt(on), id, userID)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchOpened remembers when a note was last opened (for "Recent").
func (s *Store) TouchOpened(ctx context.Context, userID, id int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE notes SET opened_at = ? WHERE id = ? AND user_id = ?`, Now(), id, userID)
}

// DeleteNote removes a note from the index.
func (s *Store) DeleteNote(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM notes WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAllNotes forgets a person's whole index (a new storage, started fresh).
func (s *Store) DeleteAllNotes(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM notes WHERE user_id = ?`, userID)
	return err
}

// SetNoteMeta replaces a note's tags and links.
func (s *Store) SetNoteMeta(ctx context.Context, userID, id int64, tags, links []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_links WHERE note_id = ?`, id); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_tags (note_id, user_id, tag) VALUES (?, ?, ?)`, id, userID, t); err != nil {
			return err
		}
	}
	for _, l := range links {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_links (note_id, user_id, target) VALUES (?, ?, ?)`, id, userID, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SyncRow is what the sync needs to know about an indexed note.
type SyncRow struct {
	ID         int64
	Path       string
	Hash       string
	RemoteETag string
	FileID     string
	Favorite   bool
}

// SyncRows returns every indexed note of a person by path.
func (s *Store) SyncRows(ctx context.Context, userID int64) (map[string]*SyncRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, path, hash, remote_etag, file_id, favorite FROM notes WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*SyncRow{}
	for rows.Next() {
		var r SyncRow
		var fav int
		if err := rows.Scan(&r.ID, &r.Path, &r.Hash, &r.RemoteETag, &r.FileID, &fav); err != nil {
			return nil, err
		}
		r.Favorite = fav == 1
		out[r.Path] = &r
	}
	return out, rows.Err()
}

// Folders lists the person's folders with their note counts. Folders without
// notes (made empty) come from known, the storage's own listing.
func (s *Store) Folders(ctx context.Context, userID int64) ([]Folder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT category, COUNT(1) FROM notes WHERE user_id = ? GROUP BY category ORDER BY category COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.Path, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Tags lists the person's tags with counts.
func (s *Store) Tags(ctx context.Context, userID int64) ([]TagCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT tag, COUNT(1) FROM note_tags WHERE user_id = ? GROUP BY tag ORDER BY tag`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Backlinks lists the notes that link to a note by its title or path.
func (s *Store) Backlinks(ctx context.Context, userID int64, n *Note) ([]Note, error) {
	targets := []any{userID, strings.ToLower(n.Title), "path:" + strings.ToLower(n.Path)}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+noteCols+` FROM notes WHERE user_id = ? AND id <> ? AND id IN
		(SELECT note_id FROM note_links WHERE user_id = ? AND (target = ? OR target = ? OR target = ? OR target LIKE ? ESCAPE '\'))
		ORDER BY modified DESC LIMIT 100`, userID, n.ID, targets[0], targets[1], targets[2], "path:"+strings.ToLower(lastPart(n.Path)), "path:%/"+likeEscape(strings.ToLower(lastPart(n.Path))))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ptrs []*Note
	for rows.Next() {
		x, err := scanNote(rows, false)
		if err != nil {
			return nil, err
		}
		ptrs = append(ptrs, x)
	}
	rows.Close()
	s.attachTags(ctx, ptrs)
	out := make([]Note, 0, len(ptrs))
	for _, p := range ptrs {
		out = append(out, *p)
	}
	return out, nil
}

func lastPart(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// MentionCandidates lists notes whose text names title without linking it.
func (s *Store) Mentions(ctx context.Context, userID int64, n *Note, limit int) ([]Note, error) {
	q := ftsPhrase(n.Title)
	if q == "" {
		return []Note{}, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+prefixed("n.", noteCols)+` FROM notes_fts f JOIN notes n ON n.id = f.rowid
		WHERE notes_fts MATCH ? AND n.user_id = ? AND n.id <> ? AND n.id NOT IN (SELECT note_id FROM note_links WHERE user_id = ? AND target = ?)
		ORDER BY n.modified DESC LIMIT ?`, "content : "+q, userID, n.ID, userID, strings.ToLower(n.Title), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		x, err := scanNote(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

func prefixed(p, cols string) string {
	parts := strings.Split(cols, ", ")
	for i := range parts {
		parts[i] = p + parts[i]
	}
	return strings.Join(parts, ", ")
}

// ftsPhrase quotes text as one FTS5 phrase.
func ftsPhrase(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if len(words) == 0 {
		return ""
	}
	return `"` + strings.Join(words, " ") + `"`
}

// FTSQuery turns what a person typed into an FTS5 query: every word must be
// there, the last one as a prefix; "quoted words" stay a phrase.
func FTSQuery(text string) string {
	var parts []string
	for len(text) > 0 {
		text = strings.TrimLeft(text, " \t")
		if text == "" {
			break
		}
		if text[0] == '"' {
			end := strings.IndexByte(text[1:], '"')
			phrase := text[1:]
			if end >= 0 {
				phrase, text = text[1:end+1], text[end+2:]
			} else {
				text = ""
			}
			if p := ftsPhrase(phrase); p != "" {
				parts = append(parts, p)
			}
			continue
		}
		end := strings.IndexAny(text, " \t")
		word := text
		if end >= 0 {
			word, text = text[:end], text[end:]
		} else {
			text = ""
		}
		for _, w := range strings.FieldsFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			parts = append(parts, `"`+w+`"*`)
		}
	}
	return strings.Join(parts, " ")
}

// Search finds a person's notes by their words, best matches first, with a
// snippet around the words. extra narrows like ListNotes.
func (s *Store) Search(ctx context.Context, userID int64, text string, f NoteFilter, limit int) ([]SearchHit, error) {
	q := FTSQuery(text)
	if q == "" {
		return []SearchHit{}, nil
	}
	sqlq := `SELECT ` + prefixed("n.", noteCols) + `, snippet(notes_fts, 1, '[[[', ']]]', '…', 14), bm25(notes_fts, 8.0, 1.0, 2.0)
		FROM notes_fts JOIN notes n ON n.id = notes_fts.rowid WHERE notes_fts MATCH ? AND n.user_id = ?`
	args := []any{q, userID}
	if f.Category != nil {
		sqlq += ` AND (n.category = ? OR n.category LIKE ? ESCAPE '\')`
		args = append(args, *f.Category, likeEscape(*f.Category)+"/%")
	}
	if f.Tag != "" {
		sqlq += ` AND n.id IN (SELECT note_id FROM note_tags WHERE user_id = ? AND (tag = ? OR tag LIKE ? ESCAPE '\'))`
		args = append(args, userID, strings.ToLower(f.Tag), likeEscape(strings.ToLower(f.Tag))+"/%")
	}
	if f.Favorite {
		sqlq += ` AND n.favorite = 1`
	}
	if f.Pinned {
		sqlq += ` AND n.pinned = 1`
	}
	if f.HasTasks {
		sqlq += ` AND n.tasks_open > 0`
	}
	sqlq += ` ORDER BY bm25(notes_fts, 8.0, 1.0, 2.0) LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []*SearchHit
	for rows.Next() {
		var h SearchHit
		var fav, pin, ro int
		n := &h.Note
		if err := rows.Scan(&n.ID, &n.UserID, &n.Path, &n.Category, &n.Title, &n.Hash, &n.RemoteETag, &n.FileID, &n.Size, &n.Modified, &fav, &pin, &ro, &n.Excerpt, &n.Words, &n.TasksOpen, &n.TasksDone, &n.OpenedAt, &n.CreatedAt, &n.UpdatedAt, &h.Snippet, &h.Rank); err != nil {
			return nil, err
		}
		n.Favorite, n.Pinned, n.Readonly = fav == 1, pin == 1, ro == 1
		n.Tags = []string{}
		hits = append(hits, &h)
	}
	rows.Close()
	ptrs := make([]*Note, len(hits))
	for i, h := range hits {
		ptrs[i] = &h.Note
	}
	s.attachTags(ctx, ptrs)
	out := make([]SearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, *h)
	}
	return out, nil
}

// TitleMatches finds notes whose title contains text, for the quick switcher
// and [[ completion.
func (s *Store) TitleMatches(ctx context.Context, userID int64, text string, limit int) ([]Note, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+noteCols+` FROM notes WHERE user_id = ? AND title LIKE ? ESCAPE '\'
		ORDER BY (lower(title) = lower(?)) DESC, (lower(title) LIKE lower(?) ESCAPE '\') DESC, modified DESC LIMIT ?`,
		userID, "%"+likeEscape(text)+"%", text, likeEscape(text)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		n, err := scanNote(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// ---- versions ----

// AddVersion records a note's state. A save from the same source within
// window of the last version replaces it instead of adding one.
func (s *Store) AddVersion(ctx context.Context, v *Version, window time.Duration) error {
	var lastID int64
	var lastSource, lastAt, lastHash string
	err := s.DB.QueryRowContext(ctx, `SELECT id, source, created_at, hash FROM note_versions WHERE note_id = ? ORDER BY id DESC LIMIT 1`, v.NoteID).Scan(&lastID, &lastSource, &lastAt, &lastHash)
	if err == nil {
		if lastHash == v.Hash {
			return nil
		}
		if lastSource == v.Source && window > 0 && time.Since(ParseTime(lastAt)) < window {
			_, err := s.DB.ExecContext(ctx, `UPDATE note_versions SET title = ?, path = ?, content = ?, hash = ? WHERE id = ?`, v.Title, v.Path, v.Content, v.Hash, lastID)
			v.ID = lastID
			return err
		}
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO note_versions (note_id, user_id, title, path, content, hash, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.NoteID, v.UserID, v.Title, v.Path, v.Content, v.Hash, v.Source, Now())
	if err != nil {
		return err
	}
	v.ID, _ = res.LastInsertId()
	return nil
}

// ListVersions returns a note's versions, newest first, without content.
func (s *Store) ListVersions(ctx context.Context, userID, noteID int64) ([]Version, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, note_id, title, path, hash, source, length(content), created_at FROM note_versions WHERE note_id = ? AND user_id = ? ORDER BY id DESC`, noteID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.NoteID, &v.Title, &v.Path, &v.Hash, &v.Source, &v.Size, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetVersion returns one version with its content.
func (s *Store) GetVersion(ctx context.Context, userID, noteID, id int64) (*Version, error) {
	var v Version
	err := s.DB.QueryRowContext(ctx, `SELECT id, note_id, title, path, content, hash, source, created_at FROM note_versions WHERE id = ? AND note_id = ? AND user_id = ?`, id, noteID, userID).
		Scan(&v.ID, &v.NoteID, &v.Title, &v.Path, &v.Content, &v.Hash, &v.Source, &v.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	v.Size = len(v.Content)
	return &v, nil
}

// PruneVersions keeps the newest keep versions of every note of a person.
func (s *Store) PruneVersions(ctx context.Context, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM note_versions WHERE id IN (
		SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY note_id ORDER BY id DESC) AS rn FROM note_versions) WHERE rn > ?)`, keep)
	return err
}

// ---- trash ----

// AddTrash keeps a deleted note.
func (s *Store) AddTrash(ctx context.Context, t *TrashItem) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO trash (user_id, note_id, path, title, category, content, favorite, pinned, reason, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.UserID, t.NoteID, t.Path, t.Title, t.Category, t.Content, boolInt(t.Favorite), boolInt(t.Pinned), t.Reason, Now())
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	return nil
}

// ListTrash returns a person's deleted notes, newest first.
func (s *Store) ListTrash(ctx context.Context, userID int64) ([]TrashItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, note_id, path, title, category, substr(content, 1, 400), favorite, pinned, reason, deleted_at FROM trash WHERE user_id = ? ORDER BY deleted_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrashItem{}
	for rows.Next() {
		var t TrashItem
		var fav, pin int
		if err := rows.Scan(&t.ID, &t.NoteID, &t.Path, &t.Title, &t.Category, &t.Excerpt, &fav, &pin, &t.Reason, &t.DeletedAt); err != nil {
			return nil, err
		}
		t.Favorite, t.Pinned = fav == 1, pin == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTrash returns one deleted note with its content.
func (s *Store) GetTrash(ctx context.Context, userID, id int64) (*TrashItem, error) {
	var t TrashItem
	var fav, pin int
	err := s.DB.QueryRowContext(ctx, `SELECT id, note_id, path, title, category, content, favorite, pinned, reason, deleted_at FROM trash WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&t.ID, &t.NoteID, &t.Path, &t.Title, &t.Category, &t.Content, &fav, &pin, &t.Reason, &t.DeletedAt)
	if err != nil {
		return nil, notFound(err)
	}
	t.Favorite, t.Pinned = fav == 1, pin == 1
	return &t, nil
}

// DeleteTrash removes one deleted note for good (all = every one of the person's).
func (s *Store) DeleteTrash(ctx context.Context, userID, id int64, all bool) error {
	if all {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM trash WHERE user_id = ?`, userID)
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM trash WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeTrash removes deleted notes older than days (per person's own setting).
func (s *Store) PurgeTrash(ctx context.Context, userID int64, days int) error {
	if days <= 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM trash WHERE user_id = ? AND deleted_at < ?`, userID, time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339Nano))
	return err
}

// ---- shares ----

// CreateShare makes a public link to a note.
func (s *Store) CreateShare(ctx context.Context, sh *Share) error {
	sh.Token = RandomToken(16)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO shares (user_id, note_id, token, expires_at) VALUES (?, ?, ?, ?)`, sh.UserID, sh.NoteID, sh.Token, sh.ExpiresAt)
	if err != nil {
		return err
	}
	sh.ID, _ = res.LastInsertId()
	sh.CreatedAt = Now()
	return nil
}

// ListShares lists a person's links (noteID 0 = all of them).
func (s *Store) ListShares(ctx context.Context, userID, noteID int64) ([]Share, error) {
	q := `SELECT s.id, s.note_id, n.title, s.token, s.expires_at, s.views, s.created_at FROM shares s JOIN notes n ON n.id = s.note_id WHERE s.user_id = ?`
	args := []any{userID}
	if noteID > 0 {
		q += ` AND s.note_id = ?`
		args = append(args, noteID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY s.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Share{}
	for rows.Next() {
		var sh Share
		if err := rows.Scan(&sh.ID, &sh.NoteID, &sh.NoteTitle, &sh.Token, &sh.ExpiresAt, &sh.Views, &sh.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// DeleteShare revokes a link.
func (s *Store) DeleteShare(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM shares WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}

// ShareByToken resolves a public link that has not expired, counting the view.
func (s *Store) ShareByToken(ctx context.Context, token string, count bool) (*Share, error) {
	var sh Share
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, note_id, token, expires_at, views, created_at FROM shares WHERE token = ?`, token).
		Scan(&sh.ID, &sh.UserID, &sh.NoteID, &sh.Token, &sh.ExpiresAt, &sh.Views, &sh.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	if sh.ExpiresAt != "" && ParseTime(sh.ExpiresAt).Before(time.Now()) {
		return nil, ErrNotFound
	}
	if count {
		_, _ = s.DB.ExecContext(ctx, `UPDATE shares SET views = views + 1 WHERE id = ?`, sh.ID)
	}
	return &sh, nil
}

// ---- dashboard ----

// NoteDashboard counts a person's notes for widgets.
func (s *Store) NoteDashboard(ctx context.Context, userID int64) Dashboard {
	var d Dashboard
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COUNT(CASE WHEN modified >= ? THEN 1 END), COUNT(CASE WHEN modified >= ? THEN 1 END),
		COALESCE(SUM(tasks_open), 0), COUNT(CASE WHEN favorite = 1 THEN 1 END), COUNT(DISTINCT category) FROM notes WHERE user_id = ?`,
		day, day-6*86400, userID).Scan(&d.Notes, &d.EditedToday, &d.EditedWeek, &d.OpenTasks, &d.Favorites, &d.Folders)
	return d
}
