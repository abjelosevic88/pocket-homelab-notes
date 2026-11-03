package store

import (
	"context"
	"strings"
)

// Member is a person a note is shared with.
type Member struct {
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	CanEdit   bool   `json:"can_edit"`
	CreatedAt string `json:"created_at"`
}

// SharedFrom tells the person a note was shared with whose note it is and
// what they may do with it.
type SharedFrom struct {
	OwnerID int64  `json:"owner_id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	CanEdit bool   `json:"can_edit"`
}

// NoteOwner answers whose note id is (ErrNotFound when there is none).
func (s *Store) NoteOwner(ctx context.Context, id int64) (int64, error) {
	var uid int64
	err := s.DB.QueryRowContext(ctx, `SELECT user_id FROM notes WHERE id = ?`, id).Scan(&uid)
	return uid, notFound(err)
}

// Membership answers how note id is shared with userID (ErrNotFound when it is not).
func (s *Store) Membership(ctx context.Context, id, userID int64) (*SharedFrom, error) {
	var sf SharedFrom
	var edit int
	err := s.DB.QueryRowContext(ctx, `SELECT m.owner_id, u.name, u.email, m.can_edit FROM note_members m JOIN users u ON u.id = m.owner_id
		WHERE m.note_id = ? AND m.user_id = ?`, id, userID).Scan(&sf.OwnerID, &sf.Name, &sf.Email, &edit)
	if err != nil {
		return nil, notFound(err)
	}
	sf.CanEdit = edit == 1
	return &sf, nil
}

// TouchMemberOpened remembers when a member last opened a shared note.
func (s *Store) TouchMemberOpened(ctx context.Context, id, userID int64) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE note_members SET opened_at = ? WHERE note_id = ? AND user_id = ?`, Now(), id, userID)
}

// SetMember shares a note with a person, or changes what they may do. It
// answers whether they are new to it.
func (s *Store) SetMember(ctx context.Context, id, ownerID, userID int64, canEdit bool) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE note_members SET can_edit = ? WHERE note_id = ? AND user_id = ? AND owner_id = ?`, boolInt(canEdit), id, userID, ownerID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO note_members (note_id, owner_id, user_id, can_edit) VALUES (?, ?, ?, ?)`, id, ownerID, userID, boolInt(canEdit))
	return err == nil, conflict(err)
}

// RemoveMember stops sharing a note with a person.
func (s *Store) RemoveMember(ctx context.Context, id, userID int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM note_members WHERE note_id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMembers lists the people a note is shared with.
func (s *Store) ListMembers(ctx context.Context, id int64) ([]Member, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT m.user_id, u.name, u.email, m.can_edit, m.created_at FROM note_members m JOIN users u ON u.id = m.user_id
		WHERE m.note_id = ? ORDER BY lower(CASE WHEN u.name <> '' THEN u.name ELSE u.email END)`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		var edit int
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &edit, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.CanEdit = edit == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemberCounts answers how many people each of a person's notes is shared with.
func (s *Store) MemberCounts(ctx context.Context, ownerID int64) map[int64]int {
	out := map[int64]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT note_id, count(*) FROM note_members WHERE owner_id = ? GROUP BY note_id`, ownerID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

// SharedWithMe lists the notes other people shared with userID, newest edit first.
func (s *Store) SharedWithMe(ctx context.Context, userID int64) ([]*Note, error) {
	cols := "n." + strings.ReplaceAll(noteCols, ", ", ", n.")
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+`, m.owner_id, u.name, u.email, m.can_edit FROM note_members m
		JOIN notes n ON n.id = m.note_id JOIN users u ON u.id = m.owner_id
		WHERE m.user_id = ? ORDER BY n.modified DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Note{}
	for rows.Next() {
		var sf SharedFrom
		var edit int
		n, err := scanNote(scanTail{rows, []any{&sf.OwnerID, &sf.Name, &sf.Email, &edit}}, false)
		if err != nil {
			return nil, err
		}
		sf.CanEdit = edit == 1
		n.SharedFrom = &sf
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.attachTags(ctx, out)
	return out, nil
}

// SharedByMe lists ownerID's notes that are shared with someone, with whom.
func (s *Store) SharedByMe(ctx context.Context, ownerID int64) ([]*Note, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+noteCols+` FROM notes WHERE user_id = ? AND id IN (SELECT note_id FROM note_members WHERE owner_id = ?)
		ORDER BY modified DESC`, ownerID, ownerID)
	if err != nil {
		return nil, err
	}
	out := []*Note{}
	for rows.Next() {
		n, err := scanNote(rows, false)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.attachTags(ctx, out)
	for _, n := range out {
		n.Members, _ = s.ListMembers(ctx, n.ID)
	}
	return out, nil
}

// CountSharedWithMe answers how many notes other people shared with userID.
func (s *Store) CountSharedWithMe(ctx context.Context, userID int64) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM note_members WHERE user_id = ?`, userID).Scan(&n)
	return n
}

// scanTail scans a note row followed by extra columns.
type scanTail struct {
	row  interface{ Scan(...any) error }
	tail []any
}

func (t scanTail) Scan(dest ...any) error { return t.row.Scan(append(dest, t.tail...)...) }
