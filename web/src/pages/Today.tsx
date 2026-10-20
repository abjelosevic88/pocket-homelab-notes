import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1, qs } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note, TaskLine } from '../lib/types'
import { ago, isoDay, today as todayISO } from '../lib/format'
import { Loading, useToast } from '../components/ui'
import NoteView, { type NoteViewHandle } from '../components/NoteView'

// One day on one page: its note to write in, and what surrounds it — the week, the calendar,
// the habits, the tasks that are due, what was written, and the same day a week, a month and
// a year ago.

interface ShortNote { id: number; title: string; category: string; excerpt: string; modified: number; new?: boolean }
interface TodayData {
  date: string
  today: string
  title: string
  note: Note | null
  week: { date: string; daily?: number; edited: number }[]
  tasks: { overdue: TaskLine[]; today: TaskLine[]; upcoming: TaskLine[]; open: number }
  edited: ShortNote[]
  on_this_day: { label: string; date: string; note: ShortNote }[]
  recent: ShortNote[]
  capture_format: string
}
interface AgendaEvent { title: string; start: string; end?: string; all_day: boolean; location?: string; calendar: string; color?: string }
interface Habit { id: number; name: string; color: string; status: number; scheduled: boolean; streak: number }
interface Weather { condition: string; code: number; t_max?: number; t_min?: number; precip_mm?: number; place?: string }
interface HabitsDay { habits: Habit[]; done: number; total: number; weather?: Weather | null }

