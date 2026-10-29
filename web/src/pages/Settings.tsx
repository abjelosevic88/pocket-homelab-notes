import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, Integration, Settings as SettingsT, StorageInfo, StorageKind, User } from '../lib/types'
import { Confirm, CopyField, Field, Loading, Modal, NeedsCore, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { Avatar } from '../components/people'
import { ago, bytes } from '../lib/format'

type TabId = 'writing' | 'daily' | 'storage' | 'integrations' | 'api' | 'data' | 'notify' | 'users' | 'profile' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'writing', label: 'Writing', icon: 'edit', sub: 'The editor, where new notes go, templates, pictures, the trash and history.' },
  { id: 'daily', label: 'Daily notes', icon: 'today', sub: 'The note each day gets, and the lines captured into it.' },
  { id: 'storage', label: 'Storage', icon: 'database', sub: 'Where your notes are kept: here, in Nextcloud or Pocket Files, on WebDAV or in a folder.' },
  { id: 'integrations', label: 'Integrations', icon: 'layers', sub: 'The services and Pocket apps the editor’s homelab tools read from.' },
  { id: 'api', label: 'Apps & API', icon: 'smartphone', sub: 'The Nextcloud Notes apps, the Pocket Homelab app, Shortcuts and scripts, and their tokens.' },
  { id: 'data', label: 'Import & export', icon: 'archive', sub: 'All your notes as a zip, Markdown files or a vault brought in, the database for admins.' },
  { id: 'notify', label: 'Notifications', icon: 'bell', sub: 'Where a message goes when your notes folder stops answering.' },
  { id: 'users', label: 'Users', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// other names the sections went by
const ALIAS: Record<string, TabId> = { people: 'users', tokens: 'api', apps: 'integrations', import: 'data', export: 'data', notifications: 'notify', general: 'writing' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  // Old links (/settings?tab=integrations) land on their section.
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = TABS.find(t => t.id === (ALIAS[want] || want))?.id || 'writing'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { if (legacy && !raw) navigate(`/settings/${tab}`, { replace: true }) }, [legacy, raw, tab, navigate])

  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (e.key === 'Escape' && !t.closest?.('input, textarea') && !document.querySelector('.modal-backdrop, .menu, .select-pop')) navigate('/')
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to the notes" title="Back to the notes (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'writing' && <Writing />}
            {tab === 'daily' && <Daily />}
            {tab === 'storage' && <Storage />}
            {tab === 'integrations' && <Integrations />}
            {tab === 'api' && <div className="stack loose"><Clients /><Tokens /></div>}
            {tab === 'data' && <Data />}
            {tab === 'notify' && <Notify />}
            {tab === 'users' && <Users />}
            {tab === 'profile' && <Profile />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-token-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
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

function FolderInput({ value, onChange, placeholder, width = 220 }: { value: string; onChange: (v: string) => void; placeholder?: string; width?: number }) {
  const { folders } = useApp()
  return <><input value={value} onChange={e => onChange(e.target.value)} placeholder={placeholder} list="pn-folders" style={{ width }} /><datalist id="pn-folders">{folders.map(f => <option key={f.path} value={f.path} />)}</datalist></>
}

const SaveBar = ({ onSave, busy, label = 'Save changes' }: { onSave: () => void; busy: boolean; label?: string }) => <div className="row"><button type="button" className="btn primary" onClick={onSave} disabled={busy}>{busy ? 'Saving…' : label}</button></div>

function Writing() {
  const { f, setF, save, busy } = useSettingsForm()
  const { user } = useApp()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Editing">
        <SettingRow title="Editor" help="Rich: the note is edited as it reads (the same editor as the Pocket Homelab app). Markdown: every character of the source. Switch any time from a note's header.">
          <Segmented size="sm" value={f.editor} onChange={v => setF({ ...f, editor: v })} options={[{ value: 'rich', label: 'Rich' }, { value: 'markdown', label: 'Markdown' }]} />
        </SettingRow>
        <SettingRow title="Title follows the first line" help="As in Nextcloud Notes: when you change a note's first line, its file is renamed to match.">
          <Switch checked={f.title_from_first_line} onChange={v => setF({ ...f, title_from_first_line: v })} label="Title follows the first line" />
        </SettingRow>
        <SettingRow title="Spell check"><Switch checked={f.spellcheck} onChange={v => setF({ ...f, spellcheck: v })} label="Spell check" /></SettingRow>
        <SettingRow title="Dates in templates">
          <Select value={f.date_format} onChange={v => setF({ ...f, date_format: v })} label="Dates in templates" options={[{ value: 'iso', label: '2026-10-20' }, { value: 'dmy-dot', label: '20.10.2026' }, { value: 'long', label: 'Tuesday, 20 October 2026' }]} />
        </SettingRow>
      </Section>
      <Section title="New notes">
        <SettingRow title="New notes go to" help="A folder, e.g. Quick Notes. Empty: the top of the notes folder."><FolderInput value={f.default_folder} onChange={v => setF({ ...f, default_folder: v })} placeholder="(top level)" /></SettingRow>
        <SettingRow title="File type"><Segmented size="sm" value={f.file_suffix} onChange={v => setF({ ...f, file_suffix: v })} options={[{ value: '.md', label: '.md' }, { value: '.txt', label: '.txt' }]} /></SettingRow>
        <SettingRow title="Sort notes by"><Segmented size="sm" value={f.sort_by} onChange={v => setF({ ...f, sort_by: v })} options={[{ value: 'modified', label: 'Edited' }, { value: 'created', label: 'Created' }, { value: 'title', label: 'Title' }]} /></SettingRow>
        <SettingRow title="Templates folder" help="Notes in it are your own templates (/ → My templates). {{date}}, {{time}}, {{weekday}}, {{title}} are filled in."><FolderInput value={f.templates_folder} onChange={v => setF({ ...f, templates_folder: v })} placeholder="Templates" /></SettingRow>
        <SettingRow title="Pictures and files go to" help="A folder for everything put into notes (e.g. attachments). Empty: beside each note in .attachments.<id>, as Nextcloud Notes keeps them."><FolderInput value={f.attachments_folder} onChange={v => setF({ ...f, attachments_folder: v })} placeholder="(beside the note)" /></SettingRow>
      </Section>
      <Section title="Keeping">
        <SettingRow title="Keep deleted notes" help="Days in the trash before a note is gone."><input type="number" min={0} max={3650} value={f.trash_days} onChange={e => setF({ ...f, trash_days: Number(e.target.value) })} style={{ width: 100 }} aria-label="Days" /></SettingRow>
        <SettingRow title="Versions kept per note"><input type="number" min={10} max={10000} value={f.versions_keep} onChange={e => setF({ ...f, versions_keep: Number(e.target.value) })} style={{ width: 100 }} aria-label="Versions" /></SettingRow>
        <SettingRow title="Look for changes made elsewhere" help="Every so many minutes; also whenever a list is shown or a note is opened."><input type="number" min={1} max={1440} value={f.sync_minutes} onChange={e => setF({ ...f, sync_minutes: Number(e.target.value) })} style={{ width: 100 }} aria-label="Minutes" /></SettingRow>
      </Section>
      {user?.role === 'admin' && <Section title="This box" sub="Applies to everyone on this box.">
        <SettingRow title="App title" help="Shown in the sidebar and the browser tab."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} style={{ width: 220 }} aria-label="App title" /></SettingRow>
      </Section>}
      <SaveBar onSave={() => save()} busy={busy} />
    </div>
  )
}

