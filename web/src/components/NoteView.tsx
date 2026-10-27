import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { EMBEDDED } from '../lib/embed'
import { api, ApiError, upload, V1, qs } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note, Person } from '../lib/types'
import { ago, headings, stamp, wordCount, leaf } from '../lib/format'
import { Confirm, Loading, type MenuItem, MenuButton, Segmented, useToast } from './ui'
import { Icon } from './icons'
import { RichEditor, type EditorMessage, type RichEditorHandle } from './RichEditor'
import { MarkdownEditor, type MarkdownEditorHandle } from './MarkdownEditor'
import { BookmarkSheet, DiagramViewer, ExpenseSheet, FilesPicker, HabitsSheet, LinkPicker, PhotoPicker, TaskSheet, TemplatePicker, VoiceMemo, toolError } from './Tools'
import { HistoryModal, MoveModal, ShareModal } from './NoteDialogs'
import { exportHTML, printNote } from '../lib/export'
import { copyText } from '../lib/clipboard'
import type { NoteAction } from './NoteMenu'
import { AskButton } from './AskButton'

type Status = 'saved' | 'dirty' | 'saving' | 'error' | 'conflict' | 'offline'
type Sheet =
  | { kind: 'link' } | { kind: 'bookmark' } | { kind: 'photos' } | { kind: 'files' } | { kind: 'habits' } | { kind: 'voice' } | { kind: 'templates' }
  | { kind: 'task'; line: Line | null } | { kind: 'expense'; line: Line | null } | { kind: 'diagram'; svg: string }
  | { kind: 'history' } | { kind: 'share' } | { kind: 'move' } | { kind: 'delete' }
type Line = { text: string; from?: number }

const REPORTS: Record<string, string> = { snapshot: 'the homelab', attention: 'what needs attention', media: 'this week’s viewing and listening', spent: 'this month’s spending', fuel: 'the vehicle log', hosts: 'the network', processes: 'the server' }

/** Where a note link points: the form Nextcloud Notes and the Pocket Homelab app understand. */
export const noteURL = (id: number) => `${location.origin}/index.php/apps/notes/note/${id}`
const noteIdIn = (url: string): number | null => {
  const m = /\/(?:index\.php\/)?apps\/notes\/note\/(\d+)/.exec(url) || (url.startsWith(location.origin) || url.startsWith('/') ? /\/note\/(\d+)/.exec(url) : null)
  return m ? Number(m[1]) : null
}

/** What a page around a note may ask of it: save what is typed now, read it again. */
export interface NoteViewHandle { saveNow(): Promise<void>; reload(): Promise<void> }

