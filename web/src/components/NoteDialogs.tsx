import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { DiffOp, Note, Share, Version } from '../lib/types'
import { ago, stamp } from '../lib/format'
import { Confirm, Loading, Modal, Segmented, useToast } from './ui'

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
              <li key={v.id}><button className={sel === v.id ? 'active' : ''} onClick={() => setSel(v.id)}>
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
                {list[0]?.id !== sel && <button className="btn primary sm" onClick={() => setConfirm(true)}>Restore this version</button>}
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
    navigator.clipboard?.writeText(url(s)); toast('Link made and copied'); load()
  }
  return (
    <Modal title="Share a link" onClose={onClose}>
      <p className="muted">Anyone with the link can read this note (and see the pictures in it) without an account. They cannot change it. Revoke a link to close it.</p>
      <div className="row wrap mb">
        <Segmented size="sm" value={days} onChange={setDays} options={[{ value: 0, label: 'No end' }, { value: 1, label: '1 day' }, { value: 7, label: '7 days' }, { value: 30, label: '30 days' }]} />
        <button className="btn primary" onClick={make}>Make a link</button>
      </div>
      {!list ? <Loading /> : list.length === 0 ? <p className="muted small">No links to this note yet.</p> : (
        <table className="table compact">
          <tbody>{list.map(s => (
            <tr key={s.id}>
              <td><code className="mono small">{url(s)}</code><div className="muted small">{s.views} view{s.views === 1 ? '' : 's'} · {s.expires_at ? `until ${stamp(s.expires_at)}` : 'no end'}</div></td>
              <td className="right"><span className="row" style={{ justifyContent: 'flex-end' }}>
                <button className="btn ghost sm" onClick={() => { navigator.clipboard?.writeText(url(s)); toast('Copied') }}>Copy</button>
                <a className="btn ghost sm" href={url(s)} target="_blank" rel="noreferrer">Open</a>
                <button className="btn ghost sm danger-text" onClick={async () => { await api.del(`${V1}/shares/${s.id}`); load() }}>Revoke</button>
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
      <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find or name a folder (Projects/Athena)…" className="mb" style={{ width: '100%' }} />
      <div className="tool-results">
        {fresh && <button className="tool-row" onClick={() => onMove(q.trim())}><span className="ico">＋</span><span className="grow"><b>New folder “{q.trim()}”</b></span></button>}
        {shown.map(p => (
          <button key={p || '/'} className={`tool-row ${p === current ? 'active' : ''}`} disabled={p === current} onClick={() => onMove(p)}>
            <span className="ico">📁</span><span className="grow" style={{ paddingLeft: (p.split('/').length - 1) * 14 }}><b>{p ? p.split('/').pop() : 'Notes (top level)'}</b>{p.includes('/') && <small className="muted">{p}</small>}</span>
          </button>
        ))}
      </div>
    </Modal>
  )
}
