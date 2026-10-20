# Backup

Everything is under `DATA_DIR` (`/data` in the image):

- `pocket-notes.db` (+ `-wal`, `-shm`) — the database. **Never copy it raw while the app runs**: use `pocket-notes backup` (`VACUUM INTO`, consistent), `GET /api/v1/backup` (admin) or a snapshot tool.
- `media/<podcast id>/` — downloaded episodes and their thumbnails. Big, regenerable (podcast files can be downloaded again; YouTube ones too while the video exists).
- `artwork/` — cached and custom show artwork. Small.
- `ytdlp/` — an in-app yt-dlp update. Regenerable.
- `backups/` — where the CLI backup writes.
- `core.token`, `core.token.oidc.json` — Pocket Core credentials.

A sensible nightly plan backs up the consistent DB copy, `artwork/` and the Core files, and skips `media/`. Restore: stop the app, put the `.db` back (remove stale `-wal`/`-shm`), start.

Everyone can export their own subscriptions as OPML and their data as JSON (Settings → Backup & data).
