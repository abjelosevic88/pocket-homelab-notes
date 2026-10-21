// ⌘K / "/" — notes by title as you type, then by what they say; folders, tags and commands too.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, V1, qs } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note, SearchHit } from '../lib/types'
import { ago, snippetParts } from '../lib/format'
import { Icon } from './icons'
import { useDebounce } from './ui'

type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }

export function CommandPalette({ onClose, onNew, onShortcuts }: { onClose: () => void; onNew: () => void; onShortcuts: () => void }) {
  const { folders, tags, themeMode, setThemeMode } = useApp()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 120)
  const [titles, setTitles] = useState<Note[]>([])
  const [hits, setHits] = useState<SearchHit[]>([])
  const [hi, setHi] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let live = true
    const t = dq.trim()
    if (!t) { api.get<Note[]>(`${V1}/notes?sort=opened&limit=8`).then(n => { if (live) { setTitles(n); setHits([]) } }).catch(() => {}); return () => { live = false } }
    Promise.all([
      api.get<Note[]>(`${V1}/suggest${qs({ q: t, limit: 8 })}`).catch(() => [] as Note[]),
      t.length > 1 ? api.get<SearchHit[]>(`${V1}/search${qs({ q: t, limit: 15 })}`).catch(() => [] as SearchHit[]) : Promise.resolve([] as SearchHit[]),
    ]).then(([a, b]) => { if (!live) return; setTitles(a); const seen = new Set(a.map(n => n.id)); setHits(b.filter(h => !seen.has(h.id))) })
    return () => { live = false }
  }, [dq])

  const rows = useMemo<Row[]>(() => {
    const n = q.trim().toLowerCase()
    const words = n.split(/\s+/).filter(Boolean)
    const hit = (text: string) => words.every(w => text.toLowerCase().includes(w))
    const open = (id: number) => navigate(`/note/${id}${location.search}`)
    const out: Row[] = []
    for (const t of titles) out.push({ key: 't' + t.id, group: n ? 'Notes' : 'Recently opened', icon: <Icon name={t.favorite ? 'star' : 'note'} size={16} className={t.favorite ? 'star' : ''} />, label: t.title || 'Untitled', sub: <>{t.category || 'Notes'} · {ago(t.modified)}</>, run: () => open(t.id) })
    for (const h of hits) out.push({ key: 'h' + h.id, group: 'In what notes say', icon: <Icon name="text" size={16} />, label: h.title || 'Untitled', sub: snippetParts(h.snippet).map((p, i) => p.hit ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>), run: () => open(h.id) })
    if (n) {
      for (const f of folders.filter(f => f.path && hit(f.path)).slice(0, 5)) out.push({ key: 'f' + f.path, group: 'Folders', icon: <Icon name="folder" size={16} />, label: f.path.split('/').pop(), sub: f.path.includes('/') ? f.path : undefined, run: () => navigate(`/?in=${encodeURIComponent(f.path)}`) })
      for (const t of tags.filter(t => hit(t.tag)).slice(0, 5)) out.push({ key: 'g' + t.tag, group: 'Tags', icon: <Icon name="hash" size={16} />, label: `#${t.tag}`, sub: `${t.count} note${t.count === 1 ? '' : 's'}`, run: () => navigate(`/?tag=${encodeURIComponent(t.tag)}`) })
    }
    const commands: (Omit<Row, 'group' | 'icon'> & { icon: string; keywords?: string })[] = [
      { key: 'search', icon: 'search', label: n ? `Search everything for “${q.trim()}”` : 'Search everything', run: () => navigate(`/?q=${encodeURIComponent(q.trim())}`) },
      { key: 'new', icon: 'plus', label: 'New note', hint: 'N', keywords: 'create make write', run: onNew },
      { key: 'today', icon: 'today', label: 'Today', hint: 'T', keywords: 'daily journal capture', run: () => navigate('/today') },
      { key: 'all', icon: 'notes', label: 'All notes', hint: 'A', run: () => navigate('/') },
      { key: 'recent', icon: 'clock', label: 'Recent', run: () => navigate('/?view=recent') },
      { key: 'fav', icon: 'star', label: 'Favourites', keywords: 'starred', run: () => navigate('/?view=favorites') },
      { key: 'tasks', icon: 'check-square', label: 'Tasks', hint: 'X', keywords: 'todo checkbox due', run: () => navigate('/tasks') },
      { key: 'calendar', icon: 'calendar', label: 'Calendar', hint: 'C', keywords: 'daily month', run: () => navigate('/calendar') },
      { key: 'trash', icon: 'trash', label: 'Trash', keywords: 'deleted restore', run: () => navigate('/trash') },
      { key: 'settings', icon: 'settings', label: 'Settings', hint: ',', keywords: 'preferences', run: () => navigate('/settings') },
      { key: 'storage', icon: 'database', label: 'Settings: Storage', keywords: 'nextcloud webdav folder files sync', run: () => navigate('/settings/storage') },
      { key: 'integrations', icon: 'layers', label: 'Settings: Integrations', keywords: 'homelab tools pocket core connect', run: () => navigate('/settings/integrations') },
      { key: 'theme', icon: themeMode === 'dark' ? 'sun' : 'moon', label: themeMode === 'dark' ? 'Light appearance' : 'Dark appearance', keywords: 'theme dark light mode', run: () => setThemeMode(themeMode === 'dark' ? 'light' : 'dark') },
      { key: 'shortcuts', icon: 'keyboard', label: 'Keyboard shortcuts', hint: '?', run: onShortcuts },
    ]
    for (const c of commands) {
      if (n && c.key !== 'search' && !hit(`${String(c.label)} ${c.keywords || ''}`)) continue
      if (!n && c.key === 'search') continue
      out.push({ ...c, group: 'Commands', icon: <Icon name={c.icon} size={16} /> })
    }
    return out
  }, [q, titles, hits, folders, tags, themeMode, setThemeMode, navigate, onNew, onShortcuts])
  useEffect(() => setHi(0), [titles, hits, q])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Find a note">
        <div className="pal-input">
          <Icon name="search" size={18} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a note by title or by what it says…" aria-label="Find a note"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {rows.length === 0 && <div className="pal-empty">Nothing matches.</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow" style={{ minWidth: 0 }}><span className="pal-label ellipsis">{r.label}</span>{r.sub && <span className="pal-sub">{r.sub}</span>}</span>
                  {r.hint && <kbd>{r.hint}</kbd>}
                  {i === hi && <Icon name="corner-down-left" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
        <div className="pal-foot"><span><kbd>↑</kbd><kbd>↓</kbd> choose <kbd>↵</kbd> open</span><span>tag:x · in:Folder · is:fav · has:tasks · "a phrase"</span></div>
      </div>
    </div>, document.body)
}