const day = (iso: string) => { const [y, m, d] = iso.split('-').map(Number); return new Date(y, m - 1, d) }
const shift = (iso: string, n: number) => { const d = day(iso); d.setDate(d.getDate() + n); return isoDay(d) }
const clock = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
const plainTask = (t: string) => t.replace(/(📅\s*|due:\s*)\d{4}-\d{2}-\d{2}/, '').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/[*_~`]+/g, '').trim()

function greeting(h: number) {
  if (h < 5) return 'Good night'
  if (h < 12) return 'Good morning'
  if (h < 18) return 'Good afternoon'
  return 'Good evening'
}

// WMO weather codes, as Open-Meteo gives them.
function sky(code: number) {
  if (code === 0) return '☀️'
  if (code <= 2) return '🌤️'
  if (code === 3) return '☁️'
  if (code <= 48) return '🌫️'
  if (code <= 67 || (code >= 80 && code <= 82)) return '🌧️'
  if (code <= 77 || code === 85 || code === 86) return '🌨️'
  if (code >= 95) return '⛈️'
  return '🌥️'
}

export default function Today() {
  const [params, setParams] = useSearchParams()
  const { user, bump, version } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const date = params.get('date') || todayISO()
  const [data, setData] = useState<TodayData | null>(null)
  const [err, setErr] = useState('')
  const [agenda, setAgenda] = useState<{ connected: boolean; events: AgendaEvent[]; error?: string; url?: string } | null>(null)
  const [habits, setHabits] = useState<{ connected: boolean; day?: HabitsDay; error?: string; url?: string } | null>(null)
  const [capture, setCapture] = useState('')
  const [busy, setBusy] = useState(false)
  const [now, setNow] = useState(() => new Date())
  const view = useRef<NoteViewHandle>(null)

  useEffect(() => { const t = setInterval(() => setNow(new Date()), 30000); return () => clearInterval(t) }, [])

  const load = useCallback(async () => {
    try { setData(await api.get<TodayData>(`${V1}/today${qs({ date })}`)); setErr('') } catch (e) { setErr((e as Error).message) }
  }, [date])
  useEffect(() => { load() }, [load, version])
  useEffect(() => {
    let live = true
    setAgenda(null); setHabits(null)
    api.get<typeof agenda>(`${V1}/today/agenda${qs({ date })}`).then(a => live && setAgenda(a)).catch(e => live && setAgenda({ connected: true, events: [], error: (e as Error).message }))
    api.get<typeof habits>(`${V1}/today/habits${qs({ date })}`).then(h => live && setHabits(h)).catch(e => live && setHabits({ connected: true, error: (e as Error).message }))
    return () => { live = false }
  }, [date])

  const go = (d: string) => setParams(d === todayISO() ? {} : { date: d })
  const isToday = date === todayISO()
  const d = day(date)

  const doCapture = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!capture.trim()) return
    setBusy(true)
    try {
      await view.current?.saveNow()
      await api.post(`${V1}/capture`, { text: capture, date })
      setCapture('')
      if (data?.note) await view.current?.reload()
      await load(); bump()
    } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const startNote = async () => {
    try { await api.post(`${V1}/daily`, { date }); await load(); bump() } catch (e) { toast((e as Error).message, 'err') }
  }
  const toggleTask = async (t: TaskLine) => {
    try {
      if (data?.note && t.note_id === data.note.id) await view.current?.saveNow()
      await api.post(`${V1}/notes/${t.note_id}/tasks`, { line: t.line, text: t.text, done: !t.done })
      if (data?.note && t.note_id === data.note.id) await view.current?.reload()
      await load()
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const setHabit = async (h: Habit) => {
    const next = h.status === 1 ? 0 : 1
    setHabits(x => x && x.day ? { ...x, day: { ...x.day, habits: x.day.habits.map(y => y.id === h.id ? { ...y, status: next } : y), done: x.day.done + (next === 1 ? 1 : -1) } } : x)
    try { await api.put(`${V1}/today/habits/${h.id}`, { date, status: next }) } catch (e) {
      toast((e as Error).message, 'err')
      setHabits(x => x && x.day ? { ...x, day: { ...x.day, habits: x.day.habits.map(y => y.id === h.id ? { ...y, status: h.status } : y), done: x.day.done + (next === 1 ? -1 : 1) } } : x)
    }
  }

  const firstName = (user?.name || user?.email || '').split(/[\s@]/)[0]
  const stamp = `${now.getHours()}:${String(now.getMinutes()).padStart(2, '0')}`
  const weather = habits?.day?.weather
  const tasksDue = data ? data.tasks.overdue.length + data.tasks.today.length : 0

  return (
    <div className="today page">
      <header className="today-head">
        <div className="grow">
          <div className="today-kicker">{isToday ? `${greeting(now.getHours())}${firstName ? `, ${firstName}` : ''}` : date < todayISO() ? 'Looking back' : 'Looking ahead'}</div>
          <h1>{d.toLocaleDateString(undefined, { weekday: 'long' })}</h1>
          <div className="today-date">{d.toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' })}</div>
        </div>
        {weather && <div className="weather" title={weather.place || ''}>
          <span className="sky">{sky(weather.code)}</span>
          <span><b>{weather.t_max != null ? Math.round(weather.t_max) : '–'}°</b><span className="muted"> / {weather.t_min != null ? Math.round(weather.t_min) : '–'}°</span><br /><small className="muted">{weather.condition}{weather.precip_mm ? ` · ${weather.precip_mm} mm` : ''}</small></span>
        </div>}
        <div className="row today-nav">
          <button className="btn icon" onClick={() => go(shift(date, -1))} aria-label="The day before">‹</button>
          <button className="btn sm" onClick={() => go(todayISO())} disabled={isToday}>Today</button>
          <button className="btn icon" onClick={() => go(shift(date, 1))} aria-label="The day after">›</button>
        </div>
      </header>

      {data && <div className="week-strip">
        {data.week.map(w => {
          const wd = day(w.date)
          return (
            <button key={w.date} className={`wday ${w.date === date ? 'sel' : ''} ${w.date === data.today ? 'now' : ''}`} onClick={() => go(w.date)}>
              <small>{wd.toLocaleDateString(undefined, { weekday: 'short' })}</small>
              <b>{wd.getDate()}</b>
              <span className="marks">{w.daily ? <i className="dot daily" title="Daily note" /> : null}{w.edited > 0 ? <i className="dot edit" title={`${w.edited} edited`} /> : null}</span>
            </button>
          )
        })}
        <Link to="/calendar" className="wday cal-link" title="Calendar">▦</Link>
      </div>}

      {err && <div className="callout danger mb">{err}</div>}
      {!data && !err && <Loading />}

      {data && <div className="today-grid">
        <section className="today-main">
          <form className="capture-big" onSubmit={doCapture}>
            <span className="stamp">{isToday ? stamp : date.slice(5)}</span>
            <input value={capture} onChange={e => setCapture(e.target.value)} placeholder={data.note ? 'Add a line to this day…' : 'Write the first line of this day…'} disabled={busy} />
            <button className="btn primary sm" disabled={busy || !capture.trim()}>{busy ? '…' : 'Add'}</button>
          </form>
          {data.note
            ? <div className="today-note"><NoteView ref={view} id={data.note.id} embedded /></div>
            : <div className="today-empty">
                <div className="big">📝</div>
                <h3>Nothing written {isToday ? 'today' : 'on this day'} yet</h3>
                <p className="muted">“{data.title}” is made from your daily template in your daily folder.</p>
                <button className="btn primary" onClick={startNote}>Start {isToday ? "today's" : 'this'} note</button>
              </div>}
        </section>

        <aside className="today-side">
          <section className="tcard">
            <div className="tcard-head"><h3>Calendar</h3>{agenda?.url && <a href={agenda.url} target="_blank" rel="noreferrer" className="small">Open ↗</a>}</div>
            {!agenda ? <div className="muted small">Looking…</div>
              : !agenda.connected ? <p className="muted small">Connect Nextcloud in <Link to="/settings?tab=integrations">Integrations</Link> to see your calendar here.</p>
              : agenda.error ? <p className="muted small">{agenda.error}</p>
              : agenda.events.length === 0 ? <p className="muted small">Nothing in the calendar.</p>
              : <ul className="agenda">{agenda.events.map((e, i) => {
                  const past = isToday && !e.all_day && e.end && new Date(e.end) < now
                  const live = isToday && !e.all_day && new Date(e.start) <= now && (!e.end || new Date(e.end) > now)
                  return (
                    <li key={i} className={`${past ? 'past' : ''} ${live ? 'live' : ''}`}>
                      <span className="bar" style={{ background: e.color || 'var(--accent)' }} />
                      <span className="when">{e.all_day ? 'All day' : <>{clock(e.start)}{e.end && <small className="muted"><br />{clock(e.end)}</small>}</>}</span>
                      <span className="grow"><b>{e.title}</b><small className="muted">{[e.location, e.calendar].filter(Boolean).join(' · ')}</small></span>
                    </li>
                  )
                })}</ul>}
          </section>

          {habits && habits.connected && <section className="tcard">
            <div className="tcard-head"><h3>Habits</h3>{habits.day && <span className="small muted">{habits.day.done}/{habits.day.total}</span>}{habits.url && <a href={habits.url} target="_blank" rel="noreferrer" className="small">Open ↗</a>}</div>
            {habits.error ? <p className="muted small">{habits.error}</p>
              : habits.day && habits.day.habits.filter(h => h.scheduled).length === 0 ? <p className="muted small">No habits on this day.</p>
              : habits.day && <div className="habit-chips">{habits.day.habits.filter(h => h.scheduled).map(h => (
                  <button key={h.id} className={`habit ${h.status === 1 ? 'done' : h.status === -1 ? 'missed' : ''}`} style={{ ['--hc' as string]: h.color || 'var(--accent)' }} onClick={() => setHabit(h)} disabled={date > todayISO()} title={h.streak ? `${h.streak}-day streak` : ''}>
                    <span className="tick">{h.status === 1 ? '✓' : h.status === -1 ? '✕' : ''}</span>{h.name}{h.streak > 1 && <small>🔥{h.streak}</small>}
                  </button>
                ))}</div>}
          </section>}

          <section className="tcard">
            <div className="tcard-head"><h3>Tasks</h3><Link to="/tasks" className="small">{data.tasks.open} open ›</Link></div>
            {tasksDue === 0 && data.tasks.upcoming.length === 0 ? <p className="muted small">Nothing due. Give a task a date with 📅 {date} or due:{date}.</p> : <>
              <TaskGroup title="Overdue" kind="overdue" items={data.tasks.overdue} onToggle={toggleTask} />
              <TaskGroup title={isToday ? 'Today' : 'This day'} kind="today" items={data.tasks.today} onToggle={toggleTask} />
              <TaskGroup title="Next 7 days" kind="soon" items={data.tasks.upcoming.slice(0, 6)} onToggle={toggleTask} />
            </>}
          </section>

          <section className="tcard">
            <div className="tcard-head"><h3>Written {isToday ? 'today' : 'that day'}</h3></div>
            {data.edited.length === 0 ? <p className="muted small">No other notes changed.</p> : <NoteList items={data.edited} onOpen={id => navigate(`/note/${id}`)} />}
          </section>

          {data.on_this_day.length > 0 && <section className="tcard">
            <div className="tcard-head"><h3>On this day</h3></div>
            <ul className="mini-list">{data.on_this_day.map(p => (
              <li key={p.date}><button className="link-btn mini" onClick={() => go(p.date)}><small className="muted">{p.label}</small><b>{p.note.title}</b><span className="muted small clamp">{p.note.excerpt || 'Empty note'}</span></button></li>
            ))}</ul>
          </section>}

          {data.recent.length > 0 && <section className="tcard">
            <div className="tcard-head"><h3>Recently opened</h3></div>
            <NoteList items={data.recent.slice(0, 5)} onOpen={id => navigate(`/note/${id}`)} />
          </section>}
        </aside>
      </div>}
    </div>
  )
}

function TaskGroup({ title, kind, items, onToggle }: { title: string; kind: string; items: TaskLine[]; onToggle: (t: TaskLine) => void }) {
  if (!items.length) return null
  return (
    <div className={`tgroup ${kind}`}>
      <div className="tgroup-title">{title}</div>
      <ul className="ttasks">{items.map((t, i) => (
        <li key={`${t.note_id}-${t.line}-${i}`}>
          <input type="checkbox" checked={t.done} onChange={() => onToggle(t)} />
          <span className="grow">{plainTask(t.text)}{t.due && kind !== 'today' && <span className={`due ${kind}`}>{t.due.slice(5)}</span>}<Link to={`/note/${t.note_id}`} className="from">{t.title}</Link></span>
        </li>
      ))}</ul>
    </div>
  )
}

function NoteList({ items, onOpen }: { items: ShortNote[]; onOpen: (id: number) => void }) {
  return (
    <ul className="mini-list">{items.map(n => (
      <li key={n.id}><button className="link-btn mini" onClick={() => onOpen(n.id)}>
        <b>{n.title}{n.new && <span className="new-pill">new</span>}</b>
        <span className="muted small">{[n.category, ago(n.modified)].filter(Boolean).join(' · ')}</span>
      </button></li>
    ))}</ul>
  )
}
