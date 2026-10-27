// Left column: new note, capture, the views, folders, tags, trash/settings and the storage's
// state — the Pocket Calendar sidebar.
import { useEffect, useMemo, useState } from 'react'
import { Link, NavLink, useLocation, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { ago } from '../lib/format'
import { Icon } from './icons'
import { type Anchor, rectOf, useToast } from './ui'

interface TreeNode { path: string; name: string; count: number; total: number; children: TreeNode[] }

function buildTree(folders: { path: string; count: number }[]): TreeNode[] {
  const root: TreeNode = { path: '', name: '', count: 0, total: 0, children: [] }
  const byPath = new Map<string, TreeNode>([['', root]])
  for (const f of [...folders].sort((a, b) => a.path.localeCompare(b.path))) {
    if (!f.path) { root.count = f.count; continue }
    const parts = f.path.split('/')
    let parent = root
    parts.forEach((_, i) => {
      const p = parts.slice(0, i + 1).join('/')
      let n = byPath.get(p)
      if (!n) { n = { path: p, name: parts[i], count: 0, total: 0, children: [] }; byPath.set(p, n); parent.children.push(n) }
      parent = n
    })
    parent.count = f.count
  }
  const sum = (n: TreeNode): number => (n.total = n.count + n.children.reduce((s, c) => s + sum(c), 0))
  sum(root)
  return root.children
}

export interface Dashboard { notes: number; edited_today: number; open_tasks: number; due_tasks: number }

/** The counts beside the views: they follow every change. */
export function useDashboard() {
  const { version } = useApp()
  const [d, setD] = useState<Dashboard | null>(null)
  useEffect(() => {
    let live = true
    api.get<Dashboard>(`${V1}/dashboard`).then(x => { if (live) setD(x) }).catch(() => {})
    return () => { live = false }
  }, [version])
  return d
}

export function Sidebar({ onNew, onNavigate, onFolderMenu, onNewFolder, onSync, syncing }: {
  onNew: () => void
  onNavigate: () => void
  onFolderMenu: (path: string, at: Anchor) => void
  onNewFolder: () => void
  onSync: () => void
  syncing: boolean
}) {
  const { settings, status, folders, tags, sync, bump } = useApp()
  const { toast } = useToast()
  const loc = useLocation()
  const [params] = useSearchParams()
  const [capture, setCapture] = useState('')
  const [foldersOpen, setFoldersOpen] = useState(() => localStorage.getItem('pn-folders') !== '0')
  const [tagsOpen, setTagsOpen] = useState(() => localStorage.getItem('pn-tags') !== '0')
  useEffect(() => { localStorage.setItem('pn-tags', tagsOpen ? '1' : '0') }, [tagsOpen])
  useEffect(() => { localStorage.setItem('pn-folders', foldersOpen ? '1' : '0') }, [foldersOpen])
  const dash = useDashboard()
  // The pictures folder holds no notes: it stays out of the tree unless it does.
  const tree = useMemo(() => buildTree(folders.filter(f => f.count > 0 || !settings?.attachments_folder || (f.path !== settings.attachments_folder && !f.path.startsWith(settings.attachments_folder + '/')))), [folders, settings?.attachments_folder])
  const here = params.get('in')
  const title = settings?.title || status?.title || 'Pocket Notes'
  const onNotes = loc.pathname === '/' || loc.pathname.startsWith('/note/')
  const view = params.get('view')

  const doCapture = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!capture.trim()) return
    try { await api.post(`${V1}/capture`, { text: capture }); setCapture(''); bump(); toast('Added to today’s note') } catch (err) { toast((err as Error).message, 'err') }
  }

  const syncKind = sync?.error ? 'bad' : sync?.syncing || syncing ? 'busy' : 'ok'
  const nav: { to: string; label: string; icon: string; active: boolean; count?: number; badge?: number; title?: string }[] = [
    { to: '/today', label: 'Today', icon: 'today', active: loc.pathname === '/today', title: 'Today (t)' },
    { to: '/', label: 'All notes', icon: 'notes', active: onNotes && !params.toString(), count: dash?.notes, title: 'All notes (a)' },
    { to: '/?view=recent', label: 'Recent', icon: 'clock', active: onNotes && view === 'recent' },
    { to: '/?view=favorites', label: 'Favourites', icon: 'star', active: onNotes && view === 'favorites' },
    { to: '/tasks', label: 'Tasks', icon: 'check-square', active: loc.pathname === '/tasks', badge: dash?.due_tasks, count: dash?.open_tasks, title: 'Tasks (x)' },
    { to: '/calendar', label: 'Calendar', icon: 'calendar', active: loc.pathname === '/calendar', title: 'Calendar (c)' },
  ]

  return (
    <aside className="sidebar" aria-label="Notes">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Notes' ? title : <>Pocket <b>Notes</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={() => { onNavigate(); onNew() }} title="New note (n or Alt+N)">
        <Icon name="plus" size={17} strokeWidth={2.2} />New note<kbd className="kbd-on-primary">N</kbd>
      </button>
      <form className="sb-capture" onSubmit={doCapture}>
        <Icon name="zap" size={14} />
        <input value={capture} onChange={e => setCapture(e.target.value)} placeholder="Capture to today…" aria-label="Quick capture to today's note" enterKeyHint="send" />
      </form>

      <nav className="sb-nav" aria-label="Views">
        {nav.map(n => (
          <Link key={n.to} to={n.to} onClick={onNavigate} className={`sb-nav-item ${n.active ? 'active' : ''}`} aria-current={n.active ? 'page' : undefined} title={n.title}>
            <Icon name={n.icon} size={16} />{n.label}
            {n.badge ? <span className="badge danger" title={`${n.badge} due or overdue`}>{n.badge}</span> : n.count != null && n.count > 0 ? <span className="sb-count">{n.count}</span> : null}
          </Link>
        ))}
      </nav>

      <section className="sb-sec">
        <h4 className="sb-h">
          <button type="button" className="sb-h-btn" aria-expanded={foldersOpen} onClick={() => setFoldersOpen(o => !o)}><Icon name={foldersOpen ? 'chevron-down' : 'chevron-right'} size={13} />Folders</button>
          <button type="button" className="btn ghost icon-only xs" aria-label="New folder" title="New folder" onClick={onNewFolder}><Icon name="plus" size={14} /></button>
        </h4>
        {foldersOpen && (tree.length === 0
          ? <p className="sb-empty">No folders yet. <button type="button" className="link-btn" onClick={onNewFolder}>Make one</button></p>
          : <ul className="tree">{tree.map(n => <Branch key={n.path} node={n} here={here} onNavigate={onNavigate} onMenu={onFolderMenu} />)}</ul>)}
      </section>

      {tags.length > 0 && (
        <section className="sb-sec">
          <h4 className="sb-h">
            <button type="button" className="sb-h-btn" aria-expanded={tagsOpen} onClick={() => setTagsOpen(o => !o)}><Icon name={tagsOpen ? 'chevron-down' : 'chevron-right'} size={13} />Tags</button>
          </h4>
          {tagsOpen && <div className="tag-cloud">{tags.map(t => (
            <NavLink key={t.tag} to={`/?tag=${encodeURIComponent(t.tag)}`} onClick={onNavigate} className={() => `tag ${params.get('tag') === t.tag ? 'active' : ''}`}>#{t.tag}<small>{t.count}</small></NavLink>
          ))}</div>}
        </section>
      )}

      <div className="sb-bottom">
        <button type="button" className={`sync-state ${syncKind}`} onClick={onSync} title={sync?.error || 'Sync now'}>
          <span className="sync-dot" />
          <span className="grow">{sync?.error ? 'Storage does not answer' : sync?.where || 'Notes'}</span>
          <span>{syncing || sync?.syncing ? 'syncing…' : sync?.last_sync ? ago(sync.last_sync) : ''}</span>
        </button>
        <div className="sb-foot">
          <Link to="/trash" className={`sb-foot-link ${loc.pathname === '/trash' ? 'active' : ''}`} onClick={onNavigate}><Icon name="trash" size={15} />Trash</Link>
          <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
          <button type="button" className="sb-foot-link sb-foot-icon" title="Keyboard shortcuts (?)" aria-label="Keyboard shortcuts" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pn:shortcuts')) }}><Icon name="keyboard" size={15} /></button>
        </div>
      </div>
    </aside>
  )
}

