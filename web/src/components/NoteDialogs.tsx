import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { DiffOp, Member, Note, PersonOnBox, Share, Version } from '../lib/types'
import { ago, colorFor, initials, stamp } from '../lib/format'
import { copyText } from '../lib/clipboard'
import { Confirm, Loading, Modal, Segmented, useToast } from './ui'
import { Icon } from './icons'
import { Select } from './Select'

const SOURCES: Record<string, string> = { web: 'Here', api: 'API', 'nextcloud-api': 'Nextcloud Notes app', sync: 'Changed in the folder', restore: 'Restored', import: 'First seen', capture: 'Quick capture', daily: 'Daily note', app: 'Pocket Homelab app', shortcut: 'Shortcut' }

export function HistoryModal({ note, onClose, onRestored }: { note: Note; onClose: () => void; onRestored: (n: Note) => void }) {
  const [list, setList] = useState<Version[] | null>(null)
  const [sel, setSel] = useState<number>(0)
  const [view, setView] = useState<{ version: Version; diff: DiffOp[]; added: number; removed: number } | null>(null)
  const [show, setShow] = useState<'diff' | 'text'>('diff')
  const [confirm, setConfirm] = useState(false)
  const { toast } = useToast()
  useEffect(() => { api.get<Version[]>(`${V1}/notes/${note.id}/versions`).then(v => { setList(v); if (v[1]) setSel(v[1].id); else if (v[0]) setSel(v[0].id) }) }, [note.id])
  useEffect(() => { if (sel) api.get<{ version: Version; diff: DiffOp[]; added: number; removed: number }>(`${V1}/notes/${note.id}/versions/${sel}`).then(setView) }, [sel, note.id])
  const restore = async () => {
    try { onRestored(await api.post<Note>(`${V1}/notes/${note.id}/versions/${sel}/restore`)) } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Modal title={`History of “${note.title}”`} onClose={onClose} size="xl">
      {!list ? <Loading /> : !list.length ? <p className="muted">No versions yet.</p> : (
        <div className="history">
          <ul className="history-list">
            {list.map((v, i) => (
              <li key={v.id}><button type="button" className={sel === v.id ? 'active' : ''} onClick={() => setSel(v.id)}>
                <b>{i === 0 ? 'Current' : ago(v.created_at)}</b>
                <small className="muted">{stamp(v.created_at)} · {SOURCES[v.source] || v.source}</small>
                {v.title !== note.title && <small className="muted">as “{v.title}”</small>}
              </button></li>
            ))}
          </ul>
          <div className="history-view">
            {view && <>
              <div className="row mb">
                <Segmented size="sm" value={show} onChange={setShow} options={[{ value: 'diff', label: 'Changes since' }, { value: 'text', label: 'This version' }]} />
                <span className="muted small">since then: {view.added} line{view.added === 1 ? '' : 's'} added, {view.removed} removed</span>
                <span className="spacer" />
                {list[0]?.id !== sel && <button type="button" className="btn primary sm" onClick={() => setConfirm(true)}><Icon name="undo" size={14} />Restore this version</button>}
              </div>
              {show === 'diff'
                ? <pre className="diff">{view.diff.length ? view.diff.map((o, i) => <div key={i} className={`d ${o.kind === '+' ? 'add' : o.kind === '-' ? 'del' : o.kind === '…' ? 'gap' : ''}`}>{o.kind === '…' ? '⋯' : `${o.kind === '=' ? ' ' : o.kind} ${o.text}`}</div>) : <span className="muted">Same as now.</span>}</pre>
                : <pre className="diff">{view.version.content}</pre>}
              <p className="muted small mt">“Changes since”: <span className="del-ink">red</span> lines were in this version and are gone now, <span className="add-ink">green</span> ones came later.</p>
            </>}
          </div>
        </div>
      )}
      {confirm && <Confirm danger={false} title="Restore this version?" message="The note gets this version's text. What it says now stays in the history." onCancel={() => setConfirm(false)} onConfirm={() => { setConfirm(false); restore() }} />}
    </Modal>
  )
}

