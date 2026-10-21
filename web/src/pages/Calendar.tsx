import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note } from '../lib/types'
import { isoDay, today } from '../lib/format'
import { PageHeader, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

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
  // A day opens on the Today page, which offers to start its note when there is none.
  const open = (d: Date) => navigate(isoDay(d) === today() ? '/today' : `/today?date=${isoDay(d)}`)
  const max = Math.max(1, ...Object.values(data?.days || {}).map(d => d.edited))
  const now = today()
  return (
    <div className="page"><div className="page-inner">
      <PageHeader title="Calendar" sub={`Daily notes live in “${settings?.daily_folder || 'Daily'}”. Pick a day to open it.`} actions={<div className="dcal-nav">
        <button type="button" className="btn sm" onClick={() => { const d = new Date(); setMonth(new Date(d.getFullYear(), d.getMonth(), 1)) }}>Today</button>
        <button type="button" className="btn ghost icon-only sm" aria-label="The month before" onClick={() => setMonth(m => new Date(m.getFullYear(), m.getMonth() - 1, 1))}><Icon name="chevron-left" size={16} /></button>
        <span className="month">{month.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</span>
        <button type="button" className="btn ghost icon-only sm" aria-label="The month after" onClick={() => setMonth(m => new Date(m.getFullYear(), m.getMonth() + 1, 1))}><Icon name="chevron-right" size={16} /></button>
      </div>} />
      <div className="dcal">
        {['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'].map(d => <div key={d} className="dcal-head">{d}</div>)}
        {cells.map((d, i) => {
          if (!d) return <div key={i} className="dcal-cell empty" />
          const info = data?.days[isoDay(d)]
          return (
            <button type="button" key={i} className={`dcal-cell ${info?.daily ? 'has' : ''} ${isoDay(d) === now ? 'today' : ''}`} onClick={() => open(d)}>
              <span className="n">{d.getDate()}</span>
              {info?.daily && <span className="pill"><Icon name="note" size={11} />daily</span>}
              {info && info.edited > 0 && <span className="heat" style={{ opacity: 0.25 + 0.75 * info.edited / max }} title={`${info.edited} note${info.edited === 1 ? '' : 's'} edited`}>{info.edited}</span>}
            </button>
          )
        })}
      </div>
    </div></div>
  )
}