function Branch({ node, here, onNavigate, onMenu }: { node: TreeNode; here: string | null; onNavigate: () => void; onMenu: (path: string, at: Anchor) => void }) {
  const key = 'pn-open:' + node.path
  const [open, setOpen] = useState(() => localStorage.getItem(key) === '1' || (here != null && here.startsWith(node.path + '/')))
  useEffect(() => { localStorage.setItem(key, open ? '1' : '0') }, [open, key])
  const active = here === node.path
  return (
    <li>
      <div className={`tree-row ${active ? 'active' : ''}`} onContextMenu={e => { e.preventDefault(); onMenu(node.path, { x: e.clientX, y: e.clientY, w: 0, h: 0 }) }}>
        <button type="button" className="twist" onClick={() => setOpen(o => !o)} style={{ visibility: node.children.length ? 'visible' : 'hidden' }} aria-label={open ? 'Collapse' : 'Expand'}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} /></button>
        <NavLink to={`/?in=${encodeURIComponent(node.path)}`} className="tree-link" title={node.path} onClick={onNavigate}>
          <Icon name={active ? 'folder-open' : 'folder'} size={15} /><span className="grow">{node.name}</span>{node.total > 0 && <span className="sb-count">{node.total}</span>}
        </NavLink>
        <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${node.name}`} aria-haspopup="menu" onClick={e => onMenu(node.path, rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
      </div>
      {open && node.children.length > 0 && <ul>{node.children.map(c => <Branch key={c.path} node={c} here={here} onNavigate={onNavigate} onMenu={onMenu} />)}</ul>}
    </li>
  )
}
