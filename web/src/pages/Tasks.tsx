import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { TaskLine } from '../lib/types'
import { today } from '../lib/format'
import { Empty, Loading, PageHeader, Segmented, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

// Every checkbox in every note, by when it is due; ticking one writes the note.
export default function Tasks() {
  const [state, setState] = useState<'open' | 'done' | 'all'>('open')
  const { data, setData, loading, error } = useAsync(() => api.get<TaskLine[]>(`${V1}/tasks?state=${state}`), [state])
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const now = today()
  const groups = useMemo(() => {
    const list = (data || []).filter(t => !q || t.text.toLowerCase().includes(q.toLowerCase()) || t.title.toLowerCase().includes(q.toLowerCase()))
    const g: { key: string; title: string; items: TaskLine[] }[] = [
      { key: 'overdue', title: 'Overdue', items: [] }, { key: 'today', title: 'Today', items: [] }, { key: 'soon', title: 'Upcoming', items: [] }, { key: 'none', title: 'No date', items: [] },
    ]
    for (const t of list) {
      const k = !t.due ? 'none' : t.due < now && !t.done ? 'overdue' : t.due === now ? 'today' : t.due < now ? 'none' : 'soon'
      g.find(x => x.key === k)!.items.push(t)
    }
    g[2].items.sort((a, b) => (a.due || '').localeCompare(b.due || ''))
    return g.filter(x => x.items.length)
  }, [data, q, now])
  const toggle = async (t: TaskLine) => {
    try {
      await api.post(`${V1}/notes/${t.note_id}/tasks`, { line: t.line, text: t.text, done: !t.done })
      setData(d => (d || []).map(x => x === t ? { ...x, done: !x.done } : x))
    } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="page"><div className="page-inner">
      <PageHeader title="Tasks" sub="Every checkbox in your notes. Give one a date with 📅 2026-10-21 or due:2026-10-21." actions={<Segmented size="sm" value={state} onChange={setState} options={[{ value: 'open', label: 'Open' }, { value: 'done', label: 'Done' }, { value: 'all', label: 'All' }]} />} />
      <div className="tool-search task-filter"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter tasks…" aria-label="Filter tasks" /></div>
      {loading && !data && <Loading />}
      {error && <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {data && !groups.length && <Empty icon="check-square" title={state === 'open' ? 'Nothing to do' : 'No tasks'}>Write <code>- [ ] something</code> in any note.</Empty>}
      {groups.map(g => (
        <section key={g.key} className={`task-group ${g.key}`}>
          <h3>{g.title}<span className={`badge ${g.key === 'overdue' ? 'danger' : ''}`}>{g.items.length}</span></h3>
          <ul className="task-list">
            {g.items.map((t, i) => (
              <li key={`${t.note_id}-${t.line}-${i}`} style={{ paddingLeft: 8 + t.indent * 10 }} className={t.done ? 'done' : ''}>
                <input type="checkbox" checked={t.done} onChange={() => toggle(t)} />
                <span className="grow">{t.text.replace(/(📅\s*|due:\s*)\d{4}-\d{2}-\d{2}/, '').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/[*_~`]+/g, '').trim()}{t.due && <span className={`due ${g.key}`}>{t.due}</span>}</span>
                <Link to={`/note/${t.note_id}`} title={t.category ? `${t.category}/${t.title}` : t.title}><Icon name="note" size={13} />{t.title}</Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div></div>
  )
}
