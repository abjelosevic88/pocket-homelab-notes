import { useEffect, useMemo, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note } from '../lib/types'
import { ago, leaf, tint } from '../lib/format'
import { Avatar } from './people'
import { Confirm, Modal, useToast } from './ui'
import { QuickSwitcher } from './QuickSwitcher'
import { ErrorBoundary } from './ErrorBoundary'

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

export function newNoteIn(category: string, navigate: (to: string) => void, bump: () => void, toast: (m: string, k?: 'ok' | 'err') => void) {
  api.post<Note>(`${V1}/notes`, { title: 'New note', category, content: '' }).then(n => { bump(); navigate(`/note/${n.id}?in=${encodeURIComponent(category)}`) }).catch(e => toast((e as Error).message, 'err'))
}

export default function Layout() {
  const { user, settings, status, theme, toggleTheme, setUser, folders, tags, sync, bump, reloadTree } = useApp()
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [switcher, setSwitcher] = useState(false)
  const [capture, setCapture] = useState('')
  const [tagsOpen, setTagsOpen] = useState(() => localStorage.getItem('pn-tags') !== '0')
  const [folderMenu, setFolderMenu] = useState<{ path: string; x: number; y: number } | null>(null)
  const [prompt, setPrompt] = useState<{ kind: 'new' | 'rename'; path: string } | null>(null)
  const [del, setDel] = useState<string | null>(null)
  const [syncing, setSyncing] = useState(false)
  const navigate = useNavigate()
  const loc = useLocation()
  const [params] = useSearchParams()
  // The pictures folder holds no notes: it stays out of the tree unless it does.
  const tree = useMemo(() => buildTree(folders.filter(f => f.count > 0 || !settings?.attachments_folder || (f.path !== settings.attachments_folder && !f.path.startsWith(settings.attachments_folder + '/')))), [folders, settings?.attachments_folder])
  const title = settings?.title || 'Pocket Notes'
  const here = params.get('in')

  useEffect(() => { localStorage.setItem('pn-tags', tagsOpen ? '1' : '0') }, [tagsOpen])
  useEffect(() => { setOpen(false) }, [loc.pathname, loc.search])

  // ⌘K finds a note, ⌘⇧N / Alt+N makes one, ⌘⇧D opens today.
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key.toLowerCase() === 'k' && !e.shiftKey) { e.preventDefault(); setSwitcher(true) }
      else if ((mod && e.shiftKey && e.key.toLowerCase() === 'n') || (e.altKey && e.key.toLowerCase() === 'n')) { e.preventDefault(); newNote() }
      else if (mod && e.shiftKey && e.key.toLowerCase() === 'd') { e.preventDefault(); openToday() }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const logout = async () => { await api.post(`${V1}/auth/logout`); setUser(null); navigate('/login') }
  const newNote = () => newNoteIn(here ?? settings?.default_folder ?? '', navigate, bump, toast)
  const openToday = async () => {
    try { const r = await api.post<{ note: Note }>(`${V1}/daily`, {}); bump(); navigate(`/note/${r.note.id}?in=${encodeURIComponent(r.note.category)}`) } catch (e) { toast((e as Error).message, 'err') }
  }
  const doCapture = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!capture.trim()) return
    try { await api.post(`${V1}/capture`, { text: capture }); setCapture(''); bump(); toast('Added to today’s note') } catch (err) { toast((err as Error).message, 'err') }
  }
  const syncNow = async () => {
    setSyncing(true)
    try { const s = await api.post<{ added: number; changed: number; removed: number; moved: number }>(`${V1}/sync`); bump(); await reloadTree(); toast(s.added + s.changed + s.removed + s.moved ? `Synced: ${s.added} new, ${s.changed} changed, ${s.moved} moved, ${s.removed} removed` : 'Up to date') } catch (e) { toast((e as Error).message, 'err') } finally { setSyncing(false) }
  }
  const folderAction = async (kind: 'new' | 'rename', path: string, name: string) => {
    try {
      if (kind === 'new') { const r = await api.post<{ path: string }>(`${V1}/folders`, { path: path ? `${path}/${name}` : name }); navigate(`/?in=${encodeURIComponent(r.path)}`) }
      else { const to = path.includes('/') ? `${path.slice(0, path.lastIndexOf('/'))}/${name}` : name; const r = await api.put<{ path: string }>(`${V1}/folders`, { from: path, to }); navigate(`/?in=${encodeURIComponent(r.path)}`) }
      bump(); await reloadTree()
    } catch (e) { toast((e as Error).message, 'err') }
  }

  const syncDot = sync?.error ? 'bad' : sync?.syncing || syncing ? 'busy' : 'ok'

  return (
    <div className="app">
      <aside className={`sidebar ${open ? 'open' : ''}`}>
        <div className="brand"><span className="logo"><img src="/favicon.svg" alt="" /></span><span className="brand-name">{title}</span></div>
        <div className="side-actions">
          <button className="btn primary grow" onClick={newNote} title="New note (Alt+N)">＋ New note</button>
          <button className="btn icon" onClick={() => setSwitcher(true)} title="Find a note (⌘K)">⌕</button>
        </div>
        <form className="capture" onSubmit={doCapture}><input value={capture} onChange={e => setCapture(e.target.value)} placeholder="Capture to today…" aria-label="Quick capture to today's note" /></form>
        <nav className="side-nav">
          <button className="nav-item" onClick={openToday} title="Today's daily note (⌘⇧D)"><span className="ico">☀</span>Today</button>
          <NavLink to="/" end className={() => `nav-item ${loc.pathname === '/' || loc.pathname.startsWith('/note/') ? (!params.toString() ? 'active' : '') : ''}`}><span className="ico">▤</span>All notes</NavLink>
          <NavLink to="/?view=recent" className={() => `nav-item ${params.get('view') === 'recent' ? 'active' : ''}`}><span className="ico">↺</span>Recent</NavLink>
          <NavLink to="/?view=favorites" className={() => `nav-item ${params.get('view') === 'favorites' ? 'active' : ''}`}><span className="ico">★</span>Favourites</NavLink>
          <NavLink to="/tasks" className="nav-item"><span className="ico">☑</span>Tasks</NavLink>
          <NavLink to="/calendar" className="nav-item"><span className="ico">▦</span>Calendar</NavLink>
        </nav>
        <div className="side-scroll">
          <div className="side-head"><span>Folders</span><button className="btn ghost xs" title="New folder" onClick={() => setPrompt({ kind: 'new', path: '' })}>＋</button></div>
          <ul className="tree">
            {tree.map(n => <Branch key={n.path} node={n} here={here} onMenu={(path, x, y) => setFolderMenu({ path, x, y })} />)}
          </ul>
          {tags.length > 0 && <>
            <button className="side-head as-btn" onClick={() => setTagsOpen(o => !o)}><span>Tags</span><span className="muted">{tagsOpen ? '▾' : '▸'}</span></button>
            {tagsOpen && <div className="tag-cloud">{tags.map(t => <NavLink key={t.tag} to={`/?tag=${encodeURIComponent(t.tag)}`} className={() => `tag ${params.get('tag') === t.tag ? 'active' : ''}`} style={{ ['--tag' as string]: tint(t.tag) }}>#{t.tag}<small>{t.count}</small></NavLink>)}</div>}
          </>}
        </div>
        <nav className="side-nav bottom">
          <NavLink to="/trash" className="nav-item"><span className="ico">🗑</span>Trash</NavLink>
          <NavLink to="/settings" className="nav-item"><span className="ico">⚙</span>Settings</NavLink>
        </nav>
        <button className={`sync-state ${syncDot}`} onClick={syncNow} title={sync?.error || 'Sync now'}>
          <span className="dot" />
          <span className="grow">{sync?.error ? 'Storage does not answer' : sync?.where || 'Notes'}</span>
          <small className="muted">{syncing || sync?.syncing ? 'syncing…' : sync?.last_sync ? ago(sync.last_sync) : ''}</small>
        </button>
        <div className="foot">
          <span className="row" title={user?.email}>{user && <Avatar user={user} />}<span className="foot-id"><span>{user?.name || user?.email}</span>{status?.version && <a className="ver" href="https://github.com/abjelosevic88/pocket-homelab-notes/releases" target="_blank" rel="noreferrer">v{status.version.replace(/^v/, '')}</a>}</span></span>
          <span className="row">
            <button className="btn ghost sm" onClick={toggleTheme} title="Toggle theme">{theme === 'dark' ? '☀' : '☾'}</button>
            {status?.auth_mode === 'local' && <button className="btn ghost sm" onClick={logout} title="Sign out">⎋</button>}
          </span>
        </div>
      </aside>
      {open && <div className="scrim" onClick={() => setOpen(false)} />}
      <div className="main">
        <header className="topbar">
          <button className="btn ghost icon" onClick={() => setOpen(o => !o)} aria-label="Menu">☰</button>
          <span className="topbar-title">{title}</span>
          <span className="spacer" />
          <button className="btn ghost icon" onClick={() => setSwitcher(true)} aria-label="Find">⌕</button>
          <button className="btn ghost icon" onClick={newNote} aria-label="New note">＋</button>
        </header>
        <main className="content"><ErrorBoundary key={loc.pathname} where={loc.pathname}><Outlet /></ErrorBoundary></main>
      </div>
      {switcher && <QuickSwitcher onClose={() => setSwitcher(false)} onNew={() => { setSwitcher(false); newNote() }} onToday={() => { setSwitcher(false); openToday() }} />}
      {folderMenu && <div className="ctx-menu" style={{ left: folderMenu.x, top: folderMenu.y }} onMouseLeave={() => setFolderMenu(null)}>
        <button onClick={() => { const p = folderMenu.path; setFolderMenu(null); newNoteIn(p, navigate, bump, toast) }}>New note here</button>
        <button onClick={() => { setPrompt({ kind: 'new', path: folderMenu.path }); setFolderMenu(null) }}>New folder inside…</button>
        <button onClick={() => { setPrompt({ kind: 'rename', path: folderMenu.path }); setFolderMenu(null) }}>Rename…</button>
        <button className="danger-text" onClick={() => { setDel(folderMenu.path); setFolderMenu(null) }}>Delete…</button>
      </div>}
      {prompt && <FolderPrompt kind={prompt.kind} path={prompt.path} onClose={() => setPrompt(null)} onDone={name => { const p = prompt; setPrompt(null); folderAction(p.kind, p.path, name) }} />}
      {del && <Confirm title={`Delete the folder “${leaf(del)}”?`} message="Every note in it (and in its folders) goes to the trash; other files in it are deleted with the folder." onCancel={() => setDel(null)} onConfirm={async () => { const d = del; setDel(null); try { await api.del(`${V1}/folders?path=${encodeURIComponent(d)}`); bump(); navigate('/') } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

function Branch({ node, here, onMenu }: { node: TreeNode; here: string | null; onMenu: (path: string, x: number, y: number) => void }) {
  const key = 'pn-open:' + node.path
  const [open, setOpen] = useState(() => localStorage.getItem(key) === '1' || (here != null && here.startsWith(node.path + '/')))
  useEffect(() => { localStorage.setItem(key, open ? '1' : '0') }, [open, key])
  const active = here === node.path
  return (
    <li>
      <div className={`tree-row ${active ? 'active' : ''}`} onContextMenu={e => { e.preventDefault(); onMenu(node.path, e.clientX, e.clientY) }}>
        <button className="twist" onClick={() => setOpen(o => !o)} style={{ visibility: node.children.length ? 'visible' : 'hidden' }} aria-label={open ? 'Collapse' : 'Expand'}>{open ? '▾' : '▸'}</button>
        <NavLink to={`/?in=${encodeURIComponent(node.path)}`} className="tree-link" title={node.path}><span className="folder-ico">📁</span><span className="grow">{node.name}</span><small className="muted">{node.total || ''}</small></NavLink>
        <button className="tree-more" onClick={e => { const r = (e.target as HTMLElement).getBoundingClientRect(); onMenu(node.path, r.left, r.bottom) }} aria-label="Folder actions">⋯</button>
      </div>
      {open && node.children.length > 0 && <ul>{node.children.map(c => <Branch key={c.path} node={c} here={here} onMenu={onMenu} />)}</ul>}
    </li>
  )
}

function FolderPrompt({ kind, path, onDone, onClose }: { kind: 'new' | 'rename'; path: string; onDone: (name: string) => void; onClose: () => void }) {
  const [name, setName] = useState(kind === 'rename' ? leaf(path) : '')
  return (
    <Modal title={kind === 'new' ? (path ? `New folder in “${leaf(path)}”` : 'New folder') : `Rename “${leaf(path)}”`} onClose={onClose}>
      <form onSubmit={e => { e.preventDefault(); if (name.trim()) onDone(name.trim()) }} className="grid" style={{ gap: 14 }}>
        <input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="Folder name" />
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">{kind === 'new' ? 'Make' : 'Rename'}</button></div>
      </form>
    </Modal>
  )
}
