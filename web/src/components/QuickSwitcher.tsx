import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1, qs } from '../lib/api'
import type { Note, SearchHit } from '../lib/types'
import { ago, snippetParts } from '../lib/format'
import { useDebounce } from './ui'

// ⌘K: notes by title as you type, then by what they say; a few commands too.
export function QuickSwitcher({ onClose, onNew, onToday }: { onClose: () => void; onNew: () => void; onToday: () => void }) {
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 120)
  const [titles, setTitles] = useState<Note[]>([])
  const [hits, setHits] = useState<SearchHit[]>([])
  const [sel, setSel] = useState(0)
  const navigate = useNavigate()

  useEffect(() => {
    let live = true
    const t = dq.trim()
    if (!t) { api.get<Note[]>(`${V1}/notes?sort=opened&limit=12`).then(n => live && (setTitles(n), setHits([]))).catch(() => {}); return () => { live = false } }
    Promise.all([
      api.get<Note[]>(`${V1}/suggest${qs({ q: t, limit: 8 })}`).catch(() => [] as Note[]),
      t.length > 1 ? api.get<SearchHit[]>(`${V1}/search${qs({ q: t, limit: 15 })}`).catch(() => [] as SearchHit[]) : Promise.resolve([] as SearchHit[]),
    ]).then(([a, b]) => { if (!live) return; setTitles(a); const seen = new Set(a.map(n => n.id)); setHits(b.filter(h => !seen.has(h.id))) })
    return () => { live = false }
  }, [dq])
  useEffect(() => setSel(0), [titles, hits])

  type Row = { key: string; run: () => void; title: string; sub?: React.ReactNode; ico: string }
  const commands: Row[] = [
    { key: 'cmd-new', run: onNew, title: 'New note', ico: '＋' },
    { key: 'cmd-today', run: onToday, title: 'Today', ico: '☀' },
    { key: 'cmd-tasks', run: () => { onClose(); navigate('/tasks') }, title: 'Tasks', ico: '☑' },
    { key: 'cmd-search', run: () => { onClose(); navigate(`/?q=${encodeURIComponent(q)}`) }, title: q.trim() ? `Search everything for “${q.trim()}”` : 'Search everything', ico: '⌕' },
  ].filter(c => !q.trim() || c.title.toLowerCase().includes(q.trim().toLowerCase()) || c.key === 'cmd-search')
  const open = (id: number) => { onClose(); navigate(`/note/${id}${location.search}`) }
  const rows: Row[] = [
    ...titles.map(n => ({ key: 't' + n.id, run: () => open(n.id), title: n.title, sub: <>{n.category || 'Notes'} · {ago(n.modified)}</>, ico: n.favorite ? '★' : '📝' })),
    ...hits.map(h => ({ key: 'h' + h.id, run: () => open(h.id), title: h.title, sub: snippetParts(h.snippet).map((p, i) => p.hit ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>), ico: '≡' })),
    ...commands,
  ]
  const key = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSel(s => Math.min(rows.length - 1, s + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel(s => Math.max(0, s - 1)) }
    else if (e.key === 'Enter') { e.preventDefault(); rows[sel]?.run() }
    else if (e.key === 'Escape') onClose()
  }
  return (
    <div className="modal-backdrop top" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="switcher" role="dialog" aria-label="Find a note">
        <input autoFocus value={q} onChange={e => setQ(e.target.value)} onKeyDown={key} placeholder="Find a note by title or by what it says…  (tag:x  in:Folder  is:fav  has:tasks)" />
        <div className="switcher-rows">
          {rows.map((r, i) => (
            <button key={r.key} className={`switcher-row ${i === sel ? 'sel' : ''}`} onMouseEnter={() => setSel(i)} onClick={r.run}>
              <span className="ico">{r.ico}</span>
              <span className="grow"><b>{r.title}</b>{r.sub && <small className="muted">{r.sub}</small>}</span>
            </button>
          ))}
        </div>
        <div className="switcher-foot muted small"><span>↑↓ choose · ↵ open · esc close</span><span>⌘K anywhere</span></div>
      </div>
    </div>
  )
}
