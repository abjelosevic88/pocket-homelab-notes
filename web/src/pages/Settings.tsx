import { useEffect, useState } from 'react'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Activity, APIToken, Podcast, Settings as SettingsT, User } from '../lib/types'
import { Card, Confirm, Field, Loading, Modal, PageHeader, Segmented, Tabs, useAsync, useToast } from '../components/ui'
import { Avatar } from '../components/people'
import { ago, bytes, SB_CATEGORIES } from '../lib/format'

type Tab = 'general' | 'playback' | 'downloads' | 'search' | 'notify' | 'feeds' | 'users' | 'profile' | 'api' | 'data' | 'activity' | 'about'

export default function Settings() {
  const [tab, setTab] = useState<Tab>(() => (new URLSearchParams(window.location.search).get('tab') as Tab) || 'general')
  return (
    <>
      <PageHeader title="Settings" />
      <Tabs tabs={[{ id: 'general', label: 'General' }, { id: 'playback', label: 'Playback' }, { id: 'downloads', label: 'Feeds & downloads' }, { id: 'search', label: 'Search' }, { id: 'notify', label: 'Notifications' }, { id: 'feeds', label: 'Public feeds' }, { id: 'users', label: 'Users' }, { id: 'profile', label: 'Profile' }, { id: 'api', label: 'API tokens' }, { id: 'data', label: 'Backup & data' }, { id: 'activity', label: 'Activity' }, { id: 'about', label: 'About' }]} value={tab} onChange={setTab} />
      <div className="mt">
        {tab === 'general' && <General />}
        {tab === 'playback' && <Playback />}
        {tab === 'downloads' && <Downloads />}
        {tab === 'search' && <Search />}
        {tab === 'notify' && <Notify />}
        {tab === 'feeds' && <Feeds />}
        {tab === 'users' && <Users />}
        {tab === 'profile' && <Profile />}
        {tab === 'api' && <Tokens />}
        {tab === 'data' && <Data />}
        {tab === 'activity' && <ActivityTab />}
        {tab === 'about' && <About />}
      </div>
    </>
  )
}

function useSettingsForm() {
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (extra: Record<string, unknown> = {}) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...extra }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy }
}

function General() {
  const { f, setF, save, busy } = useSettingsForm()
  const { user } = useApp()
  if (!f) return <Loading />
  return (
    <Card title="General">
      <div className="grid" style={{ gap: 14, maxWidth: 560 }}>
        <p className="muted">Everyone on this box has their own subscriptions, queue, history and settings. Only the title is shared.</p>
        {user?.role === 'admin' && <Field label="App title" help="Shown in the sidebar and browser tab, for everyone."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} /></Field>}
        <Field group label="Open on"><Segmented value={f.landing} onChange={v => setF({ ...f, landing: v })} options={[{ value: 'home', label: 'Home' }, { value: 'podcasts', label: 'Podcasts' }, { value: 'episodes', label: 'Episodes' }, { value: 'queue', label: 'Queue' }]} /></Field>
        <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
      </div>
    </Card>
  )
}

function Playback() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  const cats = new Set(f.skip_categories.split(',').map(s => s.trim()).filter(Boolean))
  const toggle = (id: string) => { if (cats.has(id)) cats.delete(id); else cats.add(id); setF({ ...f, skip_categories: Array.from(cats).join(',') }) }
  return (
    <div className="two-col">
      <Card title="Player">
        <div className="grid" style={{ gap: 14 }}>
          <Field label="Default speed" help="Each podcast can override it."><input type="number" step={0.05} min={0.5} max={3} value={f.playback_speed} onChange={e => setF({ ...f, playback_speed: Number(e.target.value) })} className="num" /></Field>
          <div className="two-col tight">
            <Field label="Skip back (s)"><input type="number" min={5} max={300} value={f.skip_back} onChange={e => setF({ ...f, skip_back: Number(e.target.value) })} className="num" /></Field>
            <Field label="Skip forward (s)"><input type="number" min={5} max={300} value={f.skip_forward} onChange={e => setF({ ...f, skip_forward: Number(e.target.value) })} className="num" /></Field>
          </div>
          <Field label="Count as played at (%)" help="Reaching this far marks the episode played and drops it from the queue."><input type="number" min={50} max={100} value={f.complete_at} onChange={e => setF({ ...f, complete_at: Number(e.target.value) })} className="num" /></Field>
          <label className="row"><input type="checkbox" checked={f.auto_play_next} onChange={e => setF({ ...f, auto_play_next: e.target.checked })} style={{ width: 'auto' }} /> When an episode ends, play the next one in the queue</label>
          <p className="muted small">Keyboard: <kbd>space</kbd> play/pause · <kbd>←</kbd> <kbd>→</kbd> skip · <kbd>shift</kbd>+<kbd>→</kbd> a minute · <kbd>&lt;</kbd> <kbd>&gt;</kbd> speed · <kbd>shift</kbd>+<kbd>N</kbd> next</p>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
        </div>
      </Card>
      <Card title="SponsorBlock">
        <div className="grid" style={{ gap: 12 }}>
          <p className="muted">YouTube episodes come with the segments the SponsorBlock community marked. The player jumps over the categories you tick; seek back into one and it stays.</p>
          <label className="row"><input type="checkbox" checked={f.auto_skip_sponsors} onChange={e => setF({ ...f, auto_skip_sponsors: e.target.checked })} style={{ width: 'auto' }} /> Skip automatically</label>
          <div className="cat-grid">
            {SB_CATEGORIES.map(c => <label key={c.id} className="row" title={c.hint}><input type="checkbox" checked={cats.has(c.id)} onChange={() => toggle(c.id)} style={{ width: 'auto' }} /> {c.label}<span className="muted small"> — {c.hint}</span></label>)}
          </div>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
        </div>
      </Card>
    </div>
  )
}

