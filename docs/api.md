# HTTP API

Everything the web app does goes through `/api/v1`. Authenticate with the session cookie, with
`Authorization: Bearer <token>` (Settings → Apps & API), or with Basic auth (`email:token`).
Add `X-Pocket-Source: shortcut` (or `app`) to have your changes named so in a note's history.

## Notes

| | |
| --- | --- |
| `GET /notes?category=&recursive=1&tag=&favorite=1&pinned=1&tasks=1&sort=modified\|title\|created\|opened&limit=&since=` | notes without their text, `excerpt`, `tags`, task counts |
| `POST /notes {title?, category?, content?, favorite?, pinned?, template_id?}` | a new note; no title → its first line |
| `GET /notes/{id}` | with `content` and `etag` (re-read from the storage when it changed there) |
| `PUT /notes/{id} {content?, title?, category?, favorite?, pinned?, etag?}` | `etag` (or `If-Match`) guards against overwriting; 409 + `{note}` when it changed |
| `DELETE /notes/{id}` | to the trash |
| `POST /notes/{id}/append {text}` | adds a line or block at the end |
| `POST /notes/{id}/tasks {line, text, done}` | ticks a checkbox |
| `POST /notes/{id}/duplicate` | |
| `GET /notes/{id}/backlinks` | `{backlinks, mentions}` |
| `GET /notes/{id}/versions`, `GET …/versions/{vid}`, `POST …/versions/{vid}/restore` | history with a line diff against now |
| `POST /notes/{id}/attachments` (multipart `file`) | `{path, markdown}` |
| `GET /notes/{id}/attachment?path=` | a file the note links to |
| `GET/POST /notes/{id}/shares {days?}`, `GET /shares`, `DELETE /shares/{id}` | public read-only links (`/s/<token>`) |
| `GET /notes/{id}/download` | the Markdown file |

## Finding

| | |
| --- | --- |
| `GET /search?q=` | full text, best first, `snippet` with `[[[found]]]` marks. Filters: `tag:x`, `#x`, `in:Folder`, `is:fav`, `is:pinned`, `has:tasks`, `"a phrase"` |
| `GET /suggest?q=` | titles, for completion |
| `GET /resolve?target=&from=` | the note a `[[link]]` or relative path means |
| `GET /mentions?url=…[&url=…]` | notes that link to a web page (a book, an episode in another app): the same address, one below it, or with a query (`?t=95`); newest first, without content. See [notes-from-apps.md](notes-from-apps.md) |
| `GET /folders`, `POST /folders {path}`, `PUT /folders {from, to}`, `DELETE /folders?path=` | |
| `GET /tags`, `GET /tasks?state=open\|done\|all` | |

## Days

| | |
| --- | --- |
| `GET /daily?date=YYYY-MM-DD` / `POST /daily {date?}` | today's (or a day's) daily note; POST makes it from the template |
| `POST /capture {text, date?, note_id?, raw?}` or `text/plain` body | `- 9:12 text` into today's note (format in Settings) |
| `GET /calendar?month=YYYY-MM` | days with a daily note and how many notes were edited |
| `GET /templates` | the notes in the templates folder |

## The rest

`GET /dashboard` (Homepage: `notes`, `edited_today`, `open_tasks`, `due_tasks`, `last_sync` …),
`GET/PUT /storage`, `POST /storage/test`, `POST /sync`, `GET/PUT /settings`, `GET /trash`,
`POST /trash/{id}/restore`, `GET /export.zip`, `POST /import` (files or a zip), `GET /backup` (admin),
`GET /integrations`, `PUT /integrations/{svc|app}/{name}`, `GET /tools` and the homelab tools under
`/tools/…` (link search, bookmark, Immich photos, Nextcloud files, reports, task, expense).

## Shortcuts

```sh
# A line into today's note
curl -X POST https://notes.example.com/api/v1/capture -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: text/plain' --data 'Call the plumber'

# A note from stdin
jq -n --arg c "$(cat)" '{title:"From the terminal", category:"Inbox", content:$c}' |
  curl -X POST https://notes.example.com/api/v1/notes -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @-
```
