import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note } from '../lib/types'
import { isoDay, today } from '../lib/format'
import { PageHeader, useAsync, useToast } from '../components/ui'

// Daily notes on a month: a day with a note, how much was written on each day.
export default function Calendar() {
  const [month, setMonth] = useState(() => { const d = new Date(); return new Date(d.getFullYear(), d.getMonth(), 1) })
  const key = `${month.getFullYear()}-${String(month.getMonth() + 1).padStart(2, '0')}`
  const { data } = useAsync(() => api.get<{ days: Record<string, { daily?: number; edited: number }> }>(`${V1}/calendar?month=${key}`), [key])
  const { bump, settings } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const start = (month.getDay() + 6) % 7 // weeks start on Monday
  const days = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate()
  const cells: (Date | null)[] = [...Array(start).fill(null), ...Array.from({ length: days }, (_, i) => new Date(month.getFullYear(), month.getMonth(), i + 1))]
  const open = async (d: Date) => {
    const day = isoDay(d), info = data?.days[day]
    if (info?.daily) { navigate(`/note/${info.daily}?in=${encodeURIComponent(settings?.daily_folder || '')}`); return }
    try { const r = await api.post<{ note: Note }>(`${V1}/daily`, { date: day }); bump(); navigate(`/note/${r.note.id}?in=${encodeURIComponent(r.note.category)}`) } catch (e) { toast((e as Error).message, 'err') }
  }
  const max = Math.max(1, ...Object.values(data?.days || {}).map(d => d.edited))
  const now = today()
  return (
    <div className="page">
      <PageHeader title="Calendar" sub={`Daily notes live in “${settings?.daily_folder || 'Daily'}”. A day without one makes it from your template.`} actions={<div className="row">
        <button className="btn sm" onClick={() => setMonth(m => new Date(m.getFullYear(), m.getMonth() - 1, 1))}>‹</button>
        <b style={{ minWidth: 150, textAlign: 'center' }}>{month.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</b>
        <button className="btn sm" onClick={() => setMonth(m => new Date(m.getFullYear(), m.getMonth() + 1, 1))}>›</button>
        <button className="btn ghost sm" onClick={() => { const d = new Date(); setMonth(new Date(d.getFullYear(), d.getMonth(), 1)) }}>Today</button>
      </div>} />
      <div className="cal">
        {['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'].map(d => <div key={d} className="cal-head">{d}</div>)}
        {cells.map((d, i) => {
          if (!d) return <div key={i} className="cal-cell empty" />
          const info = data?.days[isoDay(d)]
          return (
            <button key={i} className={`cal-cell ${info?.daily ? 'has' : ''} ${isoDay(d) === now ? 'today' : ''}`} onClick={() => open(d)}>
              <span className="n">{d.getDate()}</span>
              {info?.daily && <span className="pill">daily</span>}
              {info && info.edited > 0 && <span className="heat" style={{ opacity: 0.25 + 0.75 * info.edited / max }} title={`${info.edited} note${info.edited === 1 ? '' : 's'} edited`}>{info.edited}</span>}
            </button>
          )
        })}
      </div>
    </div>
  )
}
