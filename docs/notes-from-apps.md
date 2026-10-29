# Notes about things in other apps

A book in Pocket Bookshelf, an episode in Pocket Podcasts, a trip in Pocket Travel, a person in
Pocket Contacts, a drawing in Pocket Draw: the notes
about it are ordinary notes in Pocket Notes. A note is *about* a thing when it **links to the
thing's page** in its app. Nothing else is stored anywhere: no notes field in each app, no
table of attachments. The link is the relation, it reads well in any Markdown editor, and it
still works if either app is taken away.

Each app then shows a **Notes** box on the thing's page: the person's notes that link there, and
**+ New note**, which opens Pocket Notes with a note already linked.

## From inside a note

**Link to something** (`/` in the editor, or Homelab ▾ in Markdown mode) searches your books in
Pocket Bookshelf and your episodes in Pocket Podcasts along with files, documents and the rest.
Picking one inserts `[📖 Dune — Frank Herbert](…/books/12)` or `[🎧 Episode — Show](…/episodes/7)`;
an episode you are part-way through is offered a second time at the moment reached
(`…/episodes/7?t=754`). Pasting a book's or an episode's address by hand works the same. Notes
reaches the two apps through Pocket Core (links `books:read`, `history:read`).

People in Pocket Contacts come up there too (with their picture or initials), and while typing:
**`@` and a letter or more** of a name, in the rich editor and in Markdown mode, lists the people
whose names match (`GET /api/v1/tools/contacts?q=`, asked of Contacts as the person, link
`contacts:read`). Return, Tab or a click writes `[Marko Petrović](https://contacts…/contacts/12)`,
the person's page, which lists the note in its Notes box; Escape, or nothing matching, leaves the
`@` as typed. `@today` and the other dates work as before. With Contacts switched off in Settings
→ Integrations (or no link in Core) the list never asks and `@` is plain text.

## Drawings from Pocket Draw

A sketch or diagram from Pocket Draw is shown *in* the note, as its picture, rather than linked.
**Drawing** in the `/` menu (or Homelab ▾ → Drawing… in Markdown mode, or a drawing picked in Link
to something) puts in

```
![Kitchen plan](https://draw.example.com/d/12/picture.svg "Kitchen plan")
```

Draw's picture of drawing 12, which sits below the drawing's page `https://draw.example.com/d/12`,
so the rule above makes the note one *about* the drawing: Draw asks
`GET /api/v1/mentions?url=https://draw.example.com/d/12` for its Notes box. The picker also starts
a new sketch or text diagram in Draw (`POST /api/v1/tools/draw {kind, title}`), puts it in the note
and opens it in a new tab to be drawn.

Draw's picture needs a sign-in there, so the editor shows it through Notes:
`GET /api/v1/draw/12/picture.svg` fetches Draw's `GET /api/v1/drawings/12/preview.svg` with the
person's Core link (`drawings:read`; starting one needs `drawings:write`) and serves it as a locked
down SVG (no script, nothing fetched from elsewhere), kept for 30 seconds, so the note shows the
drawing as it is now. Not connected, switched off, or no picture yet: a grey card says so instead.
A shared note's page shows the drawings that note holds, fetched as the person who shared it
(`/api/public/share/{token}/draw/12/picture.svg`). The Markdown keeps Draw's address; clicking the
picture opens the drawing in Draw. Draw starts a note about a drawing with
`/new?title=Kitchen plan&url=https://draw…/d/12/picture.svg&label=Kitchen plan&image=1`.

## The two calls

**Which notes are about this?** — as the person, through Pocket Core:

```
POST {core}/api/v1/links/token   {"user": "<email>", "app": "notes", "scopes": ["notes:read"]}
GET  {notes}/api/v1/mentions?url=https://bookshelf.example.com/books/12
     Authorization: Bearer pcx_…
→ [{"id": 401, "title": "Dune, thoughts", "category": "Books", "excerpt": "…", "modified": 1792500000, …}]
```

A note matches when it links to the same address, to one below it (`/books/12/quotes`), or to
it with a query (`/episodes/7?t=95`). Host case, scheme, fragment and a trailing slash don't
matter. Only that person's notes are searched. Up to 20 `url=` at once.

**Start a note about this** — a link people open, to Notes' public address:

```
{notes public}/new?title=Dune&url=https://bookshelf.example.com/books/12&label=📖 Dune — Frank Herbert&folder=Books
```

It makes `# Dune`, then `[📖 Dune — Frank Herbert](https://bookshelf…/books/12)`, in the folder
(default: the person's folder for new notes), and opens it. `text=` adds a line under the link.
`image=1` shows the thing as a picture instead of linking to it: `url=` is then the picture's
address, and the note gets `![label](url "label")` (brackets, quotes and parentheses left out of
the label). Signed out, it signs in first and carries on. The page has the server write the note
(`POST /api/v1/notes/from {title, url, label, folder?, text, image}`), so the rules are the same
from anywhere.

The token from Core (`ActAs` in the SDK) also carries `public_url`, Notes' address for people;
`url` is the one for servers.

## In an app

1. Manifest: `Uses: []core.Use{{App: "notes", Scopes: []string{"notes:read"}, Reason: "…"}}`.
   Core makes the link switched on; people can turn it off for themselves (Core → Settings →
   Apps acting for me), admins for the box (Core → Links).
2. An endpoint for the page, e.g. `GET /api/v1/books/{id}/notes`, that answers
   `{available, missing, reason, notes: [{id, title, folder, excerpt, modified, url}], new_url}`:
   - no Core configured, or no Pocket Notes on the box (`target_missing`): `missing: true` — the
     page shows no box, so the app on its own looks as it always did;
   - the link is off, or the person may not use Notes: `available: false` and a `reason` that
     says where to switch it on — the page shows the box greyed out, the reason as the
     button's tooltip.
3. A small card on the page. Pocket Bookshelf (`internal/server/notes.go`,
   `web/src/components/NotesCard.tsx`) and Pocket Podcasts (the same, `NotesPanel.tsx`, which
   also starts a note at the current moment: `url=…/episodes/7?t=95`, and plays from `?t=`
   when the episode page is opened from such a link) are the two to copy.

The thing's address must be the app's public `BASE_URL` plus the page's path, the same one
people see, so that a link pasted into a note by hand counts too.
