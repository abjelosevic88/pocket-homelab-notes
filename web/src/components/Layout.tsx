// The app shell: sidebar, top bar, ⌘K palette, shortcuts — the Pocket Calendar layout.
import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note } from '../lib/types'
import { colorFor, leaf } from '../lib/format'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { ErrorBoundary } from './ErrorBoundary'
import { type Anchor, Avatar, Confirm, Menu, type MenuItem, MenuButton, Modal, modalOpen, useIsMobile, useMediaQuery, useToast } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pn-sidebar'

export function newNoteIn(category: string, navigate: (to: string) => void, bump: () => void, toast: (m: string, k?: 'ok' | 'err') => void) {
  api.post<Note>(`${V1}/notes`, { title: 'New note', category, content: '' }).then(n => { bump(); navigate(`/note/${n.id}?in=${encodeURIComponent(category)}`) }).catch(e => toast((e as Error).message, 'err'))
}

/** The part of the app a path belongs to: every note is part of the notes page. */
const section = (path: string) => (path === '/' || path.startsWith('/note/') ? 'notes' : path.split('/')[1])

export default function Layout() {
  const { settings, bump, reloadTree } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const loc = useLocation()
  const [params] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { closeDrawer() }, [loc.pathname, loc.search]) // eslint-disable-line react-hooks/exhaustive-deps

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [folderMenu, setFolderMenu] = useState<{ path: string; at: Anchor } | null>(null)
  const [prompt, setPrompt] = useState<{ kind: 'new' | 'rename'; path: string } | null>(null)
  const [del, setDel] = useState<string | null>(null)
  const [syncing, setSyncing] = useState(false)
  const here = params.get('in')

  const newNote = () => newNoteIn(here ?? settings?.default_folder ?? '', navigate, bump, toast)
  const openToday = () => navigate('/today')
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

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pn:shortcuts', open)
    return () => window.removeEventListener('pn:shortcuts', open)
  }, [])

  // ⌘K finds a note, ⌘⇧N / Alt+N makes one, ⌘⇧D opens today; single keys when nothing is typed.
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      const k = e.key.toLowerCase()
      if (mod && k === 'k' && !e.shiftKey) { e.preventDefault(); setPalette(p => !p); return }
      if ((mod && e.shiftKey && k === 'n') || (e.altKey && !mod && (k === 'n' || e.code === 'KeyN'))) { e.preventDefault(); newNote(); return }
      if (mod && e.shiftKey && k === 'd') { e.preventDefault(); openToday(); return }
      if (mod || e.altKey) return
      const t = e.target as HTMLElement
      if (t.closest?.('input, textarea, select, [contenteditable="true"], iframe') || modalOpen() || document.querySelector('.menu, .popover, .select-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': e.preventDefault(); newNote(); break
        case 't': go('/today'); break
        case 'a': go('/'); break
        case 'x': go('/tasks'); break
        case 'c': go('/calendar'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const folderItems = (path: string): MenuItem[] => [
    { heading: leaf(path) },
    { label: 'New note here', icon: 'file-plus', onSelect: () => newNoteIn(path, navigate, bump, toast) },
    { label: 'New folder inside…', icon: 'folder-plus', onSelect: () => setPrompt({ kind: 'new', path }) },
    { label: 'Rename…', icon: 'edit', onSelect: () => setPrompt({ kind: 'rename', path }) },
    'sep',
    { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setDel(path) },
  ]

  const onNote = loc.pathname.startsWith('/note/')
  return (
    <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''} ${onNote ? 'on-note' : ''}`}>
      {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
      <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
        <Sidebar onNew={newNote} onNavigate={closeDrawer} onFolderMenu={(path, at) => setFolderMenu({ path, at })} onNewFolder={() => { closeDrawer(); setPrompt({ kind: 'new', path: '' }) }} onSync={syncNow} syncing={syncing} />
      </div>
      <div className="main">
        <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} />
        <main className="content"><ErrorBoundary key={section(loc.pathname)} where={loc.pathname}><Outlet /></ErrorBoundary></main>
      </div>
      {mobile && <button type="button" className="fab" aria-label="New note" title="New note" onClick={newNote}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
      {palette && <CommandPalette onClose={() => setPalette(false)} onNew={newNote} onShortcuts={() => setShortcuts(true)} />}
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      {folderMenu && <Menu anchor={folderMenu.at} items={folderItems(folderMenu.path)} onClose={() => setFolderMenu(null)} width={210} />}
      {prompt && <FolderPrompt kind={prompt.kind} path={prompt.path} onClose={() => setPrompt(null)} onDone={name => { const p = prompt; setPrompt(null); folderAction(p.kind, p.path, name) }} />}
      {del && <Confirm title={`Delete the folder “${leaf(del)}”?`} confirmLabel="Delete" message="Every note in it (and in its folders) goes to the trash; other files in it are deleted with the folder." onCancel={() => setDel(null)} onConfirm={async () => { const d = del; setDel(null); try { await api.del(`${V1}/folders?path=${encodeURIComponent(d)}`); bump(); navigate('/') } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

const SECTION: Record<string, string> = { today: 'Today', tasks: 'Tasks', calendar: 'Calendar', trash: 'Trash', new: 'New note' }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [params] = useSearchParams()
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pn:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]

  // The title: the view, or the folder as a path to click back up.
  const first = loc.pathname.split('/')[1] || ''
  let title: React.ReactNode = SECTION[first] || ''
  if (first === '' || first === 'note') {
    const folder = params.get('in'), tag = params.get('tag'), view = params.get('view'), q = params.get('q')
    const sep = <Icon name="chevron-right" size={16} className="crumb-sep" />
    if (q) title = 'Search'
    else if (view === 'favorites') title = 'Favourites'
    else if (view === 'recent') title = 'Recent'
    else if (tag) title = <><button type="button" className="crumb" onClick={() => navigate('/')}>Notes</button>{sep}<span className="ellipsis">#{tag}</span></>
    else if (folder != null) {
      const parts = folder ? folder.split('/') : []
      title = <>
        <button type="button" className="crumb" onClick={() => navigate('/')}>Notes</button>
        {parts.length === 0 && <>{sep}<span className="ellipsis">Top level</span></>}
        {parts.map((p, i) => <span key={i} className="crumb-part">{sep}{i === parts.length - 1
          ? <span className="ellipsis">{p}</span>
          : <button type="button" className="crumb" onClick={() => navigate(`/?in=${encodeURIComponent(parts.slice(0, i + 1).join('/'))}`)}>{p}</button>}</span>)}
      </>
    } else title = 'All notes'
  }

  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">{title}</h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} color={user ? colorFor(user.id) : undefined} />
      </MenuButton>
    </header>
  )
}

function FolderPrompt({ kind, path, onDone, onClose }: { kind: 'new' | 'rename'; path: string; onDone: (name: string) => void; onClose: () => void }) {
  const [name, setName] = useState(kind === 'rename' ? leaf(path) : '')
  return (
    <Modal title={kind === 'new' ? (path ? `New folder in “${leaf(path)}”` : 'New folder') : `Rename “${leaf(path)}”`} onClose={onClose} size="sm">
      <form onSubmit={e => { e.preventDefault(); if (name.trim()) onDone(name.trim()) }} className="stack">
        <input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="Folder name" aria-label="Folder name" />
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary">{kind === 'new' ? 'Make' : 'Rename'}</button></div>
      </form>
    </Modal>
  )
}
