import { useEffect, useState } from 'react'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, Integration, Settings as SettingsT, StorageInfo, StorageKind, User } from '../lib/types'
import { Card, Confirm, Field, Loading, Modal, PageHeader, Segmented, Tabs, useAsync, useToast } from '../components/ui'
import { Avatar } from '../components/people'
import { ago, bytes } from '../lib/format'

type Tab = 'writing' | 'daily' | 'storage' | 'integrations' | 'api' | 'data' | 'notify' | 'users' | 'profile' | 'about'

export default function Settings() {
  const [tab, setTab] = useState<Tab>(() => (new URLSearchParams(window.location.search).get('tab') as Tab) || 'writing')
  return (
    <div className="page">
      <PageHeader title="Settings" />
      <Tabs tabs={[{ id: 'writing', label: 'Writing' }, { id: 'daily', label: 'Daily notes' }, { id: 'storage', label: 'Storage' }, { id: 'integrations', label: 'Integrations' }, { id: 'api', label: 'Apps & API' }, { id: 'data', label: 'Import & export' }, { id: 'notify', label: 'Notifications' }, { id: 'users', label: 'Users' }, { id: 'profile', label: 'Profile' }, { id: 'about', label: 'About' }]} value={tab} onChange={setTab} />
      <div className="mt">
        {tab === 'writing' && <Writing />}
        {tab === 'daily' && <Daily />}
        {tab === 'storage' && <Storage />}
        {tab === 'integrations' && <Integrations />}
        {tab === 'api' && <><Clients /><Tokens /></>}
        {tab === 'data' && <Data />}
        {tab === 'notify' && <Notify />}
        {tab === 'users' && <Users />}
        {tab === 'profile' && <Profile />}
        {tab === 'about' && <About />}
      </div>
    </div>
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

function FolderInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const { folders } = useApp()
  return <><input value={value} onChange={e => onChange(e.target.value)} placeholder={placeholder} list="pn-folders" /><datalist id="pn-folders">{folders.map(f => <option key={f.path} value={f.path} />)}</datalist></>
}

function Writing() {
  const { f, setF, save, busy } = useSettingsForm()
  const { user } = useApp()
  if (!f) return <Loading />
  return (
    <Card title="Writing">
      <div className="grid" style={{ gap: 14, maxWidth: 600 }}>
        {user?.role === 'admin' && <Field label="App title" help="Shown in the sidebar and browser tab, for everyone."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} /></Field>}
        <Field group label="Editor" help="Rich: the note is edited as it reads (the same editor as the Pocket Homelab app). Markdown: every character of the source. Switch any time from a note's header."><Segmented value={f.editor} onChange={v => setF({ ...f, editor: v })} options={[{ value: 'rich', label: 'Rich' }, { value: 'markdown', label: 'Markdown' }]} /></Field>
        <Field group label="Title follows the first line" help="As in Nextcloud Notes: when you change a note's first line, its file is renamed to match."><Segmented value={f.title_from_first_line ? 'on' : 'off'} onChange={v => setF({ ...f, title_from_first_line: v === 'on' })} options={[{ value: 'on', label: 'Yes' }, { value: 'off', label: 'No' }]} /></Field>
        <Field label="New notes go to" help="A folder, e.g. Quick Notes. Empty: the top of the notes folder."><FolderInput value={f.default_folder} onChange={v => setF({ ...f, default_folder: v })} placeholder="(top level)" /></Field>
        <Field group label="File type of new notes"><Segmented value={f.file_suffix} onChange={v => setF({ ...f, file_suffix: v })} options={[{ value: '.md', label: '.md' }, { value: '.txt', label: '.txt' }]} /></Field>
        <Field group label="Sort notes by"><Segmented value={f.sort_by} onChange={v => setF({ ...f, sort_by: v })} options={[{ value: 'modified', label: 'Edited' }, { value: 'created', label: 'Created' }, { value: 'title', label: 'Title' }]} /></Field>
        <Field group label="Dates in templates"><Segmented value={f.date_format} onChange={v => setF({ ...f, date_format: v })} options={[{ value: 'iso', label: '2026-10-20' }, { value: 'dmy-dot', label: '20.10.2026' }, { value: 'long', label: 'Tuesday, 20 October 2026' }]} /></Field>
        <Field group label="Spell check"><Segmented value={f.spellcheck ? 'on' : 'off'} onChange={v => setF({ ...f, spellcheck: v === 'on' })} options={[{ value: 'on', label: 'On' }, { value: 'off', label: 'Off' }]} /></Field>
        <Field label="Templates folder" help="Notes in it are your own templates (/ → My templates). {{date}}, {{time}}, {{weekday}}, {{title}} are filled in."><FolderInput value={f.templates_folder} onChange={v => setF({ ...f, templates_folder: v })} placeholder="Templates" /></Field>
        <Field label="Pictures and files go to" help="A folder for everything put into notes (e.g. attachments). Empty: beside each note in .attachments.<id>, as Nextcloud Notes keeps them."><FolderInput value={f.attachments_folder} onChange={v => setF({ ...f, attachments_folder: v })} placeholder="(beside the note)" /></Field>
        <div className="two-col tight">
          <Field label="Keep deleted notes (days)"><input type="number" min={0} max={3650} value={f.trash_days} onChange={e => setF({ ...f, trash_days: Number(e.target.value) })} /></Field>
          <Field label="Versions kept per note"><input type="number" min={10} max={10000} value={f.versions_keep} onChange={e => setF({ ...f, versions_keep: Number(e.target.value) })} /></Field>
        </div>
        <Field label="Look for changes made elsewhere every (minutes)" help="Also whenever a list is shown or a note is opened."><input type="number" min={1} max={1440} value={f.sync_minutes} onChange={e => setF({ ...f, sync_minutes: Number(e.target.value) })} style={{ maxWidth: 120 }} /></Field>
        <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
      </div>
    </Card>
  )
}

