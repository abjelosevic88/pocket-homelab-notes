import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note } from '../lib/types'
import { Loading } from '../components/ui'

// /new?title=…&url=…&label=…&folder=…&text=…&image=1 — a note about something in another Pocket
// app (docs/notes-from-apps.md): its first line is the title, then a link to the thing's page,
// which is how that app finds the note again for its "Notes" box. With image=1 the thing is shown
// as a picture instead (a drawing from Pocket Draw). The server writes it (POST /notes/from).
export default function NewFrom() {
  const [params] = useSearchParams()
  const { settings, bump } = useApp()
  const navigate = useNavigate()
  const [err, setErr] = useState('')
  const started = useRef(false)

  useEffect(() => {
    if (started.current || !settings) return
    started.current = true
    const url = params.get('url') || ''
    if (!/^https?:\/\//i.test(url)) { setErr('This link has no web address to write the note about.'); return }
    const folder = params.get('folder') ?? settings.default_folder ?? ''
    api.post<Note>(`${V1}/notes/from`, { title: params.get('title') || '', url, label: params.get('label') || '', text: params.get('text') || '', folder, image: params.get('image') === '1' })
      .then(n => { bump(); navigate(`/note/${n.id}`, { replace: true }) })
      .catch(e => setErr((e as Error).message))
  }, [settings, params, navigate, bump])

  if (err) return <div className="page"><div className="page-inner"><div className="callout danger">{err}</div><p className="mt"><Link to="/">Back to your notes</Link></p></div></div>
  return <div className="page"><Loading label="Making the note…" /></div>
}
