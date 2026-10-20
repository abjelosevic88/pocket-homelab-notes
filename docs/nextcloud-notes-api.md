# Nextcloud Notes compatibility

Pocket Notes answers the API of the Nextcloud Notes app, so apps written for it work against it
by changing the server address: the Nextcloud Notes apps for Android and iOS, the Pocket Homelab
app's Notes, scripts.

**Sign in:** server = this app's address, username = your email, password = an API token
(Settings → Apps & API). Your account password works too with local accounts.

| Nextcloud | Here |
| --- | --- |
| `GET/POST /index.php/apps/notes/api/v1/notes` (`category`, `exclude`, `pruneBefore`, `chunkSize`, `chunkCursor`, `If-None-Match`) | yes |
| `GET/PUT/DELETE …/notes/{id}` with `If-Match` (412 answers the note as it is now) | yes |
| `GET/PUT …/settings` (`notesPath`, `fileSuffix`, `noteMode`) | yes; `notesPath` is the folder's name in Nextcloud when the notes live there, else `Notes` |
| `…/api/v0.2/…` | the same handlers |
| attachments: `GET/POST /index.php/apps/notes/notes/{id}/attachment?path=` and `…/api/v1/attachment/{id}` | yes |
| `GET /ocs/v2.php/cloud/capabilities` (`notes.api_version`) | yes |
| `GET /ocs/v2.php/search/providers/notes/search?term=` (unified search) | yes, full-text |
| `DELETE /ocs/v2.php/apps/files_lock/lock/{id}` | answers 200 (no locks here) |
| `PROPFIND/MKCOL/PUT/GET/DELETE/MOVE /remote.php/dav/files/{user}/<notesPath>/…` | yes, inside the notes folder only |
| `GET /status.php`, `GET /ocs/v2.php/cloud/user` | yes |
| `…/apps/notes/note/{id}` in a browser | opens the note here |

Ids are Nextcloud's file ids when the notes live in Nextcloud (see storage.md), so an app that
remembered ids keeps working when it is pointed here.
