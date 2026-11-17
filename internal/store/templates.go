package store

import (
	"context"
	"database/sql"
	"errors"
)

// TemplateMeta is what a template keeps beside its file: the title of the
// notes made from it ({{date}} and friends allowed), where they go, and
// whether everyone on this server may use it.
type TemplateMeta struct {
	NoteID       int64  `json:"-"`
	UserID       int64  `json:"-"`
	TitlePattern string `json:"title_pattern"`
	Folder       string `json:"folder"`
	Shared       bool   `json:"shared"`
}

// TemplateMetas returns one person's template settings by note id.
func (s *Store) TemplateMetas(ctx context.Context, userID int64) (map[int64]TemplateMeta, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT note_id, title_pattern, folder, shared FROM templates WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]TemplateMeta{}
	for rows.Next() {
		m := TemplateMeta{UserID: userID}
		var shared int
		if err := rows.Scan(&m.NoteID, &m.TitlePattern, &m.Folder, &shared); err != nil {
			return nil, err
		}
		m.Shared = shared == 1
		out[m.NoteID] = m
	}
	return out, rows.Err()
}

// TemplateMeta returns a template's settings; the defaults when it has none.
func (s *Store) TemplateMeta(ctx context.Context, userID, noteID int64) (TemplateMeta, error) {
	m := TemplateMeta{NoteID: noteID, UserID: userID}
	var shared int
	err := s.DB.QueryRowContext(ctx, `SELECT title_pattern, folder, shared FROM templates WHERE note_id = ? AND user_id = ?`, noteID, userID).Scan(&m.TitlePattern, &m.Folder, &shared)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	m.Shared = shared == 1
	return m, err
}

// PutTemplateMeta saves a template's settings.
func (s *Store) PutTemplateMeta(ctx context.Context, m TemplateMeta) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO templates (note_id, user_id, title_pattern, folder, shared, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(note_id) DO UPDATE SET user_id = excluded.user_id, title_pattern = excluded.title_pattern, folder = excluded.folder, shared = excluded.shared, updated_at = excluded.updated_at`,
		m.NoteID, m.UserID, m.TitlePattern, m.Folder, boolInt(m.Shared), Now())
	return err
}

// DeleteTemplateMeta forgets a template's settings.
func (s *Store) DeleteTemplateMeta(ctx context.Context, userID, noteID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM templates WHERE note_id = ? AND user_id = ?`, noteID, userID)
	return err
}

// SharedTemplate is someone else's template everyone on this server may use.
type SharedTemplate struct {
	TemplateMeta
	OwnerName  string
	OwnerEmail string
}

// SharedTemplates lists the templates other people share with everyone
// (whose notes still exist; the caller checks they are still templates).
func (s *Store) SharedTemplates(ctx context.Context, notUser int64) ([]SharedTemplate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.note_id, t.user_id, t.title_pattern, t.folder, u.name, u.email
		FROM templates t JOIN notes n ON n.id = t.note_id AND n.user_id = t.user_id JOIN users u ON u.id = t.user_id
		WHERE t.shared = 1 AND t.user_id != ? ORDER BY n.title COLLATE NOCASE`, notUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SharedTemplate
	for rows.Next() {
		var t SharedTemplate
		t.Shared = true
		if err := rows.Scan(&t.NoteID, &t.UserID, &t.TitlePattern, &t.Folder, &t.OwnerName, &t.OwnerEmail); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