function Daily() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  const sample = (() => { try { return goFormat(f.daily_format, new Date()) } catch { return '' } })()
  return (
    <div className="two-col">
      <Card title="Daily notes">
        <div className="grid" style={{ gap: 14 }}>
          <Field label="Folder"><FolderInput value={f.daily_folder} onChange={v => setF({ ...f, daily_folder: v })} placeholder="Daily" /></Field>
          <Field label="Title" help={<>Go date layout: <code>2006-01-02</code> → {sample}. <code>Monday 2 January 2006</code>, <code>02.01.2006</code> work too.</>}><input value={f.daily_format} onChange={e => setF({ ...f, daily_format: e.target.value })} /></Field>
          <Field label="A new day starts with" help="{{title}}, {{date}}, {{weekday}}, {{yesterday}}, {{tomorrow}} are filled in."><textarea rows={8} className="mono" value={f.daily_template} onChange={e => setF({ ...f, daily_template: e.target.value })} /></Field>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
        </div>
      </Card>
      <Card title="Quick capture">
        <div className="grid" style={{ gap: 14 }}>
          <p className="muted">“Capture to today” in the sidebar, the API and a Shortcut or Raycast command add a line to today's daily note.</p>
          <Field label="A captured line" help="{time} is the time (9:12), {date} the day, {text} what was captured."><input className="mono" value={f.capture_format} onChange={e => setF({ ...f, capture_format: e.target.value })} /></Field>
          <pre className="mono small">{`curl -X POST ${location.origin}/api/v1/capture \\
  -H 'Authorization: Bearer <token>' \\
  -H 'Content-Type: text/plain' --data 'Call the plumber'`}</pre>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
        </div>
      </Card>
    </div>
  )
}

/** Enough of Go's layout to show a sample. */
function goFormat(layout: string, d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  const map: [string, string][] = [
    ['2006', String(d.getFullYear())], ['January', d.toLocaleDateString('en', { month: 'long' })], ['Jan', d.toLocaleDateString('en', { month: 'short' })],
    ['Monday', d.toLocaleDateString('en', { weekday: 'long' })], ['Mon', d.toLocaleDateString('en', { weekday: 'short' })],
    ['01', pad(d.getMonth() + 1)], ['02', pad(d.getDate())], ['_2', String(d.getDate()).padStart(2, ' ')], ['1', String(d.getMonth() + 1)], ['2', String(d.getDate())], ['06', String(d.getFullYear()).slice(2)],
  ]
  let out = '', i = 0
  outer: while (i < layout.length) {
    for (const [k, v] of map) if (layout.startsWith(k, i)) { out += v; i += k.length; continue outer }
    out += layout[i++]
  }
  return out
}

