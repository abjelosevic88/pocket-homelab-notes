-- Notes shared with other people on this server. The note stays the owner's
-- file in the owner's storage; a member reads it (and, with can_edit, writes
-- it) through the owner's library.
CREATE TABLE note_members (
  note_id    INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  can_edit   INTEGER NOT NULL DEFAULT 0,
  opened_at  TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (note_id, user_id)
);
CREATE INDEX note_members_user ON note_members(user_id);
CREATE INDEX note_members_owner ON note_members(owner_id);
