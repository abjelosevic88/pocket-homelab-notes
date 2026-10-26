import { useEffect } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import NoteView from '../components/NoteView'
import { Icon } from '../components/icons'
import { EMBEDDED } from '../lib/embed'

// /embed/note/:id — one note and nothing else, for another Pocket app to show in a side panel
// (Pocket Projects on an issue). Links to the rest of Notes open in a new tab; a note it moves to
// (a wiki link, a duplicate) stays in the panel. The parent hears which note is shown:
// postMessage {type: 'pocket-notes', note_id, closed}.

const tell = (msg: Record<string, unknown>) => { if (window.parent !== window) window.parent.postMessage({ type: 'pocket-notes', ...msg }, '*') }

export default function EmbedNote() {
  const { id } = useParams()
  useEffect(() => { tell({ note_id: Number(id) }) }, [id])
  // anything that is not this note opens in a new tab instead of filling the panel
  const outward = (e: React.MouseEvent) => {
    const a = (e.target as HTMLElement).closest('a')
    if (!a || !a.href || a.target === '_blank' || e.defaultPrevented) return
    const u = new URL(a.href, location.href)
    if (u.origin === location.origin && u.pathname.startsWith('/embed/')) return
    e.preventDefault()
    window.open(u.href, '_blank', 'noopener')
  }
  return <div className="embed-note" onClickCapture={outward}><NoteView key={id} id={Number(id)} embedded /></div>
}

/** Keeps an embedded Notes on its one note: a move to another note stays embedded, anything else
 *  (the note went to the trash) ends in a short message. */
export function EmbedGuard() {
  const loc = useLocation()
  const navigate = useNavigate()
  useEffect(() => {
    if (!EMBEDDED || loc.pathname.startsWith('/embed/') || loc.pathname === '/login' || loc.pathname === '/setup') return
    const m = loc.pathname.match(/^\/note\/(\d+)/)
    navigate(m ? `/embed/note/${m[1]}` : '/embed/closed', { replace: true })
  }, [loc.pathname, navigate])
  return null
}

export function EmbedClosed() {
  useEffect(() => { tell({ closed: true }) }, [])
  return <div className="embed-note embed-closed"><Icon name="trash" size={22} /><p>This note is in the trash.</p><a href="/trash" target="_blank" rel="noreferrer">Open the trash</a></div>
}
