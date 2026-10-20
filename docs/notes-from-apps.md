# Notes about things in other apps

A book in Pocket Bookshelf, an episode in Pocket Podcasts, a trip in Pocket Travel: the notes
about it are ordinary notes in Pocket Notes. A note is *about* a thing when it **links to the
thing's page** in its app. Nothing else is stored anywhere: no notes field in each app, no
table of attachments. The link is the relation, it reads well in any Markdown editor, and it
still works if either app is taken away.

Each app then shows a **Notes** box on the thing's page: the person's notes that link there, and
**+ New note**, which opens Pocket Notes with a note already linked.

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
Signed out, it signs in first and carries on.

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
