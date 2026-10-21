import { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note } from '../lib/types'
import { copyText } from '../lib/clipboard'
import { Confirm, Field, Menu, type MenuItem, Modal, useToast } from './ui'
import { MoveModal, ShareModal } from './NoteDialogs'
import { noteURL } from './NoteView'

/** A change asked of a note from outside its editor (the list's right-click menu). */
export type NoteAction = { kind: 'patch'; body: Record<string, unknown>; msg?: string } | { kind: 'rename'; title: string } | { kind: 'delete' }

// The note open in the editor takes the change itself, so what is typed there and the change
// never cross; it answers by cancelling the event. Any other note is changed here.
function askOpenNote(id: number, action: NoteAction): boolean {
  return !window.dispatchEvent(new CustomEvent('pn:note-action', { detail: { id, action }, cancelable: true }))
}

// The heading the note starts with follows a new title, as it does when renaming in the editor.
function retitled(content: string, from: string, to: string): string | undefined {
  const lines = content.split('\n')
  const first = lines.findIndex(l => l.trim())
  const m = first >= 0 ? /^(#{1,6})\s+(.*)$/.exec(lines[first].trim()) : null
  if (!m || m[2].trim() !== from) return undefined
  lines[first] = `${m[1]} ${to}`
  return lines.join('\n')
}

export function NoteMenu({ note, x, y, onClose }: { note: Note; x: number; y: number; onClose: () => void }) {
  const { bump, settings } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [sheet, setSheetRaw] = useState<'' | 'rename' | 'move' | 'share' | 'delete'>('')
  const sheetRef = useRef<typeof sheet>('')
  const setSheet = (k: typeof sheet) => { sheetRef.current = k; setSheetRaw(k) }
  const [title, setTitle] = useState(note.title)

  const run = async (action: NoteAction, done?: string) => {
    onClose()
    if (askOpenNote(note.id, action)) return
    try {
      if (action.kind === 'delete') {
        await api.del(`${V1}/notes/${note.id}`)
      } else if (action.kind === 'rename') {
        const full = await api.get<Note>(`${V1}/notes/${note.id}?peek=1`)
        const content = retitled(full.content || '', full.title, action.title)
        await api.put(`${V1}/notes/${note.id}`, content !== undefined ? { title: action.title, content, etag: full.etag } : { title: action.title })
      } else {
        await api.put(`${V1}/notes/${note.id}`, action.body)
      }
      bump()
      if (done) toast(done)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const open = (path: string) => { onClose(); navigate(`${path}${location.search}`) }
  const items: MenuItem[] = [
    { label: 'Open', icon: 'note', onSelect: () => open(`/note/${note.id}`) },
    { label: 'Open in a new tab', icon: 'external', onSelect: () => window.open(`/note/${note.id}`, '_blank') },
    'sep',
    { label: note.favorite ? 'Remove from favourites' : 'Favourite', icon: 'star', onSelect: () => run({ kind: 'patch', body: { favorite: !note.favorite } }) },
    { label: note.pinned ? 'Unpin' : 'Pin to the top', icon: 'pushpin', onSelect: () => run({ kind: 'patch', body: { pinned: !note.pinned } }) },
    { label: 'Rename…', icon: 'edit', onSelect: () => setSheet('rename') },
    { label: 'Move to…', icon: 'move', onSelect: () => setSheet('move') },
    { label: 'Duplicate', icon: 'copy', onSelect: async () => {
      onClose()
      try { const n = await api.post<Note>(`${V1}/notes/${note.id}/duplicate`); bump(); navigate(`/note/${n.id}${location.search}`) } catch (e) { toast((e as Error).message, 'err') }
    } },
    { label: 'Share a link…', icon: 'share', onSelect: () => setSheet('share') },
    'sep',
    { label: 'Copy link', icon: 'link', onSelect: async () => { onClose(); const ok = await copyText(noteURL(note.id)); toast(ok ? 'Link copied' : 'Could not copy the link', ok ? 'ok' : 'err') } },
    { label: 'Download .md', icon: 'download', onSelect: () => { onClose(); location.href = `${V1}/notes/${note.id}/download` } },
    'sep',
    { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setSheet('delete') },
  ]

  return (
    <>
      {/* The menu closes before its item runs: an item that opens a dialog keeps this alive for it. */}
      {!sheet && <Menu anchor={{ x, y, w: 0, h: 0 }} items={items} onClose={() => setTimeout(() => { if (!sheetRef.current) onClose() }, 0)} width={220} />}
      {sheet === 'rename' && <Modal title="Rename" onClose={onClose} size="sm">
        <form className="stack" onSubmit={e => { e.preventDefault(); const t = title.trim(); if (t && t !== note.title) run({ kind: 'rename', title: t }, 'Renamed'); else onClose() }}>
          <Field label="Title"><input autoFocus value={title} onChange={e => setTitle(e.target.value)} onFocus={e => e.target.select()} /></Field>
          <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary">Rename</button></div>
        </form>
      </Modal>}
      {sheet === 'move' && <MoveModal current={note.category} onClose={onClose} onMove={cat => run({ kind: 'patch', body: { category: cat }, msg: `Moved to ${cat || 'Notes'}` }, `Moved to ${cat || 'Notes'}`)} />}
      {sheet === 'share' && <ShareModal note={note} onClose={onClose} />}
      {sheet === 'delete' && <Confirm title={`Delete “${note.title}”?`} message={<>It goes to the trash (Trash in the sidebar) for {settings?.trash_days ?? 30} days{settings?.storage_kind === 'nextcloud' ? ', and to Nextcloud’s own trash bin' : ''}.</>} onCancel={onClose} onConfirm={() => run({ kind: 'delete' }, 'Moved to the trash')} />}
    </>
  )
}