function Downloads() {
  const { f, setF, save, busy } = useSettingsForm()
  const { user } = useApp()
  const { toast } = useToast()
  const [updating, setUpdating] = useState(false)
  if (!f) return <Loading />
  const updateYt = async () => {
    setUpdating(true)
    try { const r = await api.post<{ version: string }>(`${V1}/settings/ytdlp/update`); toast('yt-dlp is now ' + r.version); setF({ ...f, yt_dlp: r.version }) } catch (e) { toast((e as Error).message, 'err') } finally { setUpdating(false) }
  }
  return (
    <div className="two-col">
      <Card title="Feeds">
        <div className="grid" style={{ gap: 14 }}>
          <Field label="Check feeds every (minutes)" help={`0 = the box default (${f.refresh_default} min). A podcast can set its own.`}><input type="number" min={0} value={f.refresh_minutes} onChange={e => setF({ ...f, refresh_minutes: Number(e.target.value) })} className="num" /></Field>
          <label className="row"><input type="checkbox" checked={f.auto_download_default} onChange={e => setF({ ...f, auto_download_default: e.target.checked })} style={{ width: 'auto' }} /> New podcast subscriptions download new episodes automatically</label>
          <Field label="Keep files for (days)" help="Default retention for new subscriptions. 0 = keep. Saved and queued episodes are never removed."><input type="number" min={0} value={f.retention_days} onChange={e => setF({ ...f, retention_days: Number(e.target.value) })} className="num" /></Field>
          <label className="row"><input type="checkbox" checked={f.keep_played} onChange={e => setF({ ...f, keep_played: e.target.checked })} style={{ width: 'auto' }} /> Keep podcast files after playing (otherwise a played podcast download is removed; YouTube files follow retention)</label>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
        </div>
      </Card>
      <Card title="YouTube">
        <div className="grid" style={{ gap: 14 }}>
          <p className="muted">Channels and playlists are read and downloaded with yt-dlp on this box. YouTube changes often; when downloads start failing, update it here — no rebuild needed.</p>
          <table className="table compact"><tbody>
            <tr><th>yt-dlp</th><td className="mono">{f.yt_dlp || <span className="bad">not available</span>}</td><td className="right">{user?.role === 'admin' && f.yt_dlp_updatable && <button className="btn sm" onClick={updateYt} disabled={updating}>{updating ? 'Updating…' : 'Update now'}</button>}</td></tr>
          </tbody></table>
          <Field label="Videos fetched when subscribing to a channel" help="The newest N; afterwards every upload."><input type="number" min={0} max={200} value={f.youtube_limit} onChange={e => setF({ ...f, youtube_limit: Number(e.target.value) })} className="num" /></Field>
          <Field label="Skip videos shorter than (s)" help="Keeps Shorts out of new subscriptions."><input type="number" min={0} value={f.youtube_min_duration} onChange={e => setF({ ...f, youtube_min_duration: Number(e.target.value) })} className="num" /></Field>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
        </div>
      </Card>
    </div>
  )
}

