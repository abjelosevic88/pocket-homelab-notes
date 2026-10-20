# Backup

Two things to keep:

1. **The notes** — wherever each person keeps them. Built-in notes are files under
   `data/notes/<user id>/`; Nextcloud, WebDAV and server folders are backed up with those.
   Settings → Import & export → *Download all notes* gives anyone their whole folder as a zip.
2. **The database** — `data/pocket-notes.db`: the index plus what only it has: version history,
   the trash, shared links, pins, settings and integrations. Take a consistent copy with
   `pocket-notes backup [-o FILE]` (SQLite `VACUUM INTO`) while the app runs, or download it
   (Settings → Import & export, admins only). Don't copy the live `.db` file with its WAL.

Restore: stop the app, put the database back, start it; it reads every notes folder again and
fills in whatever changed meanwhile.