const KINDS: { kind: StorageKind; title: string; sub: string }[] = [
  { kind: 'builtin', title: 'In Pocket Notes', sub: 'Kept in Pocket Notes’ own data and backed up with it. Nothing to set up.' },
  { kind: 'nextcloud', title: 'Nextcloud', sub: 'A folder in your Nextcloud — the one Nextcloud Notes uses, if you like. Both see the same notes.' },
  { kind: 'webdav', title: 'WebDAV', sub: 'Any WebDAV folder: ownCloud, a NAS, Synology, Seafile…' },
  { kind: 'folder', title: 'Your own folder', sub: 'A folder the server shares with Pocket Notes (a NAS mount, Syncthing, Obsidian vault).' },
]

function Storage() {
  const { data, reload } = useAsync(() => api.get<StorageInfo>(`${V1}/storage`), [])
  const { bump, reloadTree } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ kind: 'builtin' as StorageKind, path: '', url: '', user: '', password: '', folder: 'Notes', via_core: true })
  const [test, setTest] = useState<{ notes: number; folders: number; files: number; where: string; same_place: boolean } | null>(null)
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState(false)
  const [syncing, setSyncing] = useState(false)
  useEffect(() => {
    if (data) setF({ kind: data.kind, path: data.path, url: data.url, user: data.user, password: '', folder: data.folder || 'Notes', via_core: data.kind === 'nextcloud' ? data.via_core : !!data.core_nextcloud })
  }, [data])
  if (!data) return <Loading />
  const st = data.status || {}
  const set = (p: Partial<typeof f>) => { setF({ ...f, ...p }); setTest(null) }
  const runTest = async () => {
    setBusy(true)
    try { setTest(await api.post(`${V1}/storage/test`, f)) } catch (e) { toast((e as Error).message, 'err'); setTest(null) } finally { setBusy(false) }
  }
  const apply = async (mode: 'move' | 'switch' | '') => {
    setAsk(false); setBusy(true)
    try {
      const r = await api.put<{ copied?: number; sync?: { added: number }; sync_error?: string }>(`${V1}/storage`, { ...f, mode })
      toast(r.sync_error ? `Saved, but the first sync failed: ${r.sync_error}` : mode === 'move' ? `Moved: ${r.copied ?? 0} files copied` : 'Saved', r.sync_error ? 'err' : 'ok')
      bump(); await reloadTree(); reload(); setTest(null)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const save = async () => {
    const t = test || await api.post<{ notes: number; folders: number; files: number; where: string; same_place: boolean }>(`${V1}/storage/test`, f).catch(e => { toast((e as Error).message, 'err'); return null })
    if (!t) return
    setTest(t)
    if (t.same_place) apply(''); else setAsk(true)
  }
  const syncNow = async () => {
    setSyncing(true)
    try { const s = await api.post<{ files: number; added: number; changed: number; removed: number; moved: number }>(`${V1}/sync`); toast(`${s.files} notes · ${s.added} new · ${s.changed} changed · ${s.moved} moved · ${s.removed} removed`); bump(); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setSyncing(false) }
  }
  return (
    <div className="grid" style={{ gap: 16 }}>
      <Card title="Where your notes are" actions={<button className="btn sm" onClick={syncNow} disabled={syncing}>{syncing ? 'Syncing…' : 'Sync now'}</button>}>
        <p><b>{st.where || '—'}</b></p>
        {st.error ? <div className="callout danger">{st.error}</div> : <p className="muted small">Last looked {st.last_sync ? ago(st.last_sync) : 'never'}{st.stats ? ` · ${st.stats.files} notes${st.stats.took ? ` · took ${st.stats.took}` : ''}` : ''}. Changes made by other apps in that folder (Nextcloud Notes, your phone, a text editor) come in every few minutes and when you open a note; each one is kept in the note's history.</p>}
      </Card>
      <Card title="Change where they are">
        <div className="storage-kinds">
          {KINDS.filter(k => k.kind !== 'folder' || (data.roots && data.roots.length)).map(k => (
            <button key={k.kind} className={`storage-kind ${f.kind === k.kind ? 'active' : ''}`} onClick={() => set({ kind: k.kind })}><b>{k.title}</b><small className="muted">{k.sub}</small></button>
          ))}
        </div>
        <div className="grid mt" style={{ gap: 14, maxWidth: 620 }}>
          {f.kind === 'nextcloud' && <>
            {data.core && <Field group label="Account"><Segmented value={f.via_core ? 'core' : 'own'} onChange={v => set({ via_core: v === 'core' })} options={[{ value: 'core', label: 'Through Pocket Core' }, { value: 'own', label: 'Type it in' }]} /></Field>}
            {f.via_core && data.core ? (
              data.core_nextcloud ? <p className="muted">Pocket Core connects you as <b>{data.core_nextcloud.user}</b> at {data.core_nextcloud.url}. Nothing to type.</p> : <div className="callout warning">Pocket Core has no Nextcloud account for you{data.core_nextcloud_error ? `: ${data.core_nextcloud_error}` : ''}. Add one in Core → Services, or type it in.</div>
            ) : <>
              <Field label="Nextcloud address"><input value={f.url} onChange={e => set({ url: e.target.value })} placeholder="https://cloud.example.com" spellCheck={false} /></Field>
              <div className="two-col tight">
                <Field label="Username"><input value={f.user} onChange={e => set({ user: e.target.value })} autoComplete="off" /></Field>
                <Field label="App password" help="Nextcloud → Settings → Security → Devices & sessions."><input type="password" value={f.password} onChange={e => set({ password: e.target.value })} placeholder={data.password_set && data.kind === 'nextcloud' ? 'saved — type to replace' : ''} autoComplete="new-password" /></Field>
              </div>
            </>}
            <Field label="Folder in your Nextcloud" help="Nextcloud Notes keeps notes in “Notes” unless its settings say otherwise (yours may be e.g. Nextcloud/Notes)."><input value={f.folder} onChange={e => set({ folder: e.target.value })} placeholder="Notes" /></Field>
          </>}
          {f.kind === 'webdav' && <>
            <Field label="Address of the folder" help="The full WebDAV address of the folder that holds (or will hold) the notes."><input value={f.url} onChange={e => set({ url: e.target.value })} placeholder="https://dav.example.com/remote.php/webdav/Notes" spellCheck={false} /></Field>
            <div className="two-col tight">
              <Field label="Username"><input value={f.user} onChange={e => set({ user: e.target.value })} autoComplete="off" /></Field>
              <Field label="Password"><input type="password" value={f.password} onChange={e => set({ password: e.target.value })} placeholder={data.password_set && data.kind === 'webdav' ? 'saved — type to replace' : ''} autoComplete="new-password" /></Field>
            </div>
          </>}
          {f.kind === 'folder' && <Field label="Folder" help={<>Inside one of: {(data.roots || []).map(r => <code key={r}>{r}</code>)} (STORAGE_ROOTS). Nobody else may use the same folder.</>}><input value={f.path} onChange={e => set({ path: e.target.value })} placeholder={(data.roots || [])[0] ? `${data.roots![0]}/notes` : '/notes'} spellCheck={false} /></Field>}
          {f.kind === 'builtin' && <p className="muted">Notes are files in <code>data/notes/</code> on the server.</p>}
          {test && <div className="callout success">It answers: <b>{test.where}</b> holds {test.notes} note{test.notes === 1 ? '' : 's'} in {test.folders} folder{test.folders === 1 ? '' : 's'}{test.files ? ` and ${test.files} other files` : ''}.</div>}
          <div className="row"><button className="btn" onClick={runTest} disabled={busy}>{busy ? 'Looking…' : 'Test'}</button><button className="btn primary" onClick={save} disabled={busy}>Use this</button></div>
        </div>
      </Card>
      {ask && test && <Modal title="Your notes, and this place" onClose={() => setAsk(false)}>
        <p>{test.where} already holds <b>{test.notes}</b> note{test.notes === 1 ? '' : 's'}. What should happen to the notes you have now?</p>
        <div className="grid" style={{ gap: 10 }}>
          <button className="storage-kind" onClick={() => apply('move')}><b>Copy my notes there</b><small className="muted">Every note and file goes over (a name already taken there gets “(copy)”). History, stars and pins come along. The old place is left as it is.</small></button>
          <button className="storage-kind" onClick={() => apply('switch')}><b>Start from what is there</b><small className="muted">Show the {test.notes} notes that are there. The notes you have now stay where they are, untouched; their history here is forgotten.</small></button>
        </div>
        <div className="form-actions"><button className="btn" onClick={() => setAsk(false)}>Cancel</button></div>
      </Modal>}
    </div>
  )
}

function Integrations() {
  const { data, reload } = useAsync(() => api.get<{ core: boolean; items: Integration[] }>(`${V1}/integrations`), [])
  const { reloadMeta } = useApp()
  if (!data) return <Loading />
  const groups: [string, Integration[]][] = [['Services', data.items.filter(i => i.kind === 'svc')], ['Pocket apps', data.items.filter(i => i.kind === 'app')]]
  return (
    <div className="grid" style={{ gap: 16 }}>
      <p className="muted">The editor's homelab tools (<code>/</code> in a note) read from these: links to files, documents and issues, photos, the homelab snapshot, the week's viewing, spending, the car's log, habits, tasks. {data.core ? 'Through Pocket Core each person uses their own account there; type an address and key in to connect directly instead.' : 'Type in an address and a key for each one you want.'}</p>
      {groups.map(([title, items]) => <Card key={title} title={title} flush>{items.map(i => <IntegrationRow key={i.kind + i.name} i={i} core={data.core} onSaved={() => { reload(); reloadMeta() }} />)}</Card>)}
    </div>
  )
}

// The services' logos (dashboard-icons, as Pocket Core ships them) and the Pocket apps' own icons.
const ICON_FILES: Record<string, string> = { backrest: 'backrest.png' }
function IntegrationIcon({ i }: { i: Integration }) {
  const [broken, setBroken] = useState(false)
  const src = `/icons/${i.kind === 'app' ? `pocket-${i.name}.svg` : ICON_FILES[i.name] || `${i.name}.svg`}`
  if (broken) return <span className="integration-icon fallback">{i.title.replace(/^Pocket /, '').slice(0, 1)}</span>
  return <img className="integration-icon" src={src} alt="" width={36} height={36} onError={() => setBroken(true)} />
}

function IntegrationRow({ i, core, onSaved }: { i: Integration; core: boolean; onSaved: () => void }) {
  const { toast } = useToast()
  const [mode, setMode] = useState(i.mode)
  const [f, setF] = useState({ url: i.url, public_url: i.public_url, user: i.user, key: '' })
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  const dirty = mode !== i.mode || f.url !== i.url || f.public_url !== i.public_url || f.user !== i.user || f.key !== ''
  const save = async () => {
    setBusy(true)
    try { await api.put(`${V1}/integrations/${i.kind}/${i.name}`, { mode, ...f, key: f.key ? f.key : undefined }); toast(`${i.title} saved`); setF(x => ({ ...x, key: '' })); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const test = async () => {
    setBusy(true); setResult(null)
    try { if (dirty) await save(); const r = await api.post<{ via: string }>(`${V1}/integrations/${i.kind}/${i.name}/test`); setResult({ ok: true, text: `Works (${r.via === 'core' ? 'through Pocket Core' : 'direct'})` }) } catch (e) { setResult({ ok: false, text: (e as Error).message }) } finally { setBusy(false) }
  }
  return (
    <div className="integration">
      <div className="integration-head">
        <IntegrationIcon i={i} />
        <div className="integration-text"><b>{i.title}</b><div className="muted small">{i.reason}</div></div>
        <div className="integration-actions">
          {dirty && <button className="btn primary sm" onClick={save} disabled={busy}>Save</button>}
          {mode !== 'off' && <button className="btn sm" onClick={test} disabled={busy}>{busy ? 'Testing…' : 'Test'}</button>}
          <Segmented size="sm" value={mode} onChange={setMode} options={[...(core ? [{ value: 'core' as const, label: 'Pocket Core' }] : []), { value: 'direct' as const, label: 'Direct' }, { value: 'off' as const, label: 'Off' }]} />
        </div>
      </div>
      {result && <div className={`integration-result small ${result.ok ? 'ok' : 'bad'}`}>{result.ok ? '✓ ' : '✕ '}{result.text}</div>}
      {mode === 'direct' && <div className="integration-form">
        <input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder={i.kind === 'app' ? `https://${i.name}.example.com` : 'Address (http://… this server reaches)'} spellCheck={false} />
        <input value={f.public_url} onChange={e => setF({ ...f, public_url: e.target.value })} placeholder="Public address for links (optional)" spellCheck={false} />
        {i.user_label && <input value={f.user} onChange={e => setF({ ...f, user: e.target.value })} placeholder={i.user_label} autoComplete="off" />}
        {i.key_label && <input type="password" value={f.key} onChange={e => setF({ ...f, key: e.target.value })} placeholder={i.key_set ? `${i.key_label} — saved, type to replace` : i.key_label} autoComplete="new-password" />}
      </div>}
    </div>
  )
}

function Clients() {
  const { user } = useApp()
  return (
    <Card title="Nextcloud Notes apps and the Pocket Homelab app">
      <p className="muted">Pocket Notes answers Nextcloud Notes' own API, so the Nextcloud Notes apps (Android, iOS) and the Pocket Homelab app's Notes work against it. Your notes keep the ids they had in Nextcloud when they live there, so links and podcast notes keep working.</p>
      <table className="table compact">
        <tbody>
          <tr><th>Server</th><td className="mono">{location.origin}</td></tr>
          <tr><th>Username</th><td className="mono">{user?.email}</td></tr>
          <tr><th>Password</th><td>an API token from below (made for that device)</td></tr>
        </tbody>
      </table>
      <p className="muted small mt">Shortcuts, Raycast and scripts: <code>POST /api/v1/capture</code> adds to today's note, <code>POST /api/v1/notes</code> makes one, <code>GET /api/v1/search?q=</code> finds them. Homepage: <code>GET /api/v1/dashboard</code>. See docs/api.md.</p>
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
    <Card title="API tokens" className="mt">
      <p className="muted">A token acts as you: <code>Authorization: Bearer &lt;token&gt;</code>, or as the password of a Nextcloud Notes app.</p>
      <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. iPhone, Homepage)" /><button className="btn primary">Create</button></form>
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
  const { user, bump } = useApp()
  const [busy, setBusy] = useState(false)
  const [into, setInto] = useState('Imported')
  const importFiles = async (files: File[]) => {
    setBusy(true)
    try { const r = await upload<{ notes: number; files: number; skipped: number; problems: string[] }>(`${V1}/import`, 'file', files, { category: into }); toast(`${r.notes} notes and ${r.files} files imported${r.skipped ? `, ${r.skipped} skipped` : ''}${r.problems?.length ? `, ${r.problems.length} problems` : ''}`); bump() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="two-col">
      <Card title="Export">
        <p className="muted">Your whole notes folder as a zip: every note as a Markdown file in its folder, with the pictures and files kept with them. Opens in Obsidian, Nextcloud Notes, any editor.</p>
        <a className="btn primary" href={`${V1}/export.zip`}>Download all notes (.zip)</a>
        {user?.role === 'admin' && <><hr className="mt mb" /><p className="muted">The database (index, history, trash, links, settings of every account) — admins only. The notes themselves are the files above.</p><a className="btn" href={`${V1}/backup`}>Download database</a></>}
      </Card>
      <Card title="Import">
        <p className="muted">Markdown or text files, or a zip of a notes folder (Obsidian vault, Nextcloud Notes, Bear/Joplin Markdown export): folders and pictures are kept.</p>
        <Field label="Into the folder"><input value={into} onChange={e => setInto(e.target.value)} placeholder="(top level)" /></Field>
        <label className="btn mt">{busy ? 'Importing…' : 'Choose files…'}<input type="file" multiple accept=".md,.txt,.markdown,.zip,text/markdown,text/plain,application/zip" hidden onChange={e => { const f = Array.from(e.target.files || []); e.target.value = ''; if (f.length) importFiles(f) }} /></label>
      </Card>
    </div>
  )
}

function Notify() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [token, setToken] = useState('')
  const [testing, setTesting] = useState(false)
  if (!f) return <Loading />
  const hints: Record<string, string> = { apprise: 'Apprise API route, e.g. http://apprise:8000/notify/notes', ntfy: 'ntfy topic URL, e.g. https://ntfy.example.com/notes', webhook: 'Any URL that accepts a JSON POST {title, body, url}' }
  const test = async () => {
    setTesting(true)
    try { await api.post(`${V1}/settings/notify/test`, { kind: f.notify_kind, url: f.notify_url, token }); toast('Sent — check your phone') } catch (e) { toast((e as Error).message, 'err') } finally { setTesting(false) }
  }
  return (
    <Card title="When your notes folder stops answering">
      <div className="grid" style={{ gap: 14, maxWidth: 600 }}>
        <p className="muted">A message goes here once when syncing with your storage keeps failing (a revoked app password, Nextcloud down).{f.notify_env && !f.notify_url && ' An address is set through NOTIFY_URL.'}</p>
        <Field group label="Send through"><Segmented value={f.notify_kind} onChange={v => setF({ ...f, notify_kind: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'webhook', label: 'Webhook' }]} /></Field>
        <Field label="Address" help={hints[f.notify_kind]}><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://…" spellCheck={false} /></Field>
        <Field label="Token (optional)" help="Sent as a bearer token."><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={f.notify_token_set ? 'saved — type to replace' : ''} autoComplete="off" /></Field>
        <div className="row"><button className="btn primary" onClick={() => save(token ? { notify_token: token } : {}).then(() => setToken(''))} disabled={busy}>Save</button><button className="btn" onClick={test} disabled={testing || !(f.notify_url || f.notify_env)}>{testing ? 'Sending…' : 'Send a test'}</button></div>
      </div>
    </Card>
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
      <p className="muted">Everyone signs in with their own account and has their own notes, storage, history and settings — admins included; nobody can read anyone else's notes. Admins manage the accounts and the app title.</p>
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
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm title={`Remove ${del.name || del.email}?`} message="Their account and index are deleted. Their note files stay where they are (built-in notes stay in data/notes)." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
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

function About() {
  const { data } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const counts = (data?.counts || {}) as Record<string, number>
  const core = (data?.core || {}) as { configured: boolean; url: string; registered: boolean; error: string }
  return (
    <Card title="About">
      <p><b>Pocket Notes</b> keeps your notes as plain Markdown files where you choose — on this server, in your Nextcloud, on any WebDAV server or in a folder — and adds full-text search, tags, links between notes, tasks, daily notes, version history, a trash, shared links, a rich editor with homelab tools, and Nextcloud Notes' own API for its apps.</p>
      <p className="muted small">Open source (MIT) — <a href="https://github.com/abjelosevic88/pocket-homelab-notes" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-notes</a>. One of the Pocket apps.</p>
      {data && (
        <table className="table compact mt">
          <tbody>
            <tr><th>Version</th><td className="mono">{String(data.version)}</td></tr>
            <tr><th>Runtime</th><td className="mono">{String(data.go)} · {String(data.os)}/{String(data.arch)}</td></tr>
            <tr><th>Auth mode</th><td className="mono">{String(data.auth_mode)}</td></tr>
            <tr><th>Database</th><td className="mono">{String(data.db_path)} · {bytes(Number(data.db_bytes))}</td></tr>
            <tr><th>Server folders</th><td className="mono">{((data.storage_roots as string[] | null) || []).join(', ') || 'none offered (STORAGE_ROOTS)'}</td></tr>
            <tr><th>Timezone</th><td className="mono">{String(data.timezone)}</td></tr>
            <tr><th>Uptime</th><td className="mono">{String(data.uptime)}</td></tr>
            <tr><th>Pocket Core</th><td className="mono">{core.configured ? (core.registered ? `registered at ${core.url}` : `not registered: ${core.error}`) : 'not configured'}</td></tr>
            <tr><th>Yours</th><td className="mono">{counts.notes} notes · {counts.tags} tags · {counts.versions} versions · {counts.trash} in the trash · {counts.shares} shared links{counts.users !== undefined && <> · {counts.users} accounts on this box</>}</td></tr>
          </tbody>
        </table>
      )}
    </Card>
  )
}