function Search() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [secret, setSecret] = useState('')
  const [testing, setTesting] = useState(false)
  if (!f) return <Loading />
  const test = async () => {
    setTesting(true)
    try { await api.post(`${V1}/search/test`, { key: f.podcastindex_key, secret }); toast('Podcast Index answers') } catch (e) { toast((e as Error).message, 'err') } finally { setTesting(false) }
  }
  return (
    <Card title="Directories">
      <div className="grid" style={{ gap: 14, maxWidth: 600 }}>
        <p className="muted">Apple's directory needs no key. <a href="https://api.podcastindex.org/" target="_blank" rel="noreferrer">Podcast Index</a> is free and open; with a key you also get trending podcasts.</p>
        <Field group label="Search in by default"><Segmented value={f.search_source} onChange={v => setF({ ...f, search_source: v })} options={[{ value: 'all', label: 'Both' }, { value: 'itunes', label: 'Apple' }, { value: 'podcastindex', label: 'Podcast Index' }]} /></Field>
        <Field label="Podcast Index API key" help={f.podcastindex_env && !f.podcastindex_key ? 'A key is set through PODCASTINDEX_KEY.' : undefined}><input value={f.podcastindex_key} onChange={e => setF({ ...f, podcastindex_key: e.target.value })} spellCheck={false} autoComplete="off" /></Field>
        <Field label="API secret"><input type="password" value={secret} onChange={e => setSecret(e.target.value)} placeholder={f.podcastindex_secret_set ? 'saved — type to replace' : ''} autoComplete="off" /></Field>
        <div className="row"><button className="btn primary" onClick={() => save(secret ? { podcastindex_secret: secret } : {}).then(() => setSecret(''))} disabled={busy}>Save</button><button className="btn" onClick={test} disabled={testing || !f.podcastindex_key}>{testing ? 'Testing…' : 'Test'}</button></div>
      </div>
    </Card>
  )
}

function Notify() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [token, setToken] = useState('')
  const [testing, setTesting] = useState(false)
  if (!f) return <Loading />
  const hints: Record<string, string> = { apprise: 'Apprise API route, e.g. http://apprise:8000/notify/podcasts', ntfy: 'ntfy topic URL, e.g. https://ntfy.example.com/podcasts', webhook: 'Any URL that accepts a JSON POST {title, body, url}' }
  const test = async () => {
    setTesting(true)
    try { await api.post(`${V1}/settings/notify/test`, { kind: f.notify_kind, url: f.notify_url, token }); toast('Sent — check your phone') } catch (e) { toast((e as Error).message, 'err') } finally { setTesting(false) }
  }
  return (
    <Card title="New episodes">
      <div className="grid" style={{ gap: 14, maxWidth: 600 }}>
        <p className="muted">Tick “Notify me” on a podcast and a message goes here whenever it has new episodes.{f.notify_env && !f.notify_url && ' An address is set through NOTIFY_URL.'}</p>
        <Field group label="Send through"><Segmented value={f.notify_kind} onChange={v => setF({ ...f, notify_kind: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'webhook', label: 'Webhook' }]} /></Field>
        <Field label="Address" help={hints[f.notify_kind]}><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://…" spellCheck={false} /></Field>
        <Field label="Token (optional)" help="Sent as a bearer token (ntfy access token, Apprise key)."><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={f.notify_token_set ? 'saved — type to replace' : ''} autoComplete="off" /></Field>
        <div className="row"><button className="btn primary" onClick={() => save(token ? { notify_token: token } : {}).then(() => setToken(''))} disabled={busy}>Save</button><button className="btn" onClick={test} disabled={testing || !(f.notify_url || f.notify_env)}>{testing ? 'Sending…' : 'Send a test'}</button></div>
      </div>
    </Card>
  )
}

