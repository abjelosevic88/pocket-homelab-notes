package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// DefaultTitle is the app title until someone changes it.
const DefaultTitle = "Pocket Notes"

// DefaultDailyTemplate is what a new daily note starts with.
const DefaultDailyTemplate = "# {{title}}\n\n"

func defaults() Settings {
	return Settings{
		Title: DefaultTitle, Editor: "rich", FileSuffix: ".md", TitleFromFirstLine: true, SortBy: "modified", SpellCheck: true, DateFormat: "iso",
		DailyFolder: "Daily", DailyFormat: "2006-01-02", DailyTemplate: DefaultDailyTemplate, CaptureFormat: "- {time} {text}",
		TemplatesFolder: "Templates", AttachmentsFolder: "attachments",
		TrashDays: 30, VersionsKeep: 200, StorageKind: "builtin", StorageFolder: "Notes", SyncMinutes: 2, NotifyKind: "apprise",
	}
}

// GetSettings loads the box settings plus the preferences of userID, with
// defaults applied. userID 0 gives the box settings with default preferences.
func (s *Store) GetSettings(ctx context.Context, userID int64) (Settings, error) {
	st := defaults()
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN ('title', 'setup_complete')
		UNION ALL SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	atoi := func(v string, def int) int {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		return def
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		switch k {
		case "title":
			if v != "" {
				st.Title = v
			}
		case "setup_complete":
			st.SetupComplete = v == "1"
		case "editor":
			if v == "rich" || v == "markdown" {
				st.Editor = v
			}
		case "file_suffix":
			if v == ".md" || v == ".txt" {
				st.FileSuffix = v
			}
		case "default_folder":
			st.DefaultFolder = v
		case "title_from_first_line":
			st.TitleFromFirstLine = v != "0"
		case "sort_by":
			if v == "modified" || v == "title" || v == "created" {
				st.SortBy = v
			}
		case "spellcheck":
			st.SpellCheck = v != "0"
		case "date_format":
			if v == "iso" || v == "dmy-dot" || v == "long" {
				st.DateFormat = v
			}
		case "daily_folder":
			st.DailyFolder = v
		case "daily_format":
			if v != "" {
				st.DailyFormat = v
			}
		case "daily_template":
			st.DailyTemplate = v
		case "capture_format":
			if strings.Contains(v, "{text}") {
				st.CaptureFormat = v
			}
		case "templates_folder":
			st.TemplatesFolder = v
		case "attachments_folder":
			st.AttachmentsFolder = v
		case "trash_days":
			st.TrashDays = atoi(v, st.TrashDays)
		case "versions_keep":
			st.VersionsKeep = atoi(v, st.VersionsKeep)
		case "storage_kind":
			if v == "builtin" || v == "folder" || v == "webdav" || v == "nextcloud" || v == "files" {
				st.StorageKind = v
			}
		case "storage_path":
			st.StoragePath = v
		case "storage_url":
			st.StorageURL = v
		case "storage_user":
			st.StorageUser = v
		case "storage_password":
			st.StoragePassword = v
		case "storage_folder":
			st.StorageFolder = v
		case "storage_via_core":
			st.StorageViaCore = v == "1"
		case "sync_minutes":
			st.SyncMinutes = atoi(v, st.SyncMinutes)
		case "notify_kind":
			if v == "apprise" || v == "ntfy" || v == "webhook" {
				st.NotifyKind = v
			}
		case "notify_url":
			st.NotifyURL = v
		case "notify_token":
			st.NotifyToken = v
		}
	}
	if st.SyncMinutes < 1 {
		st.SyncMinutes = 1
	}
	return st, rows.Err()
}

