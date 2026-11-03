package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// Sharing a note with people on this server. The note stays its owner's file
// in the owner's storage; the people it is shared with read it (and, when
// they may edit, write it) through the owner's library, so every save keeps
// the owner's history, conflicts and sync.

var (
	errReadOnlyShare = errors.New("this note is shared with you to read only")
	errOwnerOnly     = errors.New("only the note's owner can do that")
)

// noteAccess answers in whose library note id lives for person uid: their
// own (nil SharedFrom), or its owner's when it was shared with them. write
// asks for edit rights. A note neither theirs nor shared with them is not found.
func (s *Server) noteAccess(ctx context.Context, uid, id int64, write bool) (int64, *store.SharedFrom, error) {
	owner, err := s.store.NoteOwner(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	if owner == uid {
		return uid, nil, nil
	}
	sf, err := s.store.Membership(ctx, id, uid)
	if err != nil || sf.OwnerID != owner {
		return 0, nil, store.ErrNotFound
	}
	if write && !sf.CanEdit {
		return 0, nil, errReadOnlyShare
	}
	return owner, sf, nil
}

// seenBy dresses a note for the person looking at it: whose it is when it
// was shared with them (read only unless they may edit; the owner's stars
// and pins are not theirs), how many people it is shared with when it is theirs.
func (s *Server) seenBy(ctx context.Context, n *store.Note, sf *store.SharedFrom) {
	if n == nil {
		return
	}
	if sf != nil {
		n.SharedFrom = sf
		n.Readonly = n.Readonly || !sf.CanEdit
		n.Favorite, n.Pinned = false, false
		return
	}
	if m, err := s.store.ListMembers(ctx, n.ID); err == nil {
		n.Shared = len(m)
	}
}

// editSource names a member's saves in the note's history: "by Ana".
func editSource(r *http.Request, sf *store.SharedFrom) string {
	if sf == nil {
		return source(r)
	}
	if u := userFrom(r.Context()); u != nil {
		return "by " + firstNonEmpty(u.Name, u.Email)
	}
	return "shared"
}

// handlePeople lists the people on this server a note can be shared with.
func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.fail(w, err, "people")
		return
	}
	out := []map[string]any{}
	for _, u := range users {
		if u.ID == me(r) {
			continue
		}
		out = append(out, map[string]any{"id": u.ID, "name": u.Name, "email": u.Email})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleShared is the Shared view: notes others shared with me, and mine I shared.
func (s *Server) handleShared(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	with, err := s.store.SharedWithMe(ctx, uid)
	if err != nil {
		s.fail(w, err, "shared")
		return
	}
	for _, n := range with {
		n.Readonly = n.Readonly || !n.SharedFrom.CanEdit
		n.Favorite, n.Pinned = false, false
	}
	by, err := s.store.SharedByMe(ctx, uid)
	if err != nil {
		s.fail(w, err, "shared")
		return
	}
	for _, n := range by {
		n.Shared = len(n.Members)
	}
	writeJSON(w, http.StatusOK, map[string]any{"with_me": with, "by_me": by})
}

// handleNoteMembers lists who a note is shared with (its owner first).
func (s *Server) handleNoteMembers(w http.ResponseWriter, r *http.Request) {
	ctx, id := r.Context(), idParam(r, "id")
	owner, _, err := s.noteAccess(ctx, me(r), id, false)
	if err != nil {
		s.fail(w, err, "members")
		return
	}
	members, err := s.store.ListMembers(ctx, id)
	if err != nil {
		s.fail(w, err, "members")
		return
	}
	o, err := s.store.GetUser(ctx, owner)
	if err != nil {
		s.fail(w, err, "members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"owner":   map[string]any{"user_id": o.ID, "name": o.Name, "email": o.Email},
		"members": members,
		"mine":    owner == me(r),
	})
}

// handleSetMember shares a note with a person (by id or e-mail), or changes
// whether they may edit it. Only the owner shares; the person hears of it.
func (s *Server) handleSetMember(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id := r.Context(), me(r), idParam(r, "id")
	var in struct {
		UserID  int64  `json:"user_id"`
		Email   string `json:"email"`
		CanEdit bool   `json:"can_edit"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil {
		if _, sf, aerr := s.noteAccess(ctx, uid, id, false); aerr == nil && sf != nil {
			err = errOwnerOnly
		}
		s.fail(w, err, "share")
		return
	}
	var target *store.User
	if in.UserID > 0 {
		target, err = s.store.GetUser(ctx, in.UserID)
	} else if e := strings.TrimSpace(in.Email); e != "" {
		target, err = s.store.GetUserByEmail(ctx, e)
	} else {
		writeErr(w, http.StatusBadRequest, "pick a person to share with")
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "nobody with that address has signed in to Pocket Notes yet")
			return
		}
		s.fail(w, err, "share")
		return
	}
	if target.ID == uid {
		writeErr(w, http.StatusBadRequest, "the note is yours already")
		return
	}
	added, err := s.store.SetMember(ctx, id, uid, target.ID, in.CanEdit)
	if err != nil {
		s.fail(w, err, "share")
		return
	}
	if added {
		who := "Someone"
		if u := userFrom(ctx); u != nil {
			who = firstNonEmpty(u.Name, u.Email)
		}
		what := "to read"
		if in.CanEdit {
			what = "to read and edit"
		}
		link := ""
		if s.cfg.BaseURL != "" {
			link = fmt.Sprintf("%s/note/%d?view=shared", s.cfg.BaseURL, id)
		}
		s.notifyUserAt(target.ID, who+" shared a note with you", fmt.Sprintf("“%s”, %s. It is under Shared in Pocket Notes.", n.Title, what), link)
	}
	s.handleNoteMembers(w, r)
}

// handleRemoveMember stops sharing a note with a person: the owner can
// remove anyone, a member only themself ("Remove from my Shared").
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx, uid, id, who := r.Context(), me(r), idParam(r, "id"), idParam(r, "uid")
	owner, _, err := s.noteAccess(ctx, uid, id, false)
	if err != nil {
		s.fail(w, err, "unshare")
		return
	}
	if owner != uid && who != uid {
		s.fail(w, errOwnerOnly, "unshare")
		return
	}
	if err := s.store.RemoveMember(ctx, id, who); err != nil {
		s.fail(w, err, "unshare")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