function Daily() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  const sample = (() => { try { return goFormat(f.daily_format, new Date()) } catch { return '' } })()
  return (
    <div className="stack loose">
      <Section title="Daily notes" sub="Today (t) opens the day's note, made from this the first time.">
        <SettingRow title="Folder"><FolderInput value={f.daily_folder} onChange={v => setF({ ...f, daily_folder: v })} placeholder="Daily" /></SettingRow>
        <SettingRow title="Title" help={<>Go date layout: <code>2006-01-02</code> → {sample}. <code>Monday 2 January 2006</code>, <code>02.01.2006</code> work too.</>}><input value={f.daily_format} onChange={e => setF({ ...f, daily_format: e.target.value })} style={{ width: 220 }} aria-label="Title" /></SettingRow>
        <SettingRow stack title="A new day starts with" help="{{title}}, {{date}}, {{weekday}}, {{yesterday}}, {{tomorrow}} are filled in."><textarea rows={8} className="mono" value={f.daily_template} onChange={e => setF({ ...f, daily_template: e.target.value })} aria-label="Daily template" /></SettingRow>
      </Section>
      <Section title="Quick capture" sub="“Capture to today” in the sidebar, the API and a Shortcut or Raycast command add a line to today's daily note.">
        <SettingRow title="A captured line" help="{time} is the time (9:12), {date} the day, {text} what was captured."><input className="mono" value={f.capture_format} onChange={e => setF({ ...f, capture_format: e.target.value })} style={{ width: 220 }} aria-label="A captured line" /></SettingRow>
        <SettingRow stack title="From a script">
          <pre className="mono small">{`curl -X POST ${location.origin}/api/v1/capture \\
  -H 'Authorization: Bearer <token>' \\
  -H 'Content-Type: text/plain' --data 'Call the plumber'`}</pre>
        </SettingRow>
      </Section>
      <SaveBar onSave={() => save()} busy={busy} />
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

const KINDS: { kind: StorageKind; title: string; sub: string; icon: string }[] = [
  { kind: 'builtin', title: 'In Pocket Notes', sub: 'Kept in Pocket Notes’ own data and backed up with it. Nothing to set up.', icon: 'server' },
  { kind: 'nextcloud', title: 'Nextcloud', sub: 'A folder in your Nextcloud — the one Nextcloud Notes uses, if you like. Both see the same notes.', icon: 'cloud' },
  { kind: 'files', title: 'Pocket Files', sub: 'A folder in your Pocket Files, through Pocket Core. Nothing to type.', icon: 'folder' },
  { kind: 'webdav', title: 'WebDAV', sub: 'Any WebDAV folder: ownCloud, a NAS, Synology, Seafile…', icon: 'globe' },
  { kind: 'folder', title: 'Your own folder', sub: 'A folder the server shares with Pocket Notes (a NAS mount, Syncthing, Obsidian vault).', icon: 'folder-open' },
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
    if (data) setF({ kind: data.kind, path: data.path, url: data.url, user: data.user, password: '', folder: data.folder || 'Notes', via_core: data.core && (data.kind === 'nextcloud' ? data.via_core : !!data.core_nextcloud) })
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
  // Pocket Files is always listed; without Pocket Core it is greyed out with a word on why.
  const kinds = KINDS.filter(k => k.kind !== 'folder' || (data.roots && data.roots.length))
  return (
    <div className="stack loose">
      <Section title="Where your notes are" actions={<button type="button" className="btn sm" onClick={syncNow} disabled={syncing}><Icon name="refresh" size={14} />{syncing ? 'Syncing…' : 'Sync now'}</button>}>
        <div className="storage-where">
          <span className="acct-ico"><Icon name={KINDS.find(k => k.kind === data.kind)?.icon || 'database'} size={18} /></span>
          <div className="grow">
            <div className="row"><b>{st.where || '—'}</b><span className={`set-status ${st.error ? 'error' : 'ok'}`} /></div>
            {!st.error && <div className="muted small">Last looked {st.last_sync ? ago(st.last_sync) : 'never'}{st.stats ? ` · ${st.stats.files} notes${st.stats.took ? ` · took ${st.stats.took}` : ''}` : ''}</div>}
          </div>
        </div>
        {st.error ? <div className="callout danger mt"><Icon name="alert" size={15} /><span>{st.error}</span></div> : <p className="muted small mt">Changes made by other apps in that folder (Nextcloud Notes, your phone, a text editor) come in every few minutes and when you open a note; each one is kept in the note's history.</p>}
      </Section>
      <Section title="Change where they are" sub="Test a place first: it says what is there. Nothing moves until you choose.">
        <div className="storage-kinds">
          {kinds.map(k => (
            <button type="button" key={k.kind} className={`storage-kind ${f.kind === k.kind ? 'active' : ''}`} aria-pressed={f.kind === k.kind} disabled={k.kind === 'files' && !data.core} onClick={() => set({ kind: k.kind })}><Icon name={k.icon} size={18} /><b>{k.title}</b><small>{k.sub}</small></button>
          ))}
        </div>
        {!data.core && <NeedsCore className="mt" />}
        <div className="stack mt" style={{ maxWidth: 620 }}>
          {f.kind === 'nextcloud' && <>
            <Field group label="Account"><Segmented value={f.via_core && data.core ? 'core' : 'own'} onChange={v => set({ via_core: v === 'core' })} options={[{ value: 'core', label: 'Through Pocket Core', disabled: !data.core }, { value: 'own', label: 'Type it in' }]} /></Field>
            {f.via_core && data.core ? (
              data.core_nextcloud ? <p className="muted">Pocket Core connects you as <b>{data.core_nextcloud.user}</b> at {data.core_nextcloud.url}. Nothing to type.</p> : <div className="callout warning"><Icon name="alert" size={15} /><span>Pocket Core has no Nextcloud account for you{data.core_nextcloud_error ? `: ${data.core_nextcloud_error}` : ''}. Add one in Core → Services, or type it in.</span></div>
            ) : <>
              <Field label="Nextcloud address"><input value={f.url} onChange={e => set({ url: e.target.value })} placeholder="https://cloud.example.com" spellCheck={false} /></Field>
              <div className="two-col tight">
                <Field label="Username"><input value={f.user} onChange={e => set({ user: e.target.value })} autoComplete="off" /></Field>
                <Field label="App password" help="Nextcloud → Settings → Security → Devices & sessions."><input type="password" value={f.password} onChange={e => set({ password: e.target.value })} placeholder={data.password_set && data.kind === 'nextcloud' ? 'saved — type to replace' : ''} autoComplete="new-password" /></Field>
              </div>
            </>}
            <Field label="Folder in your Nextcloud" help="Nextcloud Notes keeps notes in “Notes” unless its settings say otherwise (yours may be e.g. Nextcloud/Notes)."><input value={f.folder} onChange={e => set({ folder: e.target.value })} placeholder="Notes" /></Field>
          </>}
          {f.kind === 'files' && <>
            {!data.core ? <div className="callout warning"><Icon name="alert" size={15} /><span>Pocket Files is reached through Pocket Core, and this server is not connected to one: choose another place.</span></div>
              : data.core_files?.on ? <p className="muted">Pocket Core lets Pocket Notes into your Pocket Files{data.core_files.url ? <> at {data.core_files.url}</> : ''}. Nothing to type. Notes deleted here go to your Pocket Files trash.</p>
              : <div className="callout warning"><Icon name="alert" size={15} /><span>Pocket Core does not let Pocket Notes into your Pocket Files{data.core_files?.reason ? `: ${data.core_files.reason}` : ''}. The link is “Pocket Notes → Pocket Files” in Core → Links.</span></div>}
            <Field label="Folder in your Pocket Files" help="A folder of your own there (not “Shared with me” or “Mounts” themselves); made when it is not there yet."><input value={f.folder} onChange={e => set({ folder: e.target.value })} placeholder="Notes" /></Field>
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
          {test && <div className="callout success"><Icon name="check-circle" size={15} /><span>It answers: <b>{test.where}</b> holds {test.notes} note{test.notes === 1 ? '' : 's'} in {test.folders} folder{test.folders === 1 ? '' : 's'}{test.files ? ` and ${test.files} other files` : ''}.</span></div>}
          <div className="row"><button type="button" className="btn" onClick={runTest} disabled={busy}>{busy ? 'Looking…' : 'Test'}</button><button type="button" className="btn primary" onClick={save} disabled={busy}>Use this</button></div>
        </div>
      </Section>
      {ask && test && <Modal title="Your notes, and this place" onClose={() => setAsk(false)} footer={<button type="button" className="btn" onClick={() => setAsk(false)}>Cancel</button>}>
        <p>{test.where} already holds <b>{test.notes}</b> note{test.notes === 1 ? '' : 's'}. What should happen to the notes you have now?</p>
        <div className="stack tight">
          <button type="button" className="storage-kind" onClick={() => apply('move')}><Icon name="copy" size={18} /><b>Copy my notes there</b><small>Every note and file goes over (a name already taken there gets “(copy)”). History, stars and pins come along. The old place is left as it is.</small></button>
          <button type="button" className="storage-kind" onClick={() => apply('switch')}><Icon name="refresh" size={18} /><b>Start from what is there</b><small>Show the {test.notes} notes that are there. The notes you have now stay where they are, untouched; their history here is forgotten.</small></button>
        </div>
      </Modal>}
    </div>
  )
}

function Integrations() {
  const { data, reload } = useAsync(() => api.get<{ core: boolean; items: Integration[] }>(`${V1}/integrations`), [])
  const { reloadMeta } = useApp()
  if (!data) return <Loading />
  const groups: [string, string, Integration[]][] = [
    ['Services', 'Links to files, documents and issues, photos, the homelab snapshot, the week’s viewing.', data.items.filter(i => i.kind === 'svc')],
    ['Pocket apps', 'Spending, the car’s log, habits, people, books, podcast episodes and drawings.', data.items.filter(i => i.kind === 'app')],
  ]
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><span>The editor's homelab tools (<code>/</code> in a note) read from these. {data.core ? 'Through Pocket Core each person uses their own account there; type an address and key in to connect directly instead.' : 'Type in an address and a key for each one you want.'}</span></div>
      {!data.core && <NeedsCore />}
      {groups.map(([title, sub, items]) => <Section key={title} title={title} sub={sub}>{items.map(i => <IntegrationRow key={i.kind + i.name} i={i} core={data.core} onSaved={() => { reload(); reloadMeta() }} />)}</Section>)}
    </div>
  )
}

// The services' logos (dashboard-icons, as Pocket Core ships them) and the Pocket apps' own icons.
const ICON_FILES: Record<string, string> = { backrest: 'backrest.png' }
function IntegrationIcon({ i }: { i: Integration }) {
  const [broken, setBroken] = useState(false)
  const src = `/icons/${i.kind === 'app' ? `pocket-${i.name}.svg` : ICON_FILES[i.name] || `${i.name}.svg`}`
  if (broken) return <span className="integration-icon fallback">{i.title.replace(/^Pocket /, '').slice(0, 1)}</span>
  return <img className="integration-icon" src={src} alt="" width={34} height={34} onError={() => setBroken(true)} />
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
          {dirty && <button type="button" className="btn primary sm" onClick={save} disabled={busy}>Save</button>}
          {mode !== 'off' && <button type="button" className="btn sm" onClick={test} disabled={busy}>{busy ? 'Testing…' : 'Test'}</button>}
          <Segmented size="sm" value={mode} onChange={setMode} label={`${i.title}: how`} options={[{ value: 'core' as const, label: 'Pocket Core', disabled: !core }, { value: 'direct' as const, label: 'Direct' }, { value: 'off' as const, label: 'Off' }]} />
        </div>
      </div>
      {result && <div className={`integration-result small ${result.ok ? 'ok' : 'bad'}`}><Icon name={result.ok ? 'check-circle' : 'x-circle'} size={14} />{result.text}</div>}
      {mode === 'direct' && <div className="integration-form">
        <input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder={i.kind === 'app' ? `https://${i.name}.example.com` : 'Address (http://… this server reaches)'} spellCheck={false} aria-label="Address" />
        <input value={f.public_url} onChange={e => setF({ ...f, public_url: e.target.value })} placeholder="Public address for links (optional)" spellCheck={false} aria-label="Public address" />
        {i.user_label && <input value={f.user} onChange={e => setF({ ...f, user: e.target.value })} placeholder={i.user_label} autoComplete="off" aria-label={i.user_label} />}
        {i.key_label && <input type="password" value={f.key} onChange={e => setF({ ...f, key: e.target.value })} placeholder={i.key_set ? `${i.key_label} — saved, type to replace` : i.key_label} autoComplete="new-password" aria-label={i.key_label} />}
      </div>}
    </div>
  )
}

function Clients() {
  const { user } = useApp()
  return (
    <Section title="Nextcloud Notes apps and the Pocket Homelab app" icon="smartphone" sub="Pocket Notes answers Nextcloud Notes' own API, so the Nextcloud Notes apps (Android, iOS) and the Pocket Homelab app's Notes work against it. Your notes keep the ids they had in Nextcloud when they live there, so links and podcast notes keep working.">
      <SettingRow title="Server"><CopyField value={location.origin} label="Server" /></SettingRow>
      <SettingRow title="Username"><CopyField value={user?.email || ''} label="Username" /></SettingRow>
      <SettingRow title="Password" help="An API token from below, made for that device."><span /></SettingRow>
      <p className="muted small mt">Shortcuts, Raycast and scripts: <code>POST /api/v1/capture</code> adds to today's note, <code>POST /api/v1/notes</code> makes one, <code>GET /api/v1/search?q=</code> finds them. Homepage: <code>GET /api/v1/dashboard</code>. See docs/api.md.</p>
    </Section>
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
    <Section title="API tokens" icon="key" sub={<>A token acts as you: <code>Authorization: Bearer &lt;token&gt;</code>, or as the password of a Nextcloud Notes app.</>}>
      <form onSubmit={create} className="row"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. iPhone, Homepage)" aria-label="Token name" /><button type="submit" className="btn primary"><Icon name="plus" size={15} />Create</button></form>
      {fresh && <div className="set-fresh"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><CopyField value={fresh} label="New token" /></div>}
      {data && data.length > 0 ? (
        <div className="set-people mt">
          {data.map(t => (
            <div key={t.id} className="set-person">
              <span className="set-token-ico"><Icon name="key" size={16} /></span>
              <div className="grow"><div className="set-person-name">{t.name}<code className="muted small">{t.prefix}…</code></div><div className="muted small">Used {t.last_used_at ? ago(t.last_used_at) : 'never'} · made {t.created_at.slice(0, 10)}</div></div>
              <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button>
            </div>
          ))}
        </div>
      ) : <p className="muted mt">No tokens yet.</p>}
      {del && <Confirm title={`Revoke “${del.name}”?`} confirmLabel="Revoke" message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
    </Section>
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
    <div className="stack loose">
      <Section title="Export" icon="download" sub="Your whole notes folder as a zip: every note as a Markdown file in its folder, with the pictures and files kept with them. Opens in Obsidian, Nextcloud Notes, any editor.">
        <a className="btn primary" href={`${V1}/export.zip`}><Icon name="download" size={15} />Download all notes (.zip)</a>
      </Section>
      <Section title="Import" icon="upload" sub="Markdown or text files, or a zip of a notes folder (Obsidian vault, Nextcloud Notes, Bear/Joplin Markdown export): folders and pictures are kept.">
        <SettingRow title="Into the folder"><input value={into} onChange={e => setInto(e.target.value)} placeholder="(top level)" style={{ width: 220 }} aria-label="Into the folder" /></SettingRow>
        <div className="row mt-s"><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Choose files…'}<input type="file" multiple accept=".md,.txt,.markdown,.zip,text/markdown,text/plain,application/zip" hidden onChange={e => { const f = Array.from(e.target.files || []); e.target.value = ''; if (f.length) importFiles(f) }} /></label></div>
      </Section>
      {user?.role === 'admin' && <Section title="Database" icon="database" sub="The index, history, trash, links and settings of every account — admins only. The notes themselves are the files above.">
        <a className="btn" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
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
    <Section title="When your notes folder stops answering" sub={<>A message goes here once when syncing with your storage keeps failing (a revoked app password, Nextcloud down).{f.notify_env && !f.notify_url && ' An address is set through NOTIFY_URL.'}</>}>
      <SettingRow title="Send through"><Segmented size="sm" value={f.notify_kind} onChange={v => setF({ ...f, notify_kind: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'webhook', label: 'Webhook' }]} /></SettingRow>
      <SettingRow stack title="Address" help={hints[f.notify_kind]}><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://…" spellCheck={false} aria-label="Address" /></SettingRow>
      <SettingRow stack title="Token (optional)" help="Sent as a bearer token."><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={f.notify_token_set ? 'saved — type to replace' : ''} autoComplete="off" aria-label="Token" /></SettingRow>
      <div className="row mt-s"><button type="button" className="btn primary" onClick={() => save(token ? { notify_token: token } : {}).then(() => setToken(''))} disabled={busy}>Save</button><button type="button" className="btn" onClick={test} disabled={testing || !(f.notify_url || f.notify_env)}><Icon name="bell" size={14} />{testing ? 'Sending…' : 'Send a test'}</button></div>
    </Section>
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
    <Section title="People" sub="Everyone signs in with their own account and has their own notes, storage, history and settings — admins included; nobody can read anyone else's notes. Admins manage the accounts and the app title."
      actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add user</button>}>
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar user={u} size="lg" />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}<span className={`badge ${u.role === 'admin' ? 'accent' : ''}`}>{u.role}</span></div>
              <div className="muted small">{u.email}</div>
            </div>
            {isAdmin && <span className="row"><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm title={`Remove ${del.name || del.email}?`} confirmLabel="Remove" message="Their account and index are deleted. Their note files stay where they are (built-in notes stay in data/notes)." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
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
      <form onSubmit={save} className="stack">
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add user'}</button></div>
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
    <form onSubmit={save} className="stack loose">
      <Section title="You">
        <div className="set-profile">
          {user && <Avatar user={user} size="lg" />}
          <div className="stack grow" style={{ maxWidth: 440 }}>
            <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
            <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
          </div>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="At least 8 characters. Leave empty to keep the current one.">
        <div className="stack" style={{ maxWidth: 440 }}>
          <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
          <Field label="New password"><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
        </div>
      </Section>}
      <div className="row"><button type="submit" className="btn primary" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button></div>
    </form>
  )
}

