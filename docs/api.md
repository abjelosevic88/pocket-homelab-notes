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
| `GET /notes/{id}/members`, `POST /notes/{id}/members {user_id\|email, can_edit}`, `DELETE /notes/{id}/members/{uid}` | share with people on this server (owner adds/changes; a member can remove only themself) |
| `GET /shared` → `{with_me, by_me}`, `GET /people` | the Shared view; who a note can be shared with. A shared note answers the usual `/notes/{id}…` routes for its members, with `shared_from` (read only unless `can_edit`) |
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
| `GET /templates[?mine=1]` | your templates (notes in the templates folder) with `title_pattern`, `folder`, `everyone`, `next_title`; then others' shared with everyone (`owner`) |
| `GET /templates/{id}?title=` | a template's text with its placeholders filled in |
| `POST /templates {name, content? \| from_note_id?, title_pattern?, folder?, everyone?}` | save a template: blank, or a copy of a note ("Save as template") or of a shared template |
| `PUT /templates/{id} {name?, title_pattern?, folder?, everyone?}` | rename it or change its settings (its text is changed like any note's) |
| `DELETE /templates/{id}` | to the trash |
| `POST /templates/{id}/use {title?, folder?, here?}` | a new note from it in one step: title from its pattern, in its folder, else `here`, else the default folder |
| `POST /templates/preview {pattern, name}` | what a title pattern gives today |

## For Pocket Search, Day and Hub

The four routes every Pocket app answers the same way (capabilities in Pocket Core), so Pocket
Search, Pocket Day and Pocket Hub work with Pocket Notes without knowing it. You can call them
yourself too, signed in or with an API token. They answer only your own notes; the three `GET`
ones change nothing.

| | |
| --- | --- |
| `GET /pocket/search?q=&limit=` | `{items: [{id, title, snippet, kind: "note", date, url, score}]}`: the search above, filters included, best first (higher `score` = better); `snippet` is plain text; 8 unless `limit` says more, 50 at most |
| `GET /pocket/day/{YYYY-MM-DD}[?tz=Europe/Berlin]` | `{date, summary, items: [{id, time, title, sub, kind, url}]}`: the notes made that day (`written`), the older ones changed that day (`edited`) and its daily note (`daily`). `time` is `HH:MM` of the last change (of the making, for a note changed again since), `sub` the folder, `summary` a line like `2 notes written, 1 edited`. The day is counted in `tz`, else in the server's time zone; a day with nothing answers no items |
| `GET /pocket/widget` | `{line, items: [{id, title, detail, url}], url}`: six notes, pinned ones first (three at most), then the ones changed last; `detail` is the day and the folder (`today · Recipes`), `line` how many changed today |
| `POST /pocket/capture {request_id, kind?: "note", title?, text?}` | a new note in the folder your new notes go to (Settings → "New notes go to"); no title → its first line. `201 {id, title, url}`. `request_id` (16–80 letters, digits, `_`, `-`) names the try: sent again it answers the same note and adds nothing. `400 {error}` when there is nothing to keep |

Another Pocket app reaches each of them with its own scope and nothing else: `pocket:search`,
`pocket:day`, `pocket:widget`, `pocket:capture`. A note kept through `pocket:capture` names that
app in its history.

## The rest

`GET /dashboard` (Homepage: `notes`, `edited_today`, `open_tasks`, `due_tasks`, `last_sync` …),
`GET/PUT /storage`, `POST /storage/test`, `POST /sync`, `GET/PUT /settings`, `GET /trash`,
`POST /trash/{id}/restore`, `GET /export.zip`, `POST /import` (files or a zip; front matter `title`/`created`/`updated` honoured, notes already there skipped), `GET /pocket/portable` (Pocket Move), `GET /backup` (admin),
`GET /integrations`, `PUT /integrations/{svc|app}/{name}`, `GET /tools` and the homelab tools under
`/tools/…` (link search, bookmark, people from Pocket Contacts for `@` — `GET /tools/contacts?q=` →
`{enabled, contacts: [{id, name, detail, initials, color, photo, url, markdown}]}`, `enabled: false` when
Contacts is off — and their pictures `GET /tools/contacts/{id}/photo?s=`, Immich photos, Nextcloud files,
reports, task, expense). Drawings from Pocket Draw: `GET /tools/draw?q=` → `{connected, error?, items: [{id,
title, kind, detail, edited_at, picture, url, markdown}]}` (`markdown` is the picture embed, `url` the
drawing's page), `POST /tools/draw {kind: sketch|mermaid, title}` → `{id, title, url, markdown}`,
`GET /draw/{id}/picture.svg` (the drawing's picture fetched from Draw as you, or a grey placeholder),
`GET /tools/addresses` → `{draw: "https://draw…"}`. `POST /notes/from {title, url, label, folder?, text,
image}` makes the note `/new?…` asks for ([notes-from-apps.md](notes-from-apps.md)).

## Shortcuts

```sh
# A line into today's note
curl -X POST https://notes.example.com/api/v1/capture -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: text/plain' --data 'Call the plumber'

# A note from stdin
jq -n --arg c "$(cat)" '{title:"From the terminal", category:"Inbox", content:$c}' |
  curl -X POST https://notes.example.com/api/v1/notes -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @-
```