function Feeds() {
  const { f, setF, save, busy } = useSettingsForm()
  const { setSettings } = useApp()
  const { toast } = useToast()
  const pods = useAsync(() => api.get<Podcast[]>(`${V1}/podcasts?sort=title`), [])
  const [rotate, setRotate] = useState(false)
  if (!f) return <Loading />
  const copy = (t: string) => { navigator.clipboard?.writeText(t); toast('Copied') }
  return (
    <div className="two-col">
      <Card title="Your feeds">
        <div className="grid" style={{ gap: 12 }}>
          <p className="muted">Every subscription is also a podcast feed served by this box — including YouTube channels as audio, with the files, artwork and chapters. Subscribe to them in Apple Podcasts, Overcast, AntennaPod or any app. The key in the URL is your secret: anyone with it can listen.</p>
          <div className="row"><code className="mono grow" style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{f.feed_url}</code><button className="btn sm" onClick={() => copy(f.feed_url)}>Copy</button><a className="btn sm" href={f.feed_url} target="_blank" rel="noreferrer">Open ↗</a></div>
          <Field label="Public address (optional)" help="If feeds are reached through another hostname (a public tunnel), put its base URL here; it is used inside the feeds."><input value={f.feed_base_url} onChange={e => setF({ ...f, feed_base_url: e.target.value })} placeholder="https://feeds.example.com" spellCheck={false} /></Field>
          <div className="row"><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button><button className="btn ghost danger-text" onClick={() => setRotate(true)}>New key…</button></div>
        </div>
      </Card>
      <Card title="Feed URLs">
        {pods.data ? <div className="feed-list">{pods.data.map(p => <div key={p.id} className="f"><b>{p.title}</b><div className="row"><code className="mono small grow">{f.feed_url}{p.id}.xml</code><button className="btn ghost sm" onClick={() => copy(`${f.feed_url}${p.id}.xml`)}>Copy</button></div></div>)}</div> : <Loading />}
      </Card>
      {rotate && <Confirm title="Make a new feed key?" message="Every feed URL you gave to a podcast app stops working; you will have to subscribe again with the new ones." onCancel={() => setRotate(false)} onConfirm={async () => { const s = await api.post<SettingsT>(`${V1}/settings/feed-key`); setSettings(s); setF(s); setRotate(false); toast('New key made') }} />}
    </div>
  )
}

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Card title="Users" actions={isAdmin && <button className="btn primary sm" onClick={() => setEdit({ role: 'member' })}>+ Add user</button>}>
      <p className="muted">Everyone signs in with their own account and keeps their own subscriptions, queue, history, playlists, files and settings — admins included. Admins manage the accounts and the app title.</p>
      <table className="table compact">
        <thead><tr><th /><th>Name</th><th>Email</th><th>Role</th><th /></tr></thead>
        <tbody>
          {users.map(u => (
            <tr key={u.id}>
              <td style={{ width: 32 }}><Avatar user={u} /></td>
              <td>{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge" style={{ marginLeft: 6 }}>you</span>}</td>
              <td className="muted">{u.email}</td>
              <td><span className={`badge ${u.role === 'admin' ? 'cat' : ''}`}>{u.role}</span></td>
              <td className="right">{isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their subscriptions, history and downloaded files." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Card>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit user' : 'Add user'} onClose={onClose}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add user'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Card title="Profile">
      <form onSubmit={save} className="grid" style={{ gap: 14, maxWidth: 480 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        {status?.auth_mode === 'local' && <>
          <hr />
          <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
          <Field label="New password" help="At least 8 characters. Leave empty to keep the current one."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
        </>}
        <div><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Card>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Card title="API tokens">
      <p className="muted">Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you. Good for a Homepage widget (<code>GET /api/v1/dashboard</code>), an iOS Shortcut that queues an episode, or the PinePods importer. See <code>docs/api.md</code>.</p>
      <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary">Create</button></form>
      {fresh && <div className="callout success mb"><b>Copy it now — it is shown once.</b><pre className="mono" style={{ margin: '8px 0 0' }}>{fresh}</pre></div>}
      {data && data.length > 0 ? (
        <table className="table compact">
          <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
          <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? ago(t.last_used_at) : 'never'}</td><td className="muted">{t.created_at.slice(0, 10)}</td><td className="right"><button className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
        </table>
      ) : <p className="muted">No tokens yet.</p>}
      {del && <Confirm title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
    </Card>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importOPML = async (file: File) => {
    setBusy(true)
    try { const r = await upload<{ added: number; skipped: number; failed: string[] }>(`${V1}/import/opml`, 'file', file); toast(`${r.added} added, ${r.skipped} already followed${r.failed.length ? `, ${r.failed.length} failed` : ''}`); if (r.failed.length) console.warn(r.failed) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<{ added: number; existing: number; progress: number; failed: string[] }>(`${V1}/import`, JSON.parse(await file.text())); toast(`${r.added} subscriptions added, ${r.progress} episode positions restored${r.failed.length ? `, ${r.failed.length} failed` : ''}`) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="two-col">
      {user?.role === 'admin' && <Card title="Backup">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>) — every account, so only admins can take it. Media files live next to it in <code>media/</code>, artwork in <code>artwork/</code>. Restore by stopping the app and replacing <code>pocket-notes.db</code>.</p>
        <a className="btn primary" href={`${V1}/backup`}>Download database</a>
      </Card>}
      <Card title="Export & import">
        <p className="muted">OPML carries subscriptions and works with every podcast app. JSON also carries your progress, saved and queued episodes and playlists, and can be imported again here.</p>
        <div className="row mb wrap"><a className="btn" href={`${V1}/export.opml`}>Export OPML</a><a className="btn" href={`${V1}/export.json`}>Export JSON</a></div>
        <div className="row wrap">
          <label className="btn ghost">{busy ? 'Importing…' : 'Import OPML…'}<input type="file" accept=".opml,.xml,text/xml,text/x-opml" hidden onChange={e => e.target.files?.[0] && importOPML(e.target.files[0])} /></label>
          <label className="btn ghost">{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label>
        </div>
        <p className="muted small mt">Coming from PinePods? <code>scripts/import-pinepods.py</code> reads its database and produces a JSON file for this import, with your history and positions.</p>
      </Card>
    </div>
  )
}

function ActivityTab() {
  const [kind, setKind] = useState('')
  const { data, loading } = useAsync(() => api.get<Activity[]>(`${V1}/activity?kind=${kind}&limit=200`), [kind])
  return (
    <Card title="Activity" actions={<Segmented size="sm" value={kind} onChange={setKind} options={[{ value: '', label: 'All' }, { value: 'refresh', label: 'New episodes' }, { value: 'download', label: 'Downloads' }, { value: 'error', label: 'Errors' }]} />}>
      <p className="muted small">What the app did on its own for you: feed refreshes with new episodes, downloads, files removed by retention, notifications and errors. Kept for 90 days.</p>
      {loading && !data ? <Loading /> : !data || data.length === 0 ? <p className="muted">Nothing yet.</p> : (
        <ul className="activity">{data.map(a => <li key={a.id}><span className={`k ${a.kind}`}>{a.kind}</span><span className="grow">{a.message}</span><span className="when" title={a.created_at}>{ago(a.created_at)}</span></li>)}</ul>
      )}
    </Card>
  )
}

function About() {
  const { data } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const counts = (data?.counts || {}) as Record<string, number>
  const core = (data?.core || {}) as { configured: boolean; url: string; registered: boolean; error: string }
  return (
    <Card title="About">
      <p><b>Pocket Notes</b> is a self-hosted podcast server: subscriptions, a player that remembers where you were, smart playlists, downloads on this box, YouTube channels as audio podcasts with SponsorBlock, and public feeds for any podcast app. One binary, one SQLite file, one volume.</p>
      <p className="muted small">Open source (MIT) — <a href="https://github.com/abjelosevic88/pocket-homelab-notes" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-notes</a>. One of the Pocket apps, alongside Bookshelf, Day, Habits, Kitchen and the rest.</p>
      {data && (
        <table className="table compact mt">
          <tbody>
            <tr><th>Version</th><td className="mono">{String(data.version)}</td></tr>
            <tr><th>Runtime</th><td className="mono">{String(data.go)} · {String(data.os)}/{String(data.arch)}</td></tr>
            <tr><th>yt-dlp</th><td className="mono">{data.yt_dlp ? String(data.yt_dlp) : <span className="bad">not available</span>}</td></tr>
            <tr><th>Auth mode</th><td className="mono">{String(data.auth_mode)}</td></tr>
            <tr><th>Database</th><td className="mono">{String(data.db_path)} · {bytes(Number(data.db_bytes))}</td></tr>
            <tr><th>Media</th><td className="mono">{String(data.media_dir)} · {String(data.media_files)} files · {bytes(Number(data.media_bytes))}</td></tr>
            <tr><th>Feeds</th><td className="mono">checked every {String(data.refresh_minutes)} min by default · {String(data.download_workers)} download worker{Number(data.download_workers) === 1 ? '' : 's'}</td></tr>
            <tr><th>Timezone</th><td className="mono">{String(data.timezone)}</td></tr>
            <tr><th>Uptime</th><td className="mono">{String(data.uptime)}</td></tr>
            <tr><th>Pocket Core</th><td className="mono">{core.configured ? (core.registered ? `registered at ${core.url}` : `not registered: ${core.error}`) : 'not configured'}</td></tr>
            <tr><th>Yours</th><td className="mono">{counts.podcasts} podcasts · {counts.episodes} episodes · {counts.downloaded} on server · {counts.played} played{counts.users !== undefined && <> · {counts.users} accounts on this box</>}</td></tr>
          </tbody>
        </table>
      )}
    </Card>
  )
}
