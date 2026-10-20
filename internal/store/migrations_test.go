package store

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

// 0002 drops the typed-in Paperless connection (Pocket Documents replaced
// it) and leaves every other setting alone.
func TestMigrationDropsPaperlessSettings(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := Open(path, log)
	if err != nil {
		t.Fatal(err)
	}
	u := &User{Email: "alice@example.com", Name: "alice"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"svc.paperless.url": "http://paperless:8000", "svc.paperless.key": "pl-key", "svc.paperless.mode": "direct", "svc.forgejo.key": "fj-key", "app.documents.mode": "core"} {
		if err := st.SetUserSetting(ctx, u.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}
	// As a database from before 0002 would be.
	if _, err := st.DB.Exec(`DELETE FROM schema_migrations WHERE name = '0002_drop_paperless.sql'`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if st, err = Open(path, log); err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if left := st.UserSettingsPrefix(ctx, u.ID, "svc.paperless."); len(left) != 0 {
		t.Errorf("paperless settings left: %v", left)
	}
	if st.UserSetting(ctx, u.ID, "svc.forgejo.key") != "fj-key" || st.UserSetting(ctx, u.ID, "app.documents.mode") != "core" {
		t.Error("other settings touched")
	}
}
