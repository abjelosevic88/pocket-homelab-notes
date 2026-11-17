-- What a template needs beyond its file (a note in the person's templates
-- folder): the title its new notes get, the folder they go to, and whether
-- everyone on this server may use it. No row = the defaults (the template's
-- name, the default folder, only its owner). Not tied to notes(id), so a
-- template restored from the trash under its old id keeps its settings.
CREATE TABLE templates (
  note_id       INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title_pattern TEXT NOT NULL DEFAULT '',
  folder        TEXT NOT NULL DEFAULT '',
  shared        INTEGER NOT NULL DEFAULT 0,
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX templates_user ON templates(user_id);
CREATE INDEX templates_shared ON templates(shared) WHERE shared = 1;
