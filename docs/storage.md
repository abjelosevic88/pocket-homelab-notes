# Where notes live

A note is a Markdown (`.md`) or text (`.txt`) file. Folders are categories, exactly as Nextcloud
Notes keeps them, so the same folder can be opened by Nextcloud Notes, Obsidian, a text editor or
a sync client at the same time. Pocket Notes keeps an index of the folder in SQLite (for search,
tags, links, tasks, history and the trash); the files stay the truth.

Each person chooses their own place in **Settings → Storage**:

| Kind | What it is |
| --- | --- |
| **In Pocket Notes** | `data/notes/<user id>/`, backed up with the app's data. The default. |
| **Nextcloud** | A folder in your Nextcloud, through WebDAV — the one Nextcloud Notes uses, if you like (`Notes`, or what its settings say). With Pocket Core, nothing to type: Core holds your Nextcloud account. Otherwise your username and an app password. |
| **WebDAV** | Any WebDAV folder: ownCloud, Synology, a NAS, Seafile, Apache/nginx DAV. |
| **Your own folder** | A folder inside one of `STORAGE_ROOTS` (mounted into the container). |

## Keeping in step

- Every few minutes (Settings → Writing), when a list is shown and when a note is
  opened, the folder is read again. Nextcloud tells cheaply whether anything changed (its folder
  ETag), so an unchanged folder costs one request.
- A file changed outside is read again and the change becomes a version in the note's history.
- A file moved or renamed outside keeps its note (history, star, pin): Nextcloud's file id follows
  it, elsewhere identical text does.
- A file deleted outside goes to the **Trash** here with its last text, for `trash_days`.
- A save never overwrites what someone else wrote meanwhile: every write carries the file's ETag;
  when it changed, the note shows *Changed somewhere else* and you choose theirs, yours, or keeping
  yours as a copy.
- A folder that suddenly answers with no notes at all (a wrong password, an unmounted disk) never
  empties the index: the sync stops and says so.
- Nextcloud's favourite mark is the note's star, both ways. The lock Nextcloud's own editor leaves
  on a file is released by its owner when a save meets it, as the Pocket Homelab app does.

## Moving

Choosing another place asks what to do with the notes you have:

- **Copy my notes there** — every note and file goes over (a name already taken there gets
  "(copy)"); history, stars and pins come along. The old place is not touched.
- **Start from what is there** — the notes in the new place show; the old ones stay where they
  were, untouched.

## Ids

When the notes live in Nextcloud, a note's id is Nextcloud's file id — the id Nextcloud Notes
uses — so links like `…/apps/notes/note/401450` and the Pocket Homelab app's podcast notes keep
pointing at the same notes. Elsewhere ids start at 1 000 000 000, far from Nextcloud's.
