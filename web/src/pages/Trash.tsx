import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note, TrashItem } from '../lib/types'
import { ago, stamp } from '../lib/format'
import { Confirm, Empty, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

// Deleted notes keep their text here for a while — deleted here, or found gone from the folder.
export default function Trash() {
  const { data, reload } = useAsync(() => api.get<TrashItem[]>(`${V1}/trash`), [])
  const { settings, bump } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [view, setView] = useState<TrashItem | null>(null)
  const [empty, setEmpty] = useState(false)
  const restore = async (t: TrashItem) => {
    try { const n = await api.post<Note>(`${V1}/trash/${t.id}/restore`); bump(); toast('Restored'); navigate(`/note/${n.id}`) } catch (e) { toast((e as Error).message, 'err') }
  }
  const show = async (t: TrashItem) => setView(await api.get<TrashItem>(`${V1}/trash/${t.id}`))
  return (
    <div className="page"><div className="page-inner">
      <PageHeader title="Trash" sub={`Deleted notes stay ${settings?.trash_days ?? 30} days, then they are gone. Notes that disappeared from the folder (deleted in another app) land here too.`}
        actions={data && data.length > 0 && <button type="button" className="btn danger-ghost" onClick={() => setEmpty(true)}><Icon name="trash" size={15} />Empty the trash</button>} />
      {!data ? <Loading /> : data.length === 0 ? <Empty icon="trash" title="The trash is empty" /> : (
        <div className="trash-wrap"><table className="table trash-table">
          <thead><tr><th>Note</th><th>Was in</th><th>Deleted</th><th /></tr></thead>
          <tbody>{data.map(t => (
            <tr key={t.id}>
              <td><button type="button" className="link-btn" onClick={() => show(t)}><b>{t.title}</b></button><div className="muted small ellipsis" style={{ maxWidth: 420 }}>{t.excerpt.slice(0, 140)}</div></td>
              <td className="muted">{t.category || 'Notes'}</td>
              <td className="muted" title={stamp(t.deleted_at)}>{ago(t.deleted_at)}{t.reason === 'gone' && <div className="small">gone from the folder</div>}</td>
              <td className="right"><span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn sm" onClick={() => restore(t)}><Icon name="undo" size={14} />Restore</button><button type="button" className="btn ghost sm danger-text" onClick={async () => { await api.del(`${V1}/trash/${t.id}`); reload() }}>Delete</button></span></td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
      {view && <Modal title={view.title} onClose={() => setView(null)} size="lg" footer={<><button type="button" className="btn" onClick={() => setView(null)}>Close</button><button type="button" className="btn primary" onClick={() => { setView(null); restore(view) }}><Icon name="undo" size={14} />Restore</button></>}><pre className="diff">{view.content}</pre></Modal>}
      {empty && <Confirm confirmLabel="Empty the trash" title="Empty the trash?" message="Every deleted note goes for good." onCancel={() => setEmpty(false)} onConfirm={async () => { setEmpty(false); await api.del(`${V1}/trash`); reload() }} />}
    </div></div>
  )
}
