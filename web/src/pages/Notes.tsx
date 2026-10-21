import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1, qs } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note, SearchHit } from '../lib/types'
import { ago, leaf, snippetParts, stamp } from '../lib/format'
import { Empty, Loading, Switch, useDebounce, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import NoteView from '../components/NoteView'
import { newNoteIn } from '../components/Layout'
import { NoteMenu } from '../components/NoteMenu'

// All notes, a folder, a tag, favourites, recent or a search, with the note on the right.
export default function Notes() {
  const { id } = useParams()
  const [params, setParams] = useSearchParams()
  const { settings, version, bump } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const folder = params.get('in')
  const tag = params.get('tag')
  const view = params.get('view')
  const [q, setQ] = useState(params.get('q') || '')
  const dq = useDebounce(q, 250)
  const [sort, setSort] = useState<string>(() => localStorage.getItem('pn-sort') || settings?.sort_by || 'modified')
  const [notes, setNotes] = useState<(Note | SearchHit)[] | null>(null)
  const [err, setErr] = useState('')
  const [sub, setSub] = useState(() => localStorage.getItem('pn-subfolders') !== '0')
  const [menu, setMenu] = useState<{ note: Note; x: number; y: number } | null>(null)
  useEffect(() => { localStorage.setItem('pn-sort', sort) }, [sort])
  useEffect(() => { localStorage.setItem('pn-subfolders', sub ? '1' : '0') }, [sub])
  useEffect(() => { setQ(params.get('q') || '') }, [params])

  const heading = q.trim() ? `“${q.trim()}”` : view === 'favorites' ? 'Favourites' : view === 'recent' ? 'Recent' : tag ? `#${tag}` : folder != null ? (folder ? leaf(folder) : 'Notes (top level)') : 'All notes'

  useEffect(() => {
    let live = true
    setErr('')
    const scope = [folder != null && folder !== '' ? `in:"${folder}"` : '', tag ? `tag:${tag}` : '', view === 'favorites' ? 'is:fav' : ''].filter(Boolean).join(' ')
    const run = dq.trim()
      ? api.get<SearchHit[]>(`${V1}/search${qs({ q: `${scope} ${dq.trim()}`.trim(), limit: 200 })}`)
      : api.get<Note[]>(`${V1}/notes${qs({
          category: folder ?? undefined, recursive: folder != null && sub ? 1 : undefined, tag: tag ?? undefined,
          favorite: view === 'favorites' ? 1 : undefined, sort: view === 'recent' ? 'opened' : sort, limit: view === 'recent' ? 50 : undefined,
        })}`)
    run.then(n => live && setNotes(n)).catch(e => live && setErr((e as Error).message))
    return () => { live = false }
  }, [folder, tag, view, dq, sort, sub, version])

  // Coming back to the window: the list may have changed elsewhere.
  useEffect(() => {
    const h = () => bump()
    window.addEventListener('focus', h)
    return () => window.removeEventListener('focus', h)
  }, [bump])

  const selected = id ? Number(id) : null
  const search = (v: string) => {
    setQ(v)
    const p = new URLSearchParams(params)
    if (v) p.set('q', v); else p.delete('q')
    setParams(p, { replace: true })
  }
  const back = () => navigate(`/${params.toString() ? '?' + params.toString() : ''}`)
  const onMenu = (n: Note, e: React.MouseEvent) => { e.preventDefault(); setMenu({ note: n, x: e.clientX, y: e.clientY }) }
  const pinned = useMemo(() => (notes || []).filter(n => n.pinned && !q.trim()), [notes, q])
  const rest = useMemo(() => (notes || []).filter(n => !(n.pinned && !q.trim())), [notes, q])

  return (
    <div className={`notes-page ${selected ? 'has-note' : ''}`}>
      <section className="note-list">
        <div className="list-head">
          <div className="row">
            <div className="tool-search grow" title={folder || undefined}><Icon name="search" size={15} /><input value={q} onChange={e => search(e.target.value)} placeholder={folder || tag ? `Search in ${heading}…` : 'Search all notes…'} aria-label="Search notes" />{q && <button type="button" className="btn ghost icon-only xs" onClick={() => search('')} aria-label="Clear the search"><Icon name="x" size={14} /></button>}</div>
            <button type="button" className="btn primary icon-only list-new" onClick={() => newNoteIn(folder ?? settings?.default_folder ?? '', navigate, bump, toast)} title={folder ? `New note in ${heading}` : 'New note'} aria-label="New note here"><Icon name="plus" size={17} strokeWidth={2.2} /></button>
          </div>
          <div className="row list-opts">
            <span>{notes ? `${notes.length} note${notes.length === 1 ? '' : 's'}` : ''}</span>
            <span className="spacer" />
            {folder && <label className="check-row" title="Show notes in folders inside this one"><Switch size="sm" checked={sub} onChange={setSub} label="Subfolders" />Subfolders</label>}
            {!q.trim() && view !== 'recent' && <Select size="sm" className="ghost inline" label="Sort" value={sort} onChange={v => setSort(v)} width={160} options={[{ value: 'modified', label: 'Edited' }, { value: 'created', label: 'Created' }, { value: 'title', label: 'Title' }]} />}
          </div>
        </div>
        <div className="list-scroll">
          {err && <div className="callout danger m"><Icon name="alert" size={15} /><span>{err}</span></div>}
          {!notes && !err && <Loading />}
          {notes && notes.length === 0 && <Empty icon={q ? 'search' : 'note'} title={q ? 'Nothing found' : 'No notes here yet'}>{q ? 'Try fewer words, or tag:, in:, is:fav, has:tasks.' : <>Make one with <b>New note</b> or press <kbd>N</kbd>.</>}</Empty>}
          {pinned.length > 0 && <div className="list-group">Pinned</div>}
          {pinned.map(n => <Row key={n.id} n={n} active={n.id === selected} menu={menu?.note.id === n.id} onMenu={onMenu} params={params} showFolder={folder == null} />)}
          {pinned.length > 0 && rest.length > 0 && <div className="list-group">Notes</div>}
          {rest.map(n => <Row key={n.id} n={n} active={n.id === selected} menu={menu?.note.id === n.id} onMenu={onMenu} params={params} showFolder={folder == null || (sub && n.category !== folder)} />)}
        </div>
      </section>
      <section className="note-pane">
        {selected ? <NoteView id={selected} onBack={back} /> : <Welcome />}
      </section>
      {menu && <NoteMenu note={menu.note} x={menu.x} y={menu.y} onClose={() => setMenu(null)} />}
    </div>
  )
}

function Row({ n, active, menu, onMenu, params, showFolder }: { n: Note | SearchHit; active: boolean; menu: boolean; onMenu: (n: Note, e: React.MouseEvent) => void; params: URLSearchParams; showFolder: boolean }) {
  const snippet = 'snippet' in n && n.snippet && n.snippet !== n.excerpt ? n.snippet : ''
  return (
    <Link to={`/note/${n.id}${params.toString() ? '?' + params.toString() : ''}`} className={`note-row ${active ? 'active' : ''} ${menu ? 'menu-on' : ''}`} onContextMenu={e => onMenu(n, e)}>
      <div className="row">
        <b className="grow ellipsis">{n.title || 'Untitled'}</b>
        {n.pinned && <Icon name="pushpin" size={13} title="Pinned" />}
        {n.favorite && <Icon name="star" size={13} className="star" title="Favourite" />}
      </div>
      <div className="excerpt">{snippet ? snippetParts(snippet).map((p, i) => p.hit ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>) : n.excerpt || <span className="muted">Empty note</span>}</div>
      <div className="row meta small muted">
        <span title={stamp(n.modified)}>{ago(n.modified)}</span>
        {showFolder && n.category && <span className="folder-chip">{leaf(n.category)}</span>}
        {n.tasks_open > 0 && <span title="Open tasks"><Icon name="square" size={11} />{n.tasks_open}</span>}
        {n.tags.slice(0, 3).map(t => <span key={t} className="tag-chip">#{t}</span>)}
      </div>
    </Link>
  )
}

function Welcome() {
  const { sync } = useApp()
  const [d, setD] = useState<{ notes: number; edited_today: number; open_tasks: number; due_tasks: number } | null>(null)
  useEffect(() => { api.get<{ notes: number; edited_today: number; open_tasks: number; due_tasks: number }>(`${V1}/dashboard`).then(setD).catch(() => {}) }, [])
  return (
    <div className="welcome">
      <img src="/favicon.svg" alt="" width={64} height={64} />
      <h2>Pocket Notes</h2>
      {sync?.where && <p className="muted small">Kept in {sync.where}</p>}
      {d && <div className="welcome-stats">
        <div className="welcome-stat"><b>{d.notes}</b><span>notes</span></div>
        <div className="welcome-stat"><b>{d.edited_today}</b><span>edited today</span></div>
        <Link className="welcome-stat" to="/tasks"><b>{d.open_tasks}</b><span>open tasks{d.due_tasks ? ` · ${d.due_tasks} due` : ''}</span></Link>
      </div>}
      <div className="kbd-help">
        <span><kbd>⌘K</kbd> find</span><span><kbd>N</kbd> new note</span><span><kbd>T</kbd> today</span><span><kbd>/</kbd> in a note: blocks & homelab tools</span><span><kbd>[[</kbd> link a note</span><span><kbd>?</kbd> all shortcuts</span>
      </div>
    </div>
  )
}
