import { useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { usePlayer } from '../lib/player'
import { Avatar } from './people'
import { Player } from './Player'

const nav = [
  { to: '/home', label: 'Home', ico: '◉' },
  { to: '/podcasts', label: 'Podcasts', ico: '▤' },
  { to: '/episodes', label: 'Episodes', ico: '☰' },
  { to: '/queue', label: 'Queue', ico: '≡' },
  { to: '/playlists', label: 'Playlists', ico: '✦' },
  { to: '/add', label: 'Add', ico: '+' },
  { to: '/saved', label: 'Saved', ico: '♡' },
  { to: '/history', label: 'History', ico: '↺' },
  { to: '/downloads', label: 'Downloads', ico: '↓' },
  { to: '/stats', label: 'Stats', ico: '◔' },
  { to: '/settings', label: 'Settings', ico: '⚙' },
]
const tabs = ['/home', '/podcasts', '/episodes', '/queue', '/add']

export default function Layout() {
  const { user, settings, status, theme, toggleTheme, setUser } = useApp()
  const player = usePlayer()
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const logout = async () => { await api.post(`${V1}/auth/logout`); setUser(null); navigate('/login') }
  const title = settings?.title || 'Pocket Notes'
  return (
    <div className={`app ${player.current ? 'has-player' : ''}`}>
      <aside className={`sidebar ${open ? 'open' : ''}`} onClick={() => setOpen(false)}>
        <div className="brand"><span className="logo"><img src="/favicon.svg" alt="" /></span><span className="brand-name">{title}</span></div>
        <nav>
          {nav.map(n => <NavLink key={n.to} to={n.to} className={({ isActive }) => isActive ? 'active' : ''}><span className="ico">{n.ico}</span>{n.label}</NavLink>)}
        </nav>
        <div className="foot">
          <span className="row" title={user?.email}>{user && <Avatar user={user} />}<span className="foot-id"><span>{user?.name || user?.email}</span>{status?.version && <a className="ver" href="https://github.com/abjelosevic88/pocket-homelab-notes/releases" target="_blank" rel="noreferrer" title="Version — click for release notes">v{status.version.replace(/^v/, '')}</a>}</span></span>
          <span className="row">
            <button className="btn ghost sm" onClick={toggleTheme} title="Toggle theme">{theme === 'dark' ? '☀' : '☾'}</button>
            {status?.auth_mode === 'local' && <button className="btn ghost sm" onClick={logout} title="Sign out">⎋</button>}
          </span>
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          <button className="btn ghost icon menu-btn" onClick={() => setOpen(o => !o)} aria-label="Menu">☰</button>
          <span className="topbar-title">{title}</span>
          <span className="spacer" />
          <button className="btn ghost sm" onClick={toggleTheme} title="Toggle theme">{theme === 'dark' ? '☀' : '☾'}</button>
        </header>
        <main className="content"><Outlet /></main>
        <Player />
        <nav className="tabbar">
          {nav.filter(n => tabs.includes(n.to)).map(n => <NavLink key={n.to} to={n.to} className={({ isActive }) => isActive ? 'active' : ''}><span className="ico">{n.ico}</span><span>{n.label}</span></NavLink>)}
        </nav>
      </div>
    </div>
  )
}