export function ShareModal({ note, onClose }: { note: Note; onClose: () => void }) {
  const [list, setList] = useState<Share[] | null>(null)
  const [days, setDays] = useState(0)
  const { toast } = useToast()
  const load = () => api.get<Share[]>(`${V1}/notes/${note.id}/shares`).then(setList)
  useEffect(() => { load() }, [note.id]) // eslint-disable-line react-hooks/exhaustive-deps
  const url = (s: Share) => `${location.origin}/s/${s.token}`
  const make = async () => {
    const s = await api.post<Share>(`${V1}/notes/${note.id}/shares`, { days })
    load(); toast(await copyText(url(s)) ? 'Link made and copied' : 'Link made — copy it from the list')
  }
  return (
    <Modal title="Share a link" onClose={onClose}>
      <p className="muted">Anyone with the link can read this note (and see the pictures in it) without an account. They cannot change it. Revoke a link to close it.</p>
      <div className="row wrap mb">
        <Segmented size="sm" value={days} onChange={setDays} options={[{ value: 0, label: 'No end' }, { value: 1, label: '1 day' }, { value: 7, label: '7 days' }, { value: 30, label: '30 days' }]} />
        <button type="button" className="btn primary" onClick={make}><Icon name="link" size={15} />Make a link</button>
      </div>
      {!list ? <Loading /> : list.length === 0 ? <p className="muted small">No links to this note yet.</p> : (
        <table className="table compact">
          <tbody>{list.map(s => (
            <tr key={s.id}>
              <td><code className="mono small">{url(s)}</code><div className="muted small">{s.views} view{s.views === 1 ? '' : 's'} · {s.expires_at ? `until ${stamp(s.expires_at)}` : 'no end'}</div></td>
              <td className="right"><span className="row" style={{ justifyContent: 'flex-end' }}>
                <button type="button" className="btn ghost sm" onClick={async () => { const ok = await copyText(url(s)); toast(ok ? 'Copied' : 'Could not copy — select the link instead', ok ? 'ok' : 'err') }}>Copy</button>
                <a className="btn ghost sm" href={url(s)} target="_blank" rel="noreferrer">Open</a>
                <button type="button" className="btn ghost sm danger-text" onClick={async () => { await api.del(`${V1}/shares/${s.id}`); load() }}>Revoke</button>
              </span></td>
            </tr>
          ))}</tbody>
        </table>
      )}
    </Modal>
  )
}

export function MoveModal({ current, onMove, onClose }: { current: string; onMove: (cat: string) => void; onClose: () => void }) {
  const { folders } = useApp()
  const [q, setQ] = useState('')
  const paths = Array.from(new Set(['', ...folders.map(f => f.path)])).sort((a, b) => a.localeCompare(b))
  const shown = paths.filter(p => !q || p.toLowerCase().includes(q.toLowerCase()))
  const fresh = q.trim() && !paths.some(p => p.toLowerCase() === q.trim().toLowerCase())
  return (
    <Modal title="Move to" onClose={onClose}>
      <div className="tool-search"><Icon name="search" size={15} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find or name a folder (Projects/Athena)…" aria-label="Folder" /></div>
      <div className="tool-results">
        {fresh && <button type="button" className="tool-row" onClick={() => onMove(q.trim())}><span className="ico"><Icon name="folder-plus" size={16} /></span><span className="grow"><b>New folder “{q.trim()}”</b></span></button>}
        {shown.map(p => (
          <button type="button" key={p || '/'} className={`tool-row ${p === current ? 'active' : ''}`} disabled={p === current} onClick={() => onMove(p)}>
            <span className="ico"><Icon name={p === current ? 'folder-open' : 'folder'} size={16} /></span><span className="grow" style={{ paddingLeft: (p.split('/').length - 1) * 14 }}><b>{p ? p.split('/').pop() : 'Notes (top level)'}</b>{p.includes('/') && <small className="muted">{p}</small>}</span>
          </button>
        ))}
      </div>
    </Modal>
  )
}

const personName = (p: { name: string; email: string }) => p.name || p.email

