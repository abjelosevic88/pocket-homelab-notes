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
| **Pocket Files** | A folder in your Pocket Files (`Notes`, or any folder of your own there), through Pocket Core — nothing to type. Offered only when the server has `CORE_URL`. |
| **WebDAV** | Any WebDAV folder: ownCloud, Synology, a NAS, Seafile, Apache/nginx DAV. |
| **Your own folder** | A folder inside one of `STORAGE_ROOTS` (mounted into the container). |

## Pocket Files

Pocket Notes reaches Pocket Files' WebDAV (`<files>/dav/`, your home is its root) as you, with a
short-lived Pocket Core link token as the password. The link is declared in Pocket Notes' manifest
(`files:read`, `files:write`: "Keep your notes as Markdown files in Pocket Files"), so it shows in
Core → Links as **Pocket Notes → Pocket Files** and works only while it is on there, for the box
and for you. Each person gets their own tokens for their own home; one is used for at most four
minutes, a refused one is replaced at once, and when Core says no (the link switched off, Files
not registered) the sync stops with Core's reason and the index is kept.

- Choose a folder of your own (`Notes`); it is made when missing. `Shared with me` and `Mounts`
  are Pocket Files' made-up folders, not a place for the whole library.
- Pocket Files has no file ids: notes there are known by their path (ids from 1 000 000 000), and
  a file renamed in Pocket Files is followed by its text, as on any WebDAV server.
- Pocket Files gives folders no ETag, so each sync lists the folder (Nextcloud's one-request
  "nothing changed" does not apply); files are re-read only when their ETag (size and time) changed.
- A note deleted here goes to your Pocket Files trash (and to this app's trash).
- There is no Integrations row for Pocket Files: it is only a place for notes, always through Core.

## Keeping in step

- Every few minutes (Settings → Writing), when a list is shown and when a note is
  opened, the folder is read again. Nextcloud tells cheaply whether anything changed (its folder
  ETag), so an unchanged folder costs one request.
- A file changed outside is read again and the change becomes a version in the note's history.
- A file moved or renamed outside keeps its note (history, star, pin): Nextcloud's file id follows
  it, elsewhere (Pocket Files included) identical text does.
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
