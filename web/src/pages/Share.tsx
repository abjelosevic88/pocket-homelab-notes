import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { stamp } from '../lib/format'
import { Loading } from '../components/ui'
import { RichEditor, type EditorMessage, type RichEditorHandle } from '../components/RichEditor'

// A shared note, read-only, for anyone with the link.
export default function Share() {
  const { token = '' } = useParams()
  const [note, setNote] = useState<{ title: string; content: string; modified: number; author: string } | null>(null)
  const [err, setErr] = useState('')
  const ed = useRef<RichEditorHandle>(null)
  const theme = (localStorage.getItem('pn-theme') as 'light' | 'dark') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    fetch(`/api/public/share/${encodeURIComponent(token)}`).then(async r => { const j = await r.json(); if (!r.ok) throw new Error(j.error || 'Not found'); setNote(j); document.title = j.title }).catch(e => setErr((e as Error).message))
  }, [token]) // eslint-disable-line react-hooks/exhaustive-deps
  // Links in a shared note: web links open, files kept with the note open through the link;
  // other notes stay private, so links to them go nowhere.
  const onMessage = (m: EditorMessage) => {
    if (m.name !== 'link') return
    const raw = String(m.raw ?? m.url ?? '')
    if (!raw || raw.startsWith('[[') || /\/apps\/notes\/note\/\d+|^id:/.test(raw)) return
    if (/^https?:/i.test(raw) || /^mailto:/i.test(raw)) { window.open(raw, '_blank', 'noopener'); return }
    if (!/^[a-z][a-z0-9+.-]*:/i.test(raw) && !raw.startsWith('#')) {
      let path = raw
      try { path = decodeURIComponent(raw) } catch {}
      window.open(`/api/public/share/${encodeURIComponent(token)}/attachment?path=${encodeURIComponent(path)}`, '_blank', 'noopener')
    }
  }
  if (err) return <div className="share-page"><div className="callout danger"><span>{err}</span></div></div>
  if (!note) return <div className="share-page"><Loading /></div>
  return (
    <div className="share-page">
      <header className="share-head"><div className="grow"><h1>{note.title}</h1><p className="muted small">{note.author && <>Shared by {note.author} · </>}edited {stamp(note.modified)}</p></div></header>
      <div className="share-body"><RichEditor ref={ed} docKey={token} revision={1} content={note.content} share={token} readonly theme={theme} onMessage={onMessage} /></div>
      <footer className="share-foot"><img src="/favicon.svg" alt="" />Pocket Notes</footer>
    </div>
  )
}
