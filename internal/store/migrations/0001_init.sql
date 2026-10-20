CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'admin',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup_complete). Everything a person chooses
-- (where their notes live included) is in user_settings under their id.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

-- The index of one person's notes. The note itself is the file at `path`
-- (relative to that person's notes folder, wherever it lives); `content` is
-- the copy the index read last, for search and for answering without a trip
-- to the storage.
--   id:          Nextcloud's file id when the notes live in Nextcloud (so ids
--                match Nextcloud Notes), otherwise our own.
--   hash:        sha1 of content; the ETag clients see.
--   remote_etag: the storage's ETag when the file was last read or written.
CREATE TABLE notes (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  path        TEXT NOT NULL,
  category    TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL,
  content     TEXT NOT NULL DEFAULT '',
  hash        TEXT NOT NULL DEFAULT '',
  remote_etag TEXT NOT NULL DEFAULT '',
  file_id     TEXT NOT NULL DEFAULT '',
  size        INTEGER NOT NULL DEFAULT 0,
  modified    INTEGER NOT NULL DEFAULT 0,
  favorite    INTEGER NOT NULL DEFAULT 0,
  pinned      INTEGER NOT NULL DEFAULT 0,
  readonly    INTEGER NOT NULL DEFAULT 0,
  excerpt     TEXT NOT NULL DEFAULT '',
  words       INTEGER NOT NULL DEFAULT 0,
  tasks_open  INTEGER NOT NULL DEFAULT 0,
  tasks_done  INTEGER NOT NULL DEFAULT 0,
  opened_at   TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, path)
);
CREATE INDEX notes_user_modified ON notes(user_id, modified DESC);
CREATE INDEX notes_user_category ON notes(user_id, category);

CREATE VIRTUAL TABLE notes_fts USING fts5(
  title, content, category,
  content='notes', content_rowid='id',
  tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, title, content, category) VALUES (new.id, new.title, new.content, new.category);
END;
CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, content, category) VALUES ('delete', old.id, old.title, old.content, old.category);
END;
CREATE TRIGGER notes_au AFTER UPDATE OF title, content, category ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, content, category) VALUES ('delete', old.id, old.title, old.content, old.category);
  INSERT INTO notes_fts(rowid, title, content, category) VALUES (new.id, new.title, new.content, new.category);
END;

CREATE TABLE note_tags (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL,
  tag     TEXT NOT NULL,
  PRIMARY KEY (note_id, tag)
);
CREATE INDEX note_tags_user ON note_tags(user_id, tag);

-- [[Wiki links]] by lower-cased title, relative Markdown links as "path:…".
CREATE TABLE note_links (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL,
  target  TEXT NOT NULL,
  PRIMARY KEY (note_id, target)
);
CREATE INDEX note_links_user ON note_links(user_id, target);

-- Every saved state of a note, whoever changed it (this app, Nextcloud, a
-- phone, another editor on the folder). Saves close together from the same
-- source are folded into one version.
CREATE TABLE note_versions (
  id         INTEGER PRIMARY KEY,
  note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  path       TEXT NOT NULL DEFAULT '',
  content    TEXT NOT NULL DEFAULT '',
  hash       TEXT NOT NULL DEFAULT '',
  source     TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX note_versions_note ON note_versions(note_id, id DESC);

-- Deleted notes, kept with their text for TRASH_DAYS: deleted here, or found
-- gone from the folder.
CREATE TABLE trash (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note_id    INTEGER NOT NULL DEFAULT 0,
  path       TEXT NOT NULL,
  title      TEXT NOT NULL,
  category   TEXT NOT NULL DEFAULT '',
  content    TEXT NOT NULL DEFAULT '',
  favorite   INTEGER NOT NULL DEFAULT 0,
  pinned     INTEGER NOT NULL DEFAULT 0,
  reason     TEXT NOT NULL DEFAULT 'deleted',
  deleted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX trash_user ON trash(user_id, deleted_at DESC);

-- Read-only public links to one note.
CREATE TABLE shares (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  token      TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL DEFAULT '',
  views      INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX shares_note ON shares(note_id);
