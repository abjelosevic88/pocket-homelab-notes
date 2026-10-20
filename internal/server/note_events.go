package server

import (
	"context"
	"database/sql"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
	"strconv"
)

func (s *Server) noteEvent(ctx context.Context, uid int64, topic string, n *store.Note, source string) core.CommitHook {
	if !s.core.Configured() {
		return nil
	}
	u, err := s.store.GetUser(ctx, uid)
	return func(tx *sql.Tx) error {
		if err != nil {
			return err
		}
		return core.QueueEvent(ctx, tx, topic, strconv.FormatInt(n.ID, 10), map[string]any{"id": n.ID, "title": n.Title, "category": n.Category, "source": source, "user_id": uid, "user_email": u.Email})
	}
}