func b2s(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// SaveSettings stores the personal preferences of userID (not the storage,
// which SaveStorage changes, and not the box title).
func (s *Store) SaveSettings(ctx context.Context, userID int64, st Settings) error {
	kv := map[string]string{
		"editor":                st.Editor,
		"file_suffix":           st.FileSuffix,
		"default_folder":        st.DefaultFolder,
		"title_from_first_line": b2s(st.TitleFromFirstLine),
		"sort_by":               st.SortBy,
		"spellcheck":            b2s(st.SpellCheck),
		"date_format":           st.DateFormat,
		"daily_folder":          st.DailyFolder,
		"daily_format":          st.DailyFormat,
		"daily_template":        st.DailyTemplate,
		"capture_format":        st.CaptureFormat,
		"templates_folder":      st.TemplatesFolder,
		"attachments_folder":    st.AttachmentsFolder,
		"trash_days":            strconv.Itoa(st.TrashDays),
		"versions_keep":         strconv.Itoa(st.VersionsKeep),
		"sync_minutes":          strconv.Itoa(st.SyncMinutes),
		"notify_kind":           st.NotifyKind,
		"notify_url":            st.NotifyURL,
	}
	if st.NotifyToken != "" {
		kv["notify_token"] = st.NotifyToken
	}
	return s.putUserSettings(ctx, userID, kv)
}

// SaveStorage stores where a person's notes live. An empty password keeps
// the one stored.
func (s *Store) SaveStorage(ctx context.Context, userID int64, st Settings) error {
	kv := map[string]string{
		"storage_kind":     st.StorageKind,
		"storage_path":     st.StoragePath,
		"storage_url":      st.StorageURL,
		"storage_user":     st.StorageUser,
		"storage_folder":   st.StorageFolder,
		"storage_via_core": b2s(st.StorageViaCore),
	}
	if st.StoragePassword != "" {
		kv["storage_password"] = st.StoragePassword
	}
	return s.putUserSettings(ctx, userID, kv)
}

// UserSetting reads one raw key of a person's settings ("" when missing).
func (s *Store) UserSetting(ctx context.Context, userID int64, key string) string {
	var v string
	_ = s.DB.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id = ? AND key = ?`, userID, key).Scan(&v)
	return v
}

// UserSettingsPrefix reads every key of a person's settings starting with prefix.
func (s *Store) UserSettingsPrefix(ctx context.Context, userID int64, prefix string) map[string]string {
	out := map[string]string{}
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM user_settings WHERE user_id = ? AND key LIKE ? ESCAPE '\'`, userID, likeEscape(prefix)+"%")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

// DeleteUserSettingsPrefix removes keys starting with prefix.
func (s *Store) DeleteUserSettingsPrefix(ctx context.Context, userID int64, prefix string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_settings WHERE user_id = ? AND key LIKE ? ESCAPE '\'`, userID, likeEscape(prefix)+"%")
	return err
}

// SetUserSetting stores one key for a person.
func (s *Store) SetUserSetting(ctx context.Context, userID int64, key, value string) error {
	return s.putUserSettings(ctx, userID, map[string]string{key: value})
}

// SaveBoxSettings stores the settings of the box itself: title and setup state.
func (s *Store) SaveBoxSettings(ctx context.Context, st Settings) error {
	kv := map[string]string{"title": st.Title, "setup_complete": b2s(st.SetupComplete)}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) putUserSettings(ctx context.Context, userID int64, kv map[string]string) error {
	if userID <= 0 {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OwnerID is the account the box was set up with (the first user): keys
// given to the whole box through the environment are theirs.
func (s *Store) OwnerID(ctx context.Context) int64 {
	var id sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MIN(id) FROM users`).Scan(&id)
	return id.Int64
}

// StoragePathOwner returns who keeps their notes in a server folder (0 = nobody).
func (s *Store) StoragePathOwner(ctx context.Context, path string) int64 {
	var id int64
	_ = s.DB.QueryRowContext(ctx, `SELECT a.user_id FROM user_settings a JOIN user_settings b ON b.user_id = a.user_id AND b.key = 'storage_kind' AND b.value = 'folder'
		WHERE a.key = 'storage_path' AND a.value = ?`, path).Scan(&id)
	return id
}

// UsersWithSettings lists every user id (for the sync loop).
func (s *Store) UserIDs(ctx context.Context) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}