const NoteView = forwardRef<NoteViewHandle, { id: number; onBack?: () => void; embedded?: boolean }>(function NoteView({ id, onBack, embedded }, ref) {
  const { settings, tools, theme, bump } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [note, setNote] = useState<Note | null>(null)
  const [err, setErr] = useState('')
  const [draft, setDraft] = useState('')
  const [revision, setRevision] = useState(0)
  const [status, setStatus] = useState<Status>('saved')
  const [conflict, setConflict] = useState<Note | null>(null)
  const [mode, setMode] = useState<'rich' | 'markdown'>(() => (localStorage.getItem('pn-mode') as 'rich' | 'markdown') || settings?.editor || 'rich')
  // On a phone the panel covers the note, so it is never open on arrival there.
  const [panel, setPanel] = useState<'' | 'outline' | 'links' | 'info'>(() => phone() ? '' : (localStorage.getItem('pn-panel') as '' | 'outline' | 'links' | 'info') || '')
  const [sheet, setSheet] = useState<Sheet | null>(null)
  // Reader mode stays on from note to note; an empty note still opens for writing.
  const [readPref, setReadPref] = useState(() => localStorage.getItem('pn-read') === '1')
  const [blank, setBlank] = useState(false)
  const [title, setTitle] = useState('')
  const rich = useRef<RichEditorHandle>(null)
  const md = useRef<MarkdownEditorHandle>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const cameraInput = useRef<HTMLInputElement>(null)

  // What the server has, and what is on screen, by note: a save in flight belongs to its note.
  const cur = useRef<{ id: number; etag: string; saved: string; draft: string; saving: boolean; again: boolean; timer: number }>({ id: 0, etag: '', saved: '', draft: '', saving: false, again: false, timer: 0 })

  useEffect(() => { localStorage.setItem('pn-mode', mode) }, [mode])
  useEffect(() => { localStorage.setItem('pn-read', readPref ? '1' : '') }, [readPref])
  useEffect(() => { if (!phone()) localStorage.setItem('pn-panel', panel) }, [panel])

  const save = useCallback(async (): Promise<void> => {
    const c = cur.current
    window.clearTimeout(c.timer)
    if (c.saving) { c.again = true; return }
    const text = c.draft, noteID = c.id
    if (text === c.saved || !noteID) return
    c.saving = true
    setStatus('saving')
    try {
      const n = await api.put<Note>(`${V1}/notes/${noteID}`, { content: text, etag: c.etag })
      if (cur.current.id !== noteID) return
      c.saved = text; c.etag = n.etag
      setNote(old => {
        if (old && (old.title !== n.title || old.category !== n.category)) bump()
        return { ...n, content: text }
      })
      setTitle(n.title)
      setStatus(c.draft === text ? 'saved' : 'dirty')
    } catch (e) {
      if (cur.current.id !== noteID) return
      if (e instanceof ApiError && e.status === 409 && e.data.note) {
        setConflict(e.data.note as Note); setStatus('conflict')
      } else {
        setStatus(e instanceof ApiError ? 'error' : 'offline')
        c.timer = window.setTimeout(() => save(), 8000)
      }
    } finally {
      c.saving = false
      if (c.again && cur.current.id === noteID) { c.again = false; if (c.draft !== c.saved) save() }
    }
  }, [bump])

  const changed = useCallback((text: string) => {
    const c = cur.current
    c.draft = text
    setDraft(text)
    if (text === c.saved) return
    setStatus(s => s === 'conflict' ? s : 'dirty')
    window.clearTimeout(c.timer)
    c.timer = window.setTimeout(() => { if (!conflictRef.current) save() }, 900)
  }, [save])
  const conflictRef = useRef<Note | null>(null)
  conflictRef.current = conflict

  // Opening a note: what was typed in the one before is saved first.
  useEffect(() => {
    let live = true
    const prev = cur.current
    const pending = rich.current?.flush()
    if (pending != null && prev.id) prev.draft = pending
    if (prev.id && prev.draft !== prev.saved && !conflictRef.current) {
      api.put(`${V1}/notes/${prev.id}`, { content: prev.draft, etag: prev.etag }).then(() => bump()).catch(e => toast('The last note was not saved: ' + (e as Error).message, 'err'))
    }
    window.clearTimeout(prev.timer)
    // The note on screen stays (and its editor with it) until the next one is here: no flash.
    setErr(''); setConflict(null); setSheet(null)
    cur.current = { ...prev, id: 0, timer: 0 }
    api.get<Note>(`${V1}/notes/${id}`).then(n => {
      if (!live) return
      setStatus('saved')
      cur.current = { id, etag: n.etag, saved: n.content || '', draft: n.content || '', saving: false, again: false, timer: 0 }
      setNote(n); setTitle(n.title); setDraft(n.content || ''); setBlank(!(n.content || '').trim()); setRevision(r => r + 1)
      if (n.path.toLowerCase().endsWith('.txt')) setMode('markdown')
    }).catch(e => live && setErr((e as Error).message))
    return () => { live = false }
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps

  // Saved before the page goes; a warning while something is not.
  useEffect(() => {
    const h = (e: BeforeUnloadEvent) => {
      const c = cur.current
      const pending = rich.current?.flush()
      if (pending != null) c.draft = pending
      if (c.draft !== c.saved) {
        e.preventDefault()
        save()
      }
    }
    window.addEventListener('beforeunload', h)
    return () => window.removeEventListener('beforeunload', h)
  }, [save])

  // Changes made elsewhere (the phone, Nextcloud, another tab) come in while nothing is typed here;
  // the server looks at the note's file in the storage each time (one cheap request).
  useEffect(() => {
    const look = async () => {
      const c = cur.current
      if (!c.id || document.hidden || c.saving || c.draft !== c.saved || conflictRef.current) return
      try {
        const n = await api.get<Note>(`${V1}/notes/${c.id}?peek=1`)
        if (cur.current !== c || n.etag === c.etag || c.draft !== c.saved) return
        c.etag = n.etag; c.saved = n.content || ''; c.draft = c.saved
        setNote(n); setTitle(n.title); setDraft(c.saved); setRevision(r => r + 1)
        toast('Updated with changes made elsewhere')
      } catch {}
    }
    // Every few seconds while the page is seen, and at once when it comes back into view.
    const t = window.setInterval(look, 4000)
    const onShow = () => { if (!document.hidden) look() }
    window.addEventListener('focus', onShow)
    document.addEventListener('visibilitychange', onShow)
    return () => { window.clearInterval(t); window.removeEventListener('focus', onShow); document.removeEventListener('visibilitychange', onShow) }
  }, [toast])

  // ⌘S saves now.
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') { e.preventDefault(); const p = rich.current?.flush(); if (p != null) changed(p); save() }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [save, changed])

  useImperativeHandle(ref, () => ({
    saveNow: async () => {
      const p = rich.current?.flush()
      if (p != null) changed(p)
      await save()
    },
    reload: async () => {
      const c = cur.current
      if (!c.id || c.draft !== c.saved) return
      const n = await api.get<Note>(`${V1}/notes/${c.id}?peek=1`)
      if (cur.current !== c || c.draft !== c.saved || n.etag === c.etag) return
      c.etag = n.etag; c.saved = n.content || ''; c.draft = c.saved
      setNote(n); setTitle(n.title); setDraft(c.saved); setRevision(r => r + 1)
    },
  }), [changed, save])

  const patch = async (body: Record<string, unknown>, msg?: string) => {
    if (!note) return
    try {
      await save()
      const n = await api.put<Note>(`${V1}/notes/${note.id}`, { ...body, etag: cur.current.etag })
      cur.current.etag = n.etag
      if (typeof body.content === 'string') { cur.current.saved = body.content; cur.current.draft = body.content; setDraft(body.content); setRevision(r => r + 1) }
      setNote(x => ({ ...n, content: x?.content ?? n.content })); setTitle(n.title); bump()
      if (msg) toast(msg)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.data.note) { setConflict(e.data.note as Note); setStatus('conflict') } else toast((e as Error).message, 'err')
    }
  }

  // A new title renames the file; a first line that was the title follows it.
  const rename = async () => {
    if (!note) return
    const t = title.trim()
    if (!t || t === note.title) { setTitle(note.title); return }
    await renameTo(t)
  }
  const renameTo = async (t: string) => {
    if (!note) return
    const text = cur.current.draft
    const lines = text.split('\n')
    const first = lines.findIndex(l => l.trim())
    let content: string | undefined
    if (first >= 0) {
      const m = /^(#{1,6})\s+(.*)$/.exec(lines[first].trim())
      if (m && m[2].trim() === note.title) { lines[first] = `${m[1]} ${t}`; content = lines.join('\n') }
    }
    await patch(content !== undefined ? { title: t, content } : { title: t })
  }
  const remove = async () => {
    if (!note) return
    cur.current.draft = cur.current.saved
    await api.del(`${V1}/notes/${note.id}`)
    bump(); toast('Moved to the trash'); navigate(`/${location.search}`)
  }

  // The list's right-click menu on this very note: the change goes through here, after what
  // is typed is saved, instead of past the editor (see NoteMenu).
  const onAction = useRef<(a: NoteAction) => void>(() => {})
  onAction.current = a => {
    if (a.kind === 'patch') patch(a.body, a.msg)
    else if (a.kind === 'rename') { setTitle(a.title); renameTo(a.title) }
    else remove().catch(e => toast((e as Error).message, 'err'))
  }
  useEffect(() => {
    const h = (e: Event) => {
      const d = (e as CustomEvent<{ id: number; action: NoteAction }>).detail
      if (embedded || d.id !== cur.current.id) return
      e.preventDefault()
      onAction.current(d.action)
    }
    window.addEventListener('pn:note-action', h)
    return () => window.removeEventListener('pn:note-action', h)
  }, [embedded])

  // ---- what goes into the note ----

  const put = (markdown: string, at: 'inline' | 'block' = 'block') => {
    if (!markdown) return
    if (mode === 'rich') { if (at === 'inline') rich.current?.insertInline(markdown); else rich.current?.insertMarkdown(markdown) }
    else md.current?.insert(markdown, at === 'block')
  }
  const replaceLine = (line: Line | null, markdown: string) => {
    if (line && line.from != null) {
      if (mode === 'rich' ? rich.current?.replaceLine(line.from, line.text, markdown) : md.current?.replaceLine(line.from, line.text, markdown)) return
    }
    put(markdown, 'inline')
  }
  const currentLine = (): Line | null => (mode === 'rich' ? rich.current?.currentLine() : md.current?.currentLine()) ?? null

  const uploadFiles = async (files: File[]): Promise<string[]> => {
    if (!note) return []
    const out: string[] = []
    for (const f of files) {
      try {
        const r = await upload<{ markdown: string }>(`${V1}/notes/${note.id}/attachments`, 'file', f)
        out.push(r.markdown)
      } catch (e) { toast(`${f.name}: ${(e as Error).message}`, 'err') }
    }
    return out
  }

  const report = async (kind: string) => {
    toast(`Reading ${REPORTS[kind] || kind}…`)
    try {
      const r = await api.post<{ markdown: string; empty: boolean; message?: string }>(`${V1}/tools/report/${kind}`, {})
      if (r.empty) toast(r.message || 'There was nothing to write down.', 'err'); else put(r.markdown, 'block')
    } catch (e) { toast(toolError(e), 'err') }
  }

  const runTool = (tool: string, line: Line | null) => {
    switch (tool) {
      case 'link': setSheet({ kind: 'link' }); break
      case 'bookmark': setSheet({ kind: 'bookmark' }); break
      case 'task': if (!line) { toast('Write the line first, then ask again on the line below it.', 'err'); return } setSheet({ kind: 'task', line }); break
      case 'expense': if (!line) { toast('Write the line first, then make it a transaction.', 'err'); return } setSheet({ kind: 'expense', line }); break
      case 'habits': setSheet({ kind: 'habits' }); break
      case 'voice': setSheet({ kind: 'voice' }); break
      case 'camera': case 'scan': cameraInput.current?.click(); break
      default: if (REPORTS[tool]) report(tool); else toast(`“${tool}” is not available here`, 'err')
    }
  }

  const openLink = async (url: string, raw = url) => {
    const wiki = /^\[\[(.+)\]\]$/.exec(raw)
    if (wiki && note) {
      try { const n = await api.get<Note>(`${V1}/resolve${qs({ target: wiki[1], from: note.id })}`); navigate(`/note/${n.id}${location.search}`) }
      catch { toast(`No note is called “${wiki[1]}” yet`, 'err') }
      return
    }
    const nid = noteIdIn(raw) ?? noteIdIn(url)
    if (nid) { navigate(`/note/${nid}${location.search}`); return }
    if (/^pockethomelab:/i.test(raw)) { window.location.href = raw; return }
    if (!/^[a-z][a-z0-9+.-]*:/i.test(raw) && !raw.startsWith('#') && note) {
      try {
        const n = await api.get<Note>(`${V1}/resolve${qs({ target: decodeURIComponent(raw), from: note.id })}`)
        navigate(`/note/${n.id}${location.search}`)
        return
      } catch {
        // A file kept with the note: open it.
        window.open(`${V1}/notes/${note.id}/attachment${qs({ path: decodeURIComponent(raw) })}`, '_blank', 'noopener')
        return
      }
    }
    window.open(url, '_blank', 'noopener')
  }

  const doExport = async (kind: 'pdf' | 'html') => {
    if (!note) return
    if (mode !== 'rich') { setMode('rich'); toast('Shown as rich text for the export — choose it again'); return }
    const frame = rich.current?.frame()
    if (!frame) return
    try {
      if (kind === 'pdf') await printNote(frame, note.title)
      else await exportHTML(frame, note.title)
    } catch (e) { toast((e as Error).message, 'err') }
  }

  const onMessage = (m: EditorMessage) => {
    switch (m.name) {
      case 'tool': runTool(String(m.tool), m.text ? { text: String(m.text), from: m.from as number } : currentLine()); break
      case 'attach':
        if (m.source === 'device') fileInput.current?.click()
        else if (m.source === 'nextcloud') setSheet({ kind: 'files' })
        else if (m.source === 'immich') setSheet({ kind: 'photos' })
        break
      case 'templates': setSheet({ kind: 'templates' }); break
      case 'share': doExport(m.kind === 'html' ? 'html' : 'pdf'); break
      case 'link': openLink(String(m.url), String(m.raw ?? m.url)); break
      case 'diagram': setSheet({ kind: 'diagram', svg: String(m.svg) }); break
      case 'code': copyText(String(m.code ?? '')).then(ok => toast(ok ? 'Command copied' : 'Could not copy the command', ok ? 'ok' : 'err')); break
    }
  }

  const onRequest = async (m: EditorMessage) => {
    if (m.name === 'notes') {
      const list = await api.get<Note[]>(`${V1}/notes?sort=modified&limit=2000`)
      return { notes: list.filter(n => n.id !== cur.current.id).map(n => ({ id: n.id, title: n.title || 'Untitled', folder: n.category, url: noteURL(n.id) })) }
    }
    if (m.name === 'contacts') return { contacts: await findPeople(String(m.q ?? '')) }
    return null
  }

  // People from Pocket Contacts for `@` and a name, in either editor: none when it is off or fails.
  const findPeople = async (q: string): Promise<Person[]> => {
    if (!q.trim() || !tools.contacts) return []
    try { return (await api.get<{ contacts: Person[] }>(`${V1}/tools/contacts${qs({ q: q.trim(), limit: 6 })}`)).contacts || [] } catch { return [] }
  }

  const offered = useMemo(() => {
    const t = Object.entries(tools).filter(([, ok]) => ok).map(([k]) => k)
    t.push('bookmark')
    if (navigator.mediaDevices && 'MediaRecorder' in window) t.push('voice')
    t.push('camera')
    return Array.from(new Set(t))
  }, [tools])

  // ---- conflict ----

  const takeTheirs = () => {
    if (!conflict) return
    const c = cur.current
    c.etag = conflict.etag; c.saved = conflict.content || ''; c.draft = c.saved
    setNote(conflict); setDraft(c.saved); setRevision(r => r + 1); setConflict(null); setStatus('saved')
  }
  const keepMine = async () => {
    if (!conflict) return
    cur.current.etag = conflict.etag
    setConflict(null)
    await save()
  }
  const mineAsCopy = async () => {
    if (!note) return
    const mine = cur.current.draft
    try {
      const copy = await api.post<Note>(`${V1}/notes`, { title: `${note.title} (my version)`, category: note.category, content: mine })
      takeTheirs(); bump()
      toast('Your version is kept as a new note')
      navigate(`/note/${copy.id}${location.search}`)
    } catch (e) { toast((e as Error).message, 'err') }
  }

  if (err) return <div className="note-empty"><div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div>{onBack && <button type="button" className="btn mt" onClick={onBack}><Icon name="chevron-left" size={16} />Back</button>}</div>
  if (!note) return <div className="note-empty"><Loading /></div>

  const menuItems = (): MenuItem[] => [
    ...(phone() ? [
      { label: note.favorite ? 'Remove from favourites' : 'Favourite', icon: 'star', onSelect: () => patch({ favorite: !note.favorite }) },
      { label: note.pinned ? 'Unpin' : 'Pin to the top', icon: 'pushpin', onSelect: () => patch({ pinned: !note.pinned }) },
      { label: 'Outline, links and info', icon: 'panel-right', onSelect: () => setPanel('outline') },
      'sep' as const,
    ] : []),
    { label: 'History…', icon: 'history', onSelect: () => setSheet({ kind: 'history' }) },
    { label: 'Share a link…', icon: 'share', onSelect: () => setSheet({ kind: 'share' }) },
    { label: 'Move to…', icon: 'move', onSelect: () => setSheet({ kind: 'move' }) },
    { label: 'Duplicate', icon: 'copy', onSelect: async () => { try { const n = await api.post<Note>(`${V1}/notes/${note.id}/duplicate`); bump(); navigate(`/note/${n.id}${location.search}`) } catch (e) { toast((e as Error).message, 'err') } } },
    'sep',
    { label: 'Copy link', icon: 'link', onSelect: async () => { const ok = await copyText(noteURL(note.id)); toast(ok ? 'Link copied' : 'Could not copy the link', ok ? 'ok' : 'err') } },
    { label: 'Download .md', icon: 'download', onSelect: () => { location.href = `${V1}/notes/${note.id}/download` } },
    { label: 'Print / PDF', icon: 'printer', onSelect: () => doExport('pdf') },
    { label: 'Export HTML', icon: 'code', onSelect: () => doExport('html') },
    'sep',
    { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setSheet({ kind: 'delete' }) },
  ]

  const reading = readPref && !blank && !note.readonly
  const toggleReading = () => {
    const p = rich.current?.flush(); if (p != null) changed(p)
    if (reading) setReadPref(false); else { setReadPref(true); setBlank(false) }
  }
  const locked = note.readonly || reading

  const outline = headings(draft)
  const words = wordCount(draft)
  const statusText: Record<Status, string> = { saved: 'Saved', dirty: 'Editing…', saving: 'Saving…', error: 'Not saved — retrying', conflict: 'Changed elsewhere', offline: 'Offline — retrying' }

  return (
    <div className={`note-view ${panel ? 'with-panel' : ''} ${embedded ? 'embedded' : ''} ${reading ? 'reading' : ''}`}>
      <header className="note-head">
        {onBack && <button type="button" className="btn ghost icon-only back-btn" onClick={onBack} aria-label="Back to the list"><Icon name="chevron-left" size={20} /></button>}
        <div className="note-title-wrap">
          <div className="note-crumbs">
            <Link to={`/?in=${encodeURIComponent(note.category)}`}><Icon name="folder" size={12} />{note.category ? note.category.split('/').join(' › ') : 'Notes'}</Link>
          </div>
          <input className="note-title" value={title} onChange={e => setTitle(e.target.value)} onBlur={rename} onKeyDown={e => { if (e.key === 'Enter') { (e.target as HTMLInputElement).blur() } if (e.key === 'Escape') { setTitle(note.title); (e.target as HTMLInputElement).blur() } }} readOnly={locked} aria-label="Title" />
        </div>
        <div className="note-actions">
          <span className={`save-state ${status}`} title={stamp(note.modified)}>{statusText[status]}</span>
          <button type="button" className={`btn ghost icon-only wide-only ${note.favorite ? 'on star' : ''}`} title={note.favorite ? 'Remove from favourites' : 'Favourite'} aria-label={note.favorite ? 'Remove from favourites' : 'Favourite'} aria-pressed={note.favorite} onClick={() => patch({ favorite: !note.favorite })}><Icon name="star" size={17} /></button>
          <button type="button" className={`btn ghost icon-only wide-only ${note.pinned ? 'on' : ''}`} title={note.pinned ? 'Unpin' : 'Pin to the top'} aria-label={note.pinned ? 'Unpin' : 'Pin to the top'} aria-pressed={note.pinned} onClick={() => patch({ pinned: !note.pinned })}><Icon name="pushpin" size={17} /></button>
          {!note.readonly && <button type="button" className={`btn ghost icon-only reader-btn ${reading ? 'on' : ''}`} title={reading ? 'Back to editing' : 'Reader mode'} aria-label="Reader mode" aria-pressed={reading} onClick={toggleReading}><Icon name="book-open" size={17} /></button>}
          <Segmented size="sm" className="mode-toggle" label="Editor" value={mode} onChange={m => { const p = rich.current?.flush(); if (p != null) changed(p); setMode(m); setRevision(r => r + 1) }}
            options={[{ value: 'rich' as const, label: 'Aa', title: 'Rich text' }, { value: 'markdown' as const, label: 'M↓', title: 'Markdown source' }]} />
          <button type="button" className={`btn ghost icon-only wide-only ${panel ? 'on' : ''}`} title="Outline, links and info" aria-label="Outline, links and info" aria-pressed={!!panel} onClick={() => setPanel(p => p ? '' : 'outline')}><Icon name="panel-right" size={17} /></button>
          {phone() && !EMBEDDED && <AskButton />}
          <MenuButton label="More" align="right" width={220} items={menuItems}><Icon name="more" size={18} /></MenuButton>
        </div>
      </header>

      {note.tags.length > 0 && <div className="note-tags">{note.tags.map(t => <Link key={t} to={`/?tag=${encodeURIComponent(t)}`} className="tag">#{t}</Link>)}</div>}

      {conflict && <div className="callout warning conflict">
        <Icon name="alert" size={15} />
        <div className="grow">
          <b>This note was changed somewhere else</b> (another device or app) while you were writing here.
          <div className="row wrap mt-s">
            <button type="button" className="btn sm" onClick={takeTheirs}>Show theirs (drop mine)</button>
            <button type="button" className="btn sm" onClick={mineAsCopy}>Keep mine as a copy</button>
            <button type="button" className="btn sm danger" onClick={keepMine}>Overwrite with mine</button>
          </div>
        </div>
      </div>}

      {mode === 'markdown' && !locked && <MdToolbar ed={md} onTool={t => runTool(t, currentLine())} onAttach={s => onMessage({ name: 'attach', source: s })} onTemplates={() => setSheet({ kind: 'templates' })} tools={offered} />}

      <div className="note-body">
        <div className="editor-pane">
          {mode === 'rich'
            ? <RichEditor ref={rich} docKey={String(note.id)} revision={revision} content={draft} noteID={note.id} readonly={locked} reader={reading} checkable tools={offered} theme={theme}
                spellcheck={settings?.spellcheck} placeholder="Write, or type / for blocks and homelab tools" onChange={changed} onMessage={onMessage} onRequest={onRequest}
                onFailed={msg => { toast(`The rich editor could not open this note (${msg}); showing Markdown.`, 'err'); setMode('markdown') }} />
            : <MarkdownEditor ref={md} docKey={String(note.id)} revision={revision} content={draft} readonly={locked} spellcheck={settings?.spellcheck} onChange={changed} onPasteFiles={uploadFiles} onOpenLink={u => openLink(u)} onPeople={findPeople} />}
        </div>
        {panel && <div className="panel-scrim" onClick={() => setPanel('')} />}
        {panel && <aside className="note-panel">
          <div className="panel-head">
            <Segmented size="sm" label="Panel" value={panel} onChange={setPanel} options={[{ value: 'outline' as const, label: 'Outline' }, { value: 'links' as const, label: 'Links' }, { value: 'info' as const, label: 'Info' }]} />
            <button type="button" className="btn ghost icon-only sm panel-close" onClick={() => setPanel('')} aria-label="Close"><Icon name="x" size={16} /></button>
          </div>
          {panel === 'outline' && (outline.length ? <ul className="outline">{outline.map((h, i) => <li key={i} style={{ paddingLeft: (h.level - 1) * 12 }}><button type="button" onClick={() => { if (phone()) setPanel(''); if (mode === 'rich') rich.current?.scrollToHeading(h.text, outline.slice(0, i).filter(x => x.text === h.text).length); else md.current?.scrollToLine(h.line) }}>{h.text}</button></li>)}</ul> : <p className="muted small">No headings in this note.</p>)}
          {panel === 'links' && <Backlinks id={note.id} etag={note.etag} />}
          {panel === 'info' && <Info note={note} words={words} chars={draft.length} />}
        </aside>}
      </div>

      <footer className="note-foot muted small">
        <span>{words} words</span><span>·</span><span>{Math.max(1, Math.round(words / 220))} min read</span>
        {note.tasks_open + note.tasks_done > 0 && <><span>·</span><span>{note.tasks_done}/{note.tasks_open + note.tasks_done} tasks</span></>}
        <span className="spacer" />
        <span title={stamp(note.modified)}>Edited {ago(note.modified)}</span>
      </footer>

      <input ref={fileInput} type="file" multiple hidden onChange={async e => { const f = Array.from(e.target.files || []); e.target.value = ''; for (const m of await uploadFiles(f)) put(m, 'block') }} />
      <input ref={cameraInput} type="file" accept="image/*" capture="environment" hidden onChange={async e => { const f = Array.from(e.target.files || []); e.target.value = ''; for (const m of await uploadFiles(f)) put(m, 'block') }} />

      {sheet?.kind === 'link' && <LinkPicker onClose={() => setSheet(null)} onPick={m => { setSheet(null); put(m, 'inline') }} />}
      {sheet?.kind === 'bookmark' && <BookmarkSheet onClose={() => setSheet(null)} onPick={m => { setSheet(null); put(m, 'inline') }} />}
      {sheet?.kind === 'photos' && <PhotoPicker noteID={note.id} onClose={() => setSheet(null)} onPick={m => { setSheet(null); put(m, 'block') }} />}
      {sheet?.kind === 'files' && <FilesPicker noteID={note.id} onClose={() => setSheet(null)} onPick={m => { setSheet(null); put(m, 'block') }} />}
      {sheet?.kind === 'task' && <TaskSheet line={sheet.line?.text || ''} onClose={() => setSheet(null)} onDone={m => { const l = sheet.line; setSheet(null); replaceLine(l, m); toast('Sent') }} />}
      {sheet?.kind === 'expense' && <ExpenseSheet line={sheet.line?.text || ''} onClose={() => setSheet(null)} onDone={m => { const l = sheet.line; setSheet(null); replaceLine(l, m); toast('Booked') }} />}
      {sheet?.kind === 'habits' && <HabitsSheet onClose={() => setSheet(null)} onDone={m => { setSheet(null); put(m, 'block') }} />}
      {sheet?.kind === 'voice' && <VoiceMemo upload={async f => (await uploadFiles([f]))[0] || ''} onClose={() => setSheet(null)} onDone={m => { setSheet(null); put(m, 'block') }} />}
      {sheet?.kind === 'templates' && <TemplatePicker title={note.title} onClose={() => setSheet(null)} onPick={m => { setSheet(null); if (mode === 'rich') rich.current?.insertTemplate(m); else put(m, 'block') }} />}
      {sheet?.kind === 'diagram' && <DiagramViewer svg={sheet.svg} onClose={() => setSheet(null)} />}
      {sheet?.kind === 'history' && <HistoryModal note={note} onClose={() => setSheet(null)} onRestored={n => { const c = cur.current; c.etag = n.etag; c.saved = n.content || ''; c.draft = c.saved; setNote(n); setDraft(c.saved); setRevision(r => r + 1); setSheet(null); toast('Restored') }} />}
      {sheet?.kind === 'share' && <ShareModal note={note} onClose={() => setSheet(null)} />}
      {sheet?.kind === 'move' && <MoveModal current={note.category} onClose={() => setSheet(null)} onMove={async cat => { setSheet(null); await patch({ category: cat }, `Moved to ${cat || 'Notes'}`) }} />}
      {sheet?.kind === 'delete' && <Confirm title={`Delete “${note.title}”?`} message={<>It goes to the trash (Trash in the sidebar) for {settings?.trash_days ?? 30} days{settings?.storage_kind === 'nextcloud' ? ', and to Nextcloud’s own trash bin' : ''}.</>} onCancel={() => setSheet(null)} onConfirm={async () => { setSheet(null); await remove() }} />}
    </div>
  )
})

export default NoteView

/** The one-pane layout (styles/notes.css: max-width 767px). */
function phone() { return window.matchMedia('(max-width: 767px)').matches }

function MdToolbar({ ed, onTool, onAttach, onTemplates, tools }: { ed: React.RefObject<MarkdownEditorHandle | null>; onTool: (t: string) => void; onAttach: (s: string) => void; onTemplates: () => void; tools: string[] }) {
  const b = (label: React.ReactNode, title: string, fn: () => void) => <button type="button" className="btn ghost sm" title={title} aria-label={title} onMouseDown={e => e.preventDefault()} onClick={fn}>{label}</button>
  const i = (name: string) => <Icon name={name} size={15} />
  const items: [string, string, string][] = [['link', 'Link to something…', 'link'], ['bookmark', 'Web bookmark…', 'bookmark'], ['task', 'Make this line a task…', 'check-square'], ['expense', 'Make this line a transaction…', 'wallet'], ['snapshot', 'Homelab snapshot', 'server'], ['attention', 'What needs attention', 'alert'], ['media', 'Watched and listened this week', 'film'], ['spent', 'Spending this month', 'receipt'], ['fuel', 'Vehicle log', 'car'], ['hosts', 'Hosts on the network', 'globe'], ['processes', 'Server processes', 'activity'], ['habits', 'Days of habits…', 'habit'], ['voice', 'Voice memo…', 'mic'], ['camera', 'Take a photo…', 'camera']]
  const menu = (): MenuItem[] => { const l = items.filter(([k]) => tools.includes(k)); return l.length ? l.map(([k, label, icon]) => ({ label, icon, onSelect: () => onTool(k) })) : [{ label: 'Nothing is connected yet', disabled: true }] }
  return (
    <div className="md-toolbar">
      <div className="md-tools">
      {b(<b>B</b>, 'Bold (⌘B)', () => ed.current?.wrap('**'))}
      {b(<i>I</i>, 'Italic (⌘I)', () => ed.current?.wrap('*'))}
      {b(<s>S</s>, 'Strikethrough', () => ed.current?.wrap('~~'))}
      {b(i('code'), 'Code', () => ed.current?.wrap('`'))}
      <span className="sep" />
      {b('H1', 'Heading 1', () => ed.current?.linePrefix('# '))}
      {b('H2', 'Heading 2', () => ed.current?.linePrefix('## '))}
      {b('H3', 'Heading 3', () => ed.current?.linePrefix('### '))}
      <span className="sep" />
      {b(i('list'), 'Bulleted list', () => ed.current?.linePrefix('- '))}
      {b(i('list-ordered'), 'Numbered list', () => ed.current?.linePrefix('1. '))}
      {b(i('check-square'), 'Task', () => ed.current?.linePrefix('- [ ] '))}
      {b(i('quote'), 'Quote', () => ed.current?.linePrefix('> '))}
      {b(i('link'), 'Link (⌘K)', () => ed.current?.wrap('[', '](https://)'))}
      {b(i('table'), 'Table', () => ed.current?.insert('| Column | Column |\n| --- | --- |\n|  |  |', true))}
      {b(<span className="mono">{'{ }'}</span>, 'Code block', () => ed.current?.insert('```\n\n```', true))}
      <span className="sep" />
      {b(i('paperclip'), 'Attach a file', () => onAttach('device'))}
      {b(i('image'), 'From Photos (Immich)', () => onAttach('immich'))}
      {b(i('cloud'), 'From Nextcloud', () => onAttach('nextcloud'))}
      {b(i('template'), 'My templates', onTemplates)}
      </div>
      <MenuButton label="Homelab tools" className="btn ghost sm" align="right" width={250} items={menu}><Icon name="server" size={15} />Homelab<Icon name="chevron-down" size={13} /></MenuButton>
    </div>
  )
}

function Backlinks({ id, etag }: { id: number; etag: string }) {
  const [data, setData] = useState<{ backlinks: Note[]; mentions: Note[] } | null>(null)
  useEffect(() => { api.get<{ backlinks: Note[]; mentions: Note[] }>(`${V1}/notes/${id}/backlinks`).then(setData).catch(() => setData({ backlinks: [], mentions: [] })) }, [id, etag])
  if (!data) return <Loading />
  return (
    <div className="links-panel">
      <h4 className="panel-h">Linked from</h4>
      {data.backlinks.length ? data.backlinks.map(n => <Link key={n.id} className="mini-note" to={`/note/${n.id}${location.search}`}><b>{n.title}</b><small className="muted">{n.category || 'Notes'} · {ago(n.modified)}</small></Link>) : <p className="muted small">No note links here yet. Type <code>[[</code> in another note to link to this one.</p>}
      {data.mentions.length > 0 && <><h4 className="panel-h mt">Mentioned in</h4>{data.mentions.map(n => <Link key={n.id} className="mini-note" to={`/note/${n.id}${location.search}`}><b>{n.title}</b><small className="muted">{n.category || 'Notes'}</small></Link>)}</>}
    </div>
  )
}

function Info({ note, words, chars }: { note: Note; words: number; chars: number }) {
  const { sync } = useApp()
  return (
    <table className="table compact info">
      <tbody>
        <tr><th>File</th><td className="mono small">{note.path}</td></tr>
        <tr><th>Folder</th><td>{note.category ? <Link to={`/?in=${encodeURIComponent(note.category)}`}>{leaf(note.category)}</Link> : 'Notes'}</td></tr>
        <tr><th>Stored</th><td>{sync?.where || '—'}</td></tr>
        <tr><th>Edited</th><td>{stamp(note.modified)}</td></tr>
        <tr><th>Created</th><td>{stamp(note.created_at)}</td></tr>
        <tr><th>Words</th><td>{words} · {chars} characters</td></tr>
        <tr><th>Tasks</th><td>{note.tasks_open} open · {note.tasks_done} done</td></tr>
        <tr><th>Tags</th><td>{note.tags.length ? note.tags.map(t => <Link key={t} to={`/?tag=${encodeURIComponent(t)}`} className="tag">#{t}</Link>) : '—'}</td></tr>
        <tr><th>Id</th><td className="mono small">{note.id}</td></tr>
      </tbody>
    </table>
  )
}
