import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { api, V1 } from './api'
import type { Folder, Settings, SyncStatus, TagCount, Tools, User } from './types'

interface AuthStatus { needs_setup: boolean; auth_mode: string; authenticated: boolean; user: User | null; version: string; title: string; oidc?: boolean; oidc_name?: string }

export type ThemeMode = 'system' | 'light' | 'dark'

interface AppState {
  status: AuthStatus | null
  user: User | null
  settings: Settings | null
  users: User[]
  folders: Folder[]
  tags: TagCount[]
  tools: Tools
  sync: SyncStatus | null
  /** Changes whenever notes were added, renamed, moved or deleted: lists reload on it. */
  version: number
  bump: () => void
  /** The appearance chosen: the system's unless set to light or dark. */
  themeMode: ThemeMode
  theme: 'light' | 'dark'
  setThemeMode: (m: ThemeMode) => void
  toggleTheme: () => void
  refresh: () => Promise<void>
  reloadMeta: () => Promise<void>
  reloadTree: () => Promise<void>
  setUser: (u: User | null) => void
  setSettings: (s: Settings) => void
}

const Ctx = createContext<AppState>(null!)
export function useApp() { return useContext(Ctx) }

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [folders, setFolders] = useState<Folder[]>([])
  const [tags, setTags] = useState<TagCount[]>([])
  const [tools, setTools] = useState<Tools>({})
  const [sync, setSync] = useState<SyncStatus | null>(null)
  const [version, setVersion] = useState(0)
  // The theme follows the system unless one is chosen (pn-theme holds the choice; the shared-note
  // page reads it too).
  const [themeMode, setThemeModeRaw] = useState<ThemeMode>(() => (localStorage.getItem('pn-theme') as ThemeMode) || 'system')
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => setSystemDark(mq.matches)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [])
  const theme: 'light' | 'dark' = themeMode === 'system' ? (systemDark ? 'dark' : 'light') : themeMode
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.querySelectorAll('meta[name="theme-color"]').forEach(m => m.setAttribute('content', theme === 'dark' ? '#111113' : '#ffffff'))
  }, [theme])
  const setThemeMode = useCallback((m: ThemeMode) => { setThemeModeRaw(m); if (m === 'system') localStorage.removeItem('pn-theme'); else localStorage.setItem('pn-theme', m) }, [])
  useEffect(() => { if (settings?.title) document.title = settings.title }, [settings?.title])

  const reloadMeta = useCallback(async () => {
    const [u, t] = await Promise.all([api.get<User[]>(`${V1}/users`), api.get<Tools>(`${V1}/tools`).catch(() => ({}))])
    setUsers(u); setTools(t)
  }, [])

  const reloadTree = useCallback(async () => {
    const [f, t, s] = await Promise.all([
      api.get<Folder[]>(`${V1}/folders`).catch(() => [] as Folder[]),
      api.get<TagCount[]>(`${V1}/tags`).catch(() => [] as TagCount[]),
      api.get<{ status: SyncStatus }>(`${V1}/storage`).then(r => r.status).catch(() => null),
    ])
    setFolders(f); setTags(t); setSync(s)
  }, [])

  const refresh = useCallback(async () => {
    const st = await api.get<AuthStatus>(`${V1}/auth/status`)
    setStatus(st)
    setUser(st.user)
    if (st.authenticated) {
      setSettings(await api.get<Settings>(`${V1}/settings`))
      await Promise.all([reloadMeta(), reloadTree()])
    }
  }, [reloadMeta, reloadTree])

  useEffect(() => { refresh().catch(() => setStatus({ needs_setup: false, auth_mode: 'local', authenticated: false, user: null, version: '', title: 'Pocket Notes' })) }, [refresh])
  useEffect(() => {
    const h = () => { setUser(null); setStatus(s => s ? { ...s, authenticated: false, user: null } : s) }
    window.addEventListener('pn:unauthorized', h)
    return () => window.removeEventListener('pn:unauthorized', h)
  }, [])
  // The folder tree, tags and sync state follow every change, and the storage every minute.
  useEffect(() => { if (user && version > 0) reloadTree().catch(() => {}) }, [version, user, reloadTree])
  useEffect(() => {
    if (!user) return
    const t = setInterval(() => reloadTree().catch(() => {}), 60000)
    const onFocus = () => reloadTree().catch(() => {})
    window.addEventListener('focus', onFocus)
    return () => { clearInterval(t); window.removeEventListener('focus', onFocus) }
  }, [user, reloadTree])

  const bump = useCallback(() => setVersion(v => v + 1), [])

  // Changes made elsewhere (the phone, Nextcloud, another tab) reach the open lists within
  // seconds: a small stamp is asked for while the page is visible, and when it moves the lists
  // reload. The first answer only sets where it starts.
  const stamp = useRef('')
  useEffect(() => {
    if (!user) return
    const check = async () => {
      if (document.hidden) return
      try {
        const r = await api.get<{ stamp: string }>(`${V1}/changes`)
        if (stamp.current && r.stamp !== stamp.current) bump()
        stamp.current = r.stamp
      } catch {}
    }
    check()
    const t = setInterval(check, 10000)
    const onShow = () => { if (!document.hidden) check() }
    document.addEventListener('visibilitychange', onShow)
    window.addEventListener('focus', onShow)
    return () => { clearInterval(t); document.removeEventListener('visibilitychange', onShow); window.removeEventListener('focus', onShow) }
  }, [user, bump])

  return <Ctx.Provider value={{ status, user, settings, users, folders, tags, tools, sync, version, bump, themeMode, theme, setThemeMode, toggleTheme: () => setThemeMode(theme === 'dark' ? 'light' : 'dark'), refresh, reloadMeta, reloadTree, setUser, setSettings }}>{children}</Ctx.Provider>
}
