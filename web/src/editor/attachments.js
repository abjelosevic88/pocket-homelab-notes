// Pictures and files that belong to a note. In the note they are paths relative to its folder
// (`.attachments.123/photo.jpg` as Nextcloud's own editor writes them, `../attachments/photo.jpg`
// for the app's attachments folder; Nextcloud Notes reads upwards as far as the notes folder). The page has no
// network, so the app serves their bytes under a scheme of its own, and takes uploads.

export const state = { noteID: null, share: null }

const isStoredWithNote = (target) => {
  const t = String(target || '').trim()
  if (!t || t[0] === '#' || t[0] === '/' || t.includes('\\')) { return false }
  if (/^[A-Za-z][A-Za-z0-9+.-]*:/.test(t)) { return false }
  // Upwards only at the start; a path that climbs after it went down is nothing the app writes.
  const parts = t.split('/').filter(Boolean)
  let up = 0
  while (parts[up] === '..') { up += 1 }
  return up < parts.length && !parts.slice(up).includes('..')
}

// What the page loads for an image in the note. Anything that is not a file of this note is
// left as it is (and an address out on the network is then refused by the page's rules).
export function displayURL(src) {
  if (!isStoredWithNote(src) || (state.noteID == null && !state.share)) { return src }
  let path = src
  try { path = decodeURIComponent(src) } catch (e) {}
  if (state.share) { return '/api/public/share/' + encodeURIComponent(state.share) + '/attachment?path=' + encodeURIComponent(path) }
  return '/api/v1/notes/' + encodeURIComponent(state.noteID) + '/attachment?path=' + encodeURIComponent(path)
}

// Stores the file with the note on the server and answers the path to write into the note.
export async function upload(file) {
  if (state.noteID == null) { throw new Error('Save the note first') }
  const form = new FormData()
  form.append('file', file, file.name || 'image.png')
  const res = await fetch('/api/v1/notes/' + encodeURIComponent(state.noteID) + '/attachments', { method: 'POST', body: form, credentials: 'same-origin', headers: { 'X-Pocket-Source': 'web' } })
  const answer = await res.json().catch(() => ({}))
  if (!res.ok || !answer.path) { throw new Error(answer.error || 'The upload failed') }
  return encodePath(answer.path)
}

// Spaces and brackets would end a Markdown target early.
export const encodePath = (path) => String(path).split('/').map((part) => encodeURIComponent(part).replace(/[()]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase())).join('/')