function About() {
  const { data } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const counts = (data?.counts || {}) as Record<string, number>
  const core = (data?.core || {}) as { configured: boolean; url: string; registered: boolean; error: string }
  return (
    <div className="stack loose">
      <Section title="This app">
        <div className="set-about-hero mb"><img src="/favicon.svg" alt="" width={44} height={44} /><div><h3>Pocket Notes</h3><p className="muted small">{data ? `Version ${String(data.version)}` : ''}</p></div></div>
        <p>Pocket Notes keeps your notes as plain Markdown files where you choose — on this server, in your Nextcloud or Pocket Files, on any WebDAV server or in a folder — and adds full-text search, tags, links between notes, tasks, daily notes, version history, a trash, shared links, a rich editor with homelab tools, and Nextcloud Notes' own API for its apps.</p>
        <p className="muted small">Open source (MIT) — <a href="https://github.com/abjelosevic88/pocket-homelab-notes" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-notes</a>. One of the Pocket apps.</p>
      </Section>
      {data && <>
        <div className="set-stats">
          <div className="set-stat"><Icon name="note" size={16} /><span className="set-stat-n">{counts.notes ?? 0}</span><span className="set-stat-l">notes</span></div>
          <div className="set-stat"><Icon name="hash" size={16} /><span className="set-stat-n">{counts.tags ?? 0}</span><span className="set-stat-l">tags</span></div>
          <div className="set-stat"><Icon name="history" size={16} /><span className="set-stat-n">{counts.versions ?? 0}</span><span className="set-stat-l">versions</span></div>
          <div className="set-stat"><Icon name="share" size={16} /><span className="set-stat-n">{counts.shares ?? 0}</span><span className="set-stat-l">shared links</span></div>
        </div>
        <Section title="The server">
          <dl className="set-dl">
            <div><dt>Version</dt><dd className="mono">{String(data.version)}</dd></div>
            <div><dt>Runtime</dt><dd className="mono">{String(data.go)} · {String(data.os)}/{String(data.arch)}</dd></div>
            <div><dt>Auth mode</dt><dd className="mono">{String(data.auth_mode)}</dd></div>
            <div><dt>Database</dt><dd className="mono">{String(data.db_path)} · {bytes(Number(data.db_bytes))}</dd></div>
            <div><dt>Server folders</dt><dd className="mono">{((data.storage_roots as string[] | null) || []).join(', ') || 'none offered (STORAGE_ROOTS)'}</dd></div>
            <div><dt>Timezone</dt><dd className="mono">{String(data.timezone)}</dd></div>
            <div><dt>Uptime</dt><dd className="mono">{String(data.uptime)}</dd></div>
            <div><dt>Pocket Core</dt><dd className="mono">{core.configured ? (core.registered ? `registered at ${core.url}` : `not registered: ${core.error}`) : 'not configured'}</dd></div>
            <div><dt>Yours</dt><dd className="mono">{counts.notes} notes · {counts.tags} tags · {counts.versions} versions · {counts.trash} in the trash · {counts.shares} shared links{counts.users !== undefined && <> · {counts.users} accounts on this box</>}</dd></div>
          </dl>
        </Section>
      </>}
    </div>
  )
}
