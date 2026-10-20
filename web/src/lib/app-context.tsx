import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { Folder, Settings, SyncStatus, TagCount, Tools, User } from './types'

interface AuthStatus { needs_setup: boolean; auth_mode: string; authenticated: boolean; user: User | null; version: string; title: string; oidc?: boolean; oidc_name?: string }

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
  theme: 'light' | 'dark'
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
  const [theme, setTheme] = useState<'light' | 'dark'>(() => (localStorage.getItem('pn-theme') as 'light' | 'dark') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'))

  useEffect(() => { document.documentElement.dataset.theme = theme; localStorage.setItem('pn-theme', theme) }, [theme])
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

  return <Ctx.Provider value={{ status, user, settings, users, folders, tags, tools, sync, version, bump, theme, toggleTheme: () => setTheme(t => t === 'dark' ? 'light' : 'dark'), refresh, reloadMeta, reloadTree, setUser, setSettings }}>{children}</Ctx.Provider>
}