/** Share a note with people on this server: they find it under Shared, to read or to edit. */
export function PeopleModal({ note, onClose, onChanged }: { note: Note; onClose: () => void; onChanged?: (count: number) => void }) {
  const { toast } = useToast()
  const [data, setData] = useState<{ owner: Member; members: Member[]; mine: boolean } | null>(null)
  const [people, setPeople] = useState<PersonOnBox[] | null>(null)
  const [pick, setPick] = useState<number | null>(null)
  const [edit, setEdit] = useState(true)
  const [busy, setBusy] = useState(false)
  const load = () => api.get<{ owner: Member; members: Member[]; mine: boolean }>(`${V1}/notes/${note.id}/members`).then(d => { setData(d); onChanged?.(d.members.length) })
  useEffect(() => {
    load().catch(e => toast((e as Error).message, 'err'))
    api.get<PersonOnBox[]>(`${V1}/people`).then(setPeople).catch(() => setPeople([]))
  }, [note.id]) // eslint-disable-line react-hooks/exhaustive-deps
  const set = async (body: { user_id: number; can_edit: boolean }, msg?: string) => {
    setBusy(true)
    try {
      const d = await api.post<{ owner: Member; members: Member[]; mine: boolean }>(`${V1}/notes/${note.id}/members`, body)
      setData(d); onChanged?.(d.members.length); if (msg) toast(msg)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async (m: Member) => {
    try { await api.del(`${V1}/notes/${note.id}/members/${m.user_id}`); await load(); toast(`${personName(m)} no longer has it`) } catch (e) { toast((e as Error).message, 'err') }
  }
  const taken = new Set((data?.members || []).map(m => m.user_id))
  const free = (people || []).filter(p => !taken.has(p.id))
  const mine = data?.mine
  return (
    <Modal title="Share with people" onClose={onClose}>
      <p className="muted">People on this server find the note under <b>Shared</b> in their sidebar. It stays your note, in your folder: what they write lands here, with their name in its history.</p>
      {!data ? <Loading /> : <>
        {mine && (people && free.length === 0
          ? <p className="muted small mb">{people.length === 0 ? 'Nobody else has signed in to Pocket Notes yet. Once they have, they show up here.' : 'It is shared with everyone on this server.'}</p>
          : <div className="row wrap mb share-add">
              <Select searchable className="grow" label="Person" placeholder="Choose a person…" value={pick ?? 0} onChange={v => setPick(v)} width={300}
                options={free.map(p => ({ value: p.id, label: p.name ? `${p.name} · ${p.email}` : p.email }))} />
              <Segmented size="sm" label="They can" value={edit ? 'edit' : 'read'} onChange={v => setEdit(v === 'edit')} options={[{ value: 'edit', label: 'Can edit' }, { value: 'read', label: 'Can read' }]} />
              <button type="button" className="btn primary" disabled={!pick || busy} onClick={async () => { if (pick) { const p = free.find(x => x.id === pick); await set({ user_id: pick, can_edit: edit }, `Shared with ${p ? personName(p) : 'them'}`); setPick(null) } }}><Icon name="users" size={15} />Share</button>
            </div>)}
        <ul className="member-list">
          <li><span className="avatar" style={{ background: colorFor(data.owner.user_id) }}>{initials(personName(data.owner))}</span><span className="grow"><b>{personName(data.owner)}</b>{data.owner.name && <small className="muted">{data.owner.email}</small>}</span><span className="muted small">Owner</span></li>
          {data.members.map(m => (
            <li key={m.user_id}>
              <span className="avatar" style={{ background: colorFor(m.user_id) }}>{initials(personName(m))}</span>
              <span className="grow"><b>{personName(m)}</b>{m.name && <small className="muted">{m.email}</small>}</span>
              {mine
                ? <>
                    <Segmented size="sm" label={`What ${personName(m)} can do`} value={m.can_edit ? 'edit' : 'read'} onChange={v => set({ user_id: m.user_id, can_edit: v === 'edit' })} options={[{ value: 'edit', label: 'Edit' }, { value: 'read', label: 'Read' }]} />
                    <button type="button" className="btn ghost icon-only sm" title={`Stop sharing with ${personName(m)}`} aria-label={`Stop sharing with ${personName(m)}`} onClick={() => remove(m)}><Icon name="x" size={15} /></button>
                  </>
                : <span className="muted small">{m.can_edit ? 'Can edit' : 'Can read'}</span>}
            </li>
          ))}
        </ul>
        {data.members.length === 0 && <p className="muted small">Not shared with anyone yet.</p>}
      </>}
    </Modal>
  )
}
