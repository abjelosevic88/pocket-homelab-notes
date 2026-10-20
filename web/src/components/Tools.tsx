import { useEffect, useMemo, useRef, useState } from 'react'
import { api, ApiError, V1, qs } from '../lib/api'
import type { Note } from '../lib/types'
import { Field, Loading, Modal, Segmented, useDebounce, useToast } from './ui'
import { ago, bytes } from '../lib/format'

// The sheets behind the editor's homelab tools: what the app shows as its own pickers. Each
// answers the Markdown to put into the note (and where), the page puts it in.

export type ToolResult = { markdown: string; at: 'inline' | 'block' | 'line' }

/** Tells "not connected" apart: it points to Settings → Integrations. */
export function toolError(e: unknown): string {
  if (e instanceof ApiError && e.status === 412) return `${e.message}`
  return (e as Error).message
}

// ---------------------------------------------------------------- link

interface LinkItem { id: string; title: string; detail: string; url: string; icon: string; markdown: string }
interface LinkAnswer { sections: { source: string; title: string; items: LinkItem[] }[]; errors?: Record<string, string>; note?: string }

const ICONS: Record<string, string> = { file: '📄', folder: '📁', document: '🧾', issue: '◎', pull: '⇄', movie: '🎬', series: '📺', episode: '📺', photo: '🖼', link: '🔗' }

export function LinkPicker({ onPick, onClose }: { onPick: (md: string) => void; onClose: () => void }) {
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 350)
  const [data, setData] = useState<LinkAnswer | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => {
    if (dq.trim().length < 2) { setData(null); return }
    let live = true
    setBusy(true); setErr('')
    api.get<LinkAnswer>(`${V1}/tools/link${qs({ q: dq.trim() })}`).then(d => { if (live) setData(d) }).catch(e => { if (live) setErr(toolError(e)) }).finally(() => live && setBusy(false))
    return () => { live = false }
  }, [dq])
  const sections = data?.sections?.filter(s => s.items.length) || []
  return (
    <Modal title="Link to something" onClose={onClose} size="lg">
      <div className="tool-search"><span className="muted">⌕</span><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search files, documents, issues, films, photos…" />{busy && <span className="spinner" />}</div>
      <div className="tool-results">
        {err && <div className="callout danger">{err}</div>}
        {!sections.length && !busy && !err && <p className="muted center pad">{q.trim().length < 2 ? 'Type a few letters. The note gets a link to what you pick, not a copy.' : `Nothing matches “${q}”.`}</p>}
        {sections.map(s => (
          <div key={s.source} className="tool-section">
            <h4>{s.title}</h4>
            {s.items.map(it => (
              <button key={it.id} className="tool-row" onClick={() => onPick(it.markdown || `[${it.title}](${it.url})`)}>
                <span className="ico">{ICONS[it.icon] || '🔗'}</span>
                <span className="grow"><b>{it.title}</b>{it.detail && <small className="muted">{it.detail}</small>}</span>
              </button>
            ))}
          </div>
        ))}
        {data?.errors && Object.entries(data.errors).map(([k, v]) => <p key={k} className="muted small">{v}</p>)}
      </div>
      <p className="muted small mt">{data?.note || 'Each result becomes a link in the note. Nothing is copied.'}</p>
    </Modal>
  )
}

// ---------------------------------------------------------------- bookmark

export function BookmarkSheet({ onPick, onClose }: { onPick: (md: string) => void; onClose: () => void }) {
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const { toast } = useToast()
  useEffect(() => {
    navigator.clipboard?.readText?.().then(t => { if (/^(https?:\/\/|www\.)\S+$/i.test(t.trim())) setUrl(t.trim()) }).catch(() => {})
  }, [])
  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try { const r = await api.post<{ markdown: string }>(`${V1}/tools/bookmark`, { url }); onPick(r.markdown) } catch (e) { toast(toolError(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Web bookmark" onClose={onClose}>
      <form onSubmit={add} className="grid" style={{ gap: 14 }}>
        <Field label="Address" help="The page's title becomes the link's words."><input autoFocus value={url} onChange={e => setUrl(e.target.value)} placeholder="https://…" spellCheck={false} required /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Reading the page…' : 'Add'}</button></div>
      </form>
    </Modal>
  )
}

// ---------------------------------------------------------------- photos (Immich)

interface Photo { id: string; date: string; filename: string; width: number; height: number; type: string }

export function PhotoPicker({ noteID, onPick, onClose }: { noteID: number; onPick: (md: string) => void; onClose: () => void }) {
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 450)
  const [favorites, setFavorites] = useState(false)
  const [items, setItems] = useState<Photo[]>([])
  const [next, setNext] = useState(0)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [picked, setPicked] = useState<string[]>([])
  const [mode, setMode] = useState<'copy' | 'link'>('copy')
  const [size, setSize] = useState<'preview' | 'original'>('preview')
  const [adding, setAdding] = useState(false)
  const ask = useRef(0)
  const load = async (page: number) => {
    const n = ++ask.current
    setBusy(true); setErr('')
    try {
      const r = await api.get<{ items: Photo[]; next_page: number }>(`${V1}/tools/photos${qs({ q: dq.trim(), page, favorites })}`)
      if (n !== ask.current) return
      setItems(x => page === 1 ? r.items : [...x, ...r.items]); setNext(r.next_page)
    } catch (e) { if (n === ask.current) setErr(toolError(e)) } finally { if (n === ask.current) setBusy(false) }
  }
  useEffect(() => { load(1) }, [dq, favorites]) // eslint-disable-line react-hooks/exhaustive-deps
  const toggle = (id: string) => setPicked(p => p.includes(id) ? p.filter(x => x !== id) : [...p, id])
  const add = async () => {
    setAdding(true)
    try { const r = await api.post<{ markdown: string }>(`${V1}/tools/photos/attach`, { note_id: noteID, ids: picked, mode, size }); onPick(r.markdown) } catch (e) { setErr(toolError(e)) } finally { setAdding(false) }
  }
  return (
    <Modal title="From Photos" onClose={onClose} size="xl">
      <div className="row wrap mb">
        <div className="tool-search grow"><span className="muted">⌕</span><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Search Immich: “beach at sunset”, “server rack”, “invoice”…" />{busy && <span className="spinner" />}</div>
        <Segmented size="sm" value={favorites ? 'fav' : 'all'} onChange={v => setFavorites(v === 'fav')} options={[{ value: 'all', label: 'Newest' }, { value: 'fav', label: '★ Favourites' }]} />
      </div>
      {err && <div className="callout danger mb">{err}</div>}
      <div className="photo-grid">
        {items.map(p => {
          const i = picked.indexOf(p.id)
          return (
            <button key={p.id} className={`photo ${i >= 0 ? 'on' : ''}`} onClick={() => toggle(p.id)} title={`${p.filename} · ${new Date(p.date).toLocaleDateString()}`}>
              <img loading="lazy" src={`${V1}/tools/photos/${encodeURIComponent(p.id)}/thumb?size=thumbnail`} alt={p.filename} />
              {i >= 0 && <span className="pick">{i + 1}</span>}
            </button>
          )
        })}
      </div>
      {!busy && !items.length && !err && <p className="muted center pad">No pictures found.</p>}
      {next > 0 && <div className="center mt"><button className="btn" onClick={() => load(next)} disabled={busy}>More</button></div>}
      <div className="modal-foot">
        <Segmented size="sm" value={mode} onChange={setMode} options={[{ value: 'copy', label: 'Put in the note' }, { value: 'link', label: 'Link to Immich' }]} />
        {mode === 'copy' && <Segmented size="sm" value={size} onChange={setSize} options={[{ value: 'preview', label: 'Preview size' }, { value: 'original', label: 'Original' }]} />}
        <span className="spacer" />
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" disabled={!picked.length || adding} onClick={add}>{adding ? 'Adding…' : picked.length ? `Add ${picked.length}` : 'Add'}</button>
      </div>
    </Modal>
  )
}

// ---------------------------------------------------------------- files (Nextcloud)

interface CloudFile { name: string; path: string; dir: boolean; size: number; modified: string; mime: string; file_id: string }

export function FilesPicker({ noteID, onPick, onClose }: { noteID: number; onPick: (md: string) => void; onClose: () => void }) {
  const [path, setPath] = useState('/')
  const [data, setData] = useState<{ path: string; parent: string; items: CloudFile[] } | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [mode, setMode] = useState<'copy' | 'link'>('link')
  useEffect(() => {
    let live = true
    setBusy(true); setErr('')
    api.get<{ path: string; parent: string; items: CloudFile[] }>(`${V1}/tools/files${qs({ path })}`).then(d => live && setData(d)).catch(e => live && setErr(toolError(e))).finally(() => live && setBusy(false))
    return () => { live = false }
  }, [path])
  const pick = async (f: CloudFile) => {
    setBusy(true)
    try { const r = await api.post<{ markdown: string }>(`${V1}/tools/files/attach`, { note_id: noteID, path: f.path, mode }); onPick(r.markdown) } catch (e) { setErr(toolError(e)); setBusy(false) }
  }
  const crumbs = path.split('/').filter(Boolean)
  return (
    <Modal title="From Nextcloud" onClose={onClose} size="lg">
      <div className="crumbs mb">
        <button className="btn ghost sm" onClick={() => setPath('/')}>Files</button>
        {crumbs.map((c, i) => <span key={i} className="row"><span className="muted">/</span><button className="btn ghost sm" onClick={() => setPath('/' + crumbs.slice(0, i + 1).join('/'))}>{c}</button></span>)}
        {busy && <span className="spinner" />}
      </div>
      {err && <div className="callout danger mb">{err}</div>}
      <div className="tool-results files">
        {data?.items.map(f => (
          <button key={f.path} className="tool-row" onClick={() => f.dir ? setPath(f.path) : pick(f)}>
            <span className="ico">{f.dir ? '📁' : f.mime?.startsWith('image/') ? '🖼' : '📄'}</span>
            <span className="grow"><b>{f.name}</b><small className="muted">{f.dir ? 'Folder' : `${bytes(f.size)} · ${ago(f.modified)}`}</small></span>
            {f.dir && <span className="muted">›</span>}
          </button>
        ))}
        {data && !data.items.length && <p className="muted center pad">This folder is empty.</p>}
      </div>
      <div className="modal-foot">
        <Segmented size="sm" value={mode} onChange={setMode} options={[{ value: 'link', label: 'Link to it' }, { value: 'copy', label: 'Copy into the note' }]} />
        <span className="spacer" /><button className="btn" onClick={onClose}>Cancel</button>
      </div>
    </Modal>
  )
}

// ---------------------------------------------------------------- task (Nextcloud Tasks / Deck)

interface TaskTargets { lists: { id: string; name: string; color?: string }[]; boards: { id: number; title: string; color?: string; stacks: { id: number; title: string }[] }[]; default?: 'tasks' | 'deck'; errors?: Record<string, string> }

export function TaskSheet({ line, onDone, onClose }: { line: string; onDone: (md: string) => void; onClose: () => void }) {
  const [targets, setTargets] = useState<TaskTargets | null>(null)
  const [err, setErr] = useState('')
  const [text, setText] = useState(line.replace(/^\s*([-*+]|\d+[.)])\s+(\[[ xX]\]\s+)?/, ''))
  const [dest, setDest] = useState<'tasks' | 'deck'>('tasks')
  const [list, setList] = useState('')
  const [board, setBoard] = useState(0)
  const [stack, setStack] = useState(0)
  const [due, setDue] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.get<TaskTargets>(`${V1}/tools/task/targets`).then(t => {
      setTargets(t)
      const d = t.default || (t.lists.length ? 'tasks' : 'deck')
      setDest(d)
      if (t.lists[0]) setList(t.lists[0].id)
      if (t.boards[0]) { setBoard(t.boards[0].id); if (t.boards[0].stacks[0]) setStack(t.boards[0].stacks[0].id) }
    }).catch(e => setErr(toolError(e)))
  }, [])
  const stacks = targets?.boards.find(b => b.id === board)?.stacks || []
  const send = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try { const r = await api.post<{ markdown: string }>(`${V1}/tools/task`, { text, destination: dest, list, board, stack, due: due || undefined }); onDone(r.markdown) } catch (e) { setErr(toolError(e)) } finally { setBusy(false) }
  }
  return (
    <Modal title="Make this line a task" onClose={onClose}>
      {!targets && !err && <Loading />}
      {err && <div className="callout danger mb">{err}</div>}
      {targets && <form onSubmit={send} className="grid" style={{ gap: 14 }}>
        <Field label="Task"><input autoFocus value={text} onChange={e => setText(e.target.value)} required /></Field>
        <Field group label="Send to"><Segmented value={dest} onChange={setDest} options={[{ value: 'tasks', label: 'Nextcloud Tasks' }, { value: 'deck', label: 'Deck' }]} /></Field>
        {dest === 'tasks' ? (
          <Field label="List">{targets.lists.length ? <select value={list} onChange={e => setList(e.target.value)}>{targets.lists.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</select> : <p className="muted">No task list in Nextcloud. {targets.errors?.tasks}</p>}</Field>
        ) : (
          <div className="two-col tight">
            <Field label="Board">{targets.boards.length ? <select value={board} onChange={e => { const b = Number(e.target.value); setBoard(b); setStack(targets.boards.find(x => x.id === b)?.stacks[0]?.id || 0) }}>{targets.boards.map(b => <option key={b.id} value={b.id}>{b.title}</option>)}</select> : <p className="muted">No Deck board. {targets.errors?.deck}</p>}</Field>
            <Field label="List">{<select value={stack} onChange={e => setStack(Number(e.target.value))}>{stacks.map(s => <option key={s.id} value={s.id}>{s.title}</option>)}</select>}</Field>
          </div>
        )}
        <Field label="Due (optional)"><input type="date" value={due} onChange={e => setDue(e.target.value)} /></Field>
        <p className="muted small">The line stays in the note with a link to where it went.</p>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || (dest === 'tasks' ? !list : !stack)}>{busy ? 'Sending…' : 'Send'}</button></div>
      </form>}
    </Modal>
  )
}

// ---------------------------------------------------------------- transaction (Pocket Money)

interface ExpenseOptions { default_account?: number | string; accounts?: { id: number | string; name: string; currency?: string }[]; categories?: { id: number | string; name: string }[]; templates?: { id: number | string; name: string }[]; currency?: string }

export function ExpenseSheet({ line, onDone, onClose }: { line: string; onDone: (md: string) => void; onClose: () => void }) {
  const [opts, setOpts] = useState<ExpenseOptions | null>(null)
  const [text, setText] = useState(line.replace(/^\s*([-*+]|\d+[.)])\s+(\[[ xX]\]\s+)?/, ''))
  const [account, setAccount] = useState('')
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10))
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.get<ExpenseOptions>(`${V1}/tools/expense/options`).then(o => { setOpts(o); if (o.default_account != null) setAccount(String(o.default_account)); else if (o.accounts?.[0]) setAccount(String(o.accounts[0].id)) }).catch(e => setErr(toolError(e)))
  }, [])
  const send = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try { const r = await api.post<{ markdown: string }>(`${V1}/tools/expense`, { text, account_id: account || undefined, date }); onDone(r.markdown) } catch (e) { setErr(toolError(e)) } finally { setBusy(false) }
  }
  return (
    <Modal title="Make this line a transaction" onClose={onClose}>
      {err && <div className="callout danger mb">{err}</div>}
      {!opts && !err && <Loading />}
      {opts && <form onSubmit={send} className="grid" style={{ gap: 14 }}>
        <Field label="Line" help="Template or category + amount + what for, e.g. groceries 24.50 bread"><input autoFocus value={text} onChange={e => setText(e.target.value)} required /></Field>
        {!!opts.accounts?.length && <Field label="From account"><select value={account} onChange={e => setAccount(e.target.value)}>{opts.accounts.map(a => <option key={a.id} value={String(a.id)}>{a.name}{a.currency ? ` (${a.currency})` : ''}</option>)}</select></Field>}
        <Field label="Date"><input type="date" value={date} onChange={e => setDate(e.target.value)} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Booking…' : 'Book it'}</button></div>
      </form>}
    </Modal>
  )
}

// ---------------------------------------------------------------- habits (days)

export function HabitsSheet({ onDone, onClose }: { onDone: (md: string) => void; onClose: () => void }) {
  const iso = (d: Date) => d.toISOString().slice(0, 10)
  const [from, setFrom] = useState(() => iso(new Date(Date.now() - 6 * 86400000)))
  const [to, setTo] = useState(() => iso(new Date()))
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const run = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try { const r = await api.post<{ markdown: string; empty: boolean; message?: string }>(`${V1}/tools/report/habits`, { from, to }); if (r.empty) setErr(r.message || 'There was nothing to write down.'); else onDone(r.markdown) } catch (e) { setErr(toolError(e)) } finally { setBusy(false) }
  }
  return (
    <Modal title="Days of habits" onClose={onClose}>
      <form onSubmit={run} className="grid" style={{ gap: 14 }}>
        <p className="muted">Habits done and missed, trackers and journal lines from Pocket Habits.</p>
        <div className="two-col tight"><Field label="From"><input type="date" value={from} onChange={e => setFrom(e.target.value)} /></Field><Field label="To"><input type="date" value={to} onChange={e => setTo(e.target.value)} /></Field></div>
        {err && <div className="callout danger">{err}</div>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Reading…' : 'Put in the note'}</button></div>
      </form>
    </Modal>
  )
}

// ---------------------------------------------------------------- voice memo

type SpeechRec = { lang: string; continuous: boolean; interimResults: boolean; start(): void; stop(): void; onresult: ((e: { results: ArrayLike<ArrayLike<{ transcript: string }> & { isFinal: boolean }> }) => void) | null }

export function VoiceMemo({ upload, onDone, onClose }: { upload: (f: File) => Promise<string>; onDone: (md: string) => void; onClose: () => void }) {
  const [state, setState] = useState<'idle' | 'rec' | 'done' | 'saving'>('idle')
  const [secs, setSecs] = useState(0)
  const [blob, setBlob] = useState<Blob | null>(null)
  const [words, setWords] = useState('')
  const [err, setErr] = useState('')
  const rec = useRef<MediaRecorder | null>(null)
  const speech = useRef<SpeechRec | null>(null)
  const timer = useRef<number>(0)
  useEffect(() => () => { clearInterval(timer.current); rec.current?.stream.getTracks().forEach(t => t.stop()); speech.current?.stop() }, [])
  const start = async () => {
    setErr('')
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      const mr = new MediaRecorder(stream)
      const chunks: Blob[] = []
      mr.ondataavailable = e => chunks.push(e.data)
      mr.onstop = () => { setBlob(new Blob(chunks, { type: mr.mimeType || 'audio/webm' })); stream.getTracks().forEach(t => t.stop()); setState('done') }
      mr.start(); rec.current = mr
      setSecs(0); timer.current = window.setInterval(() => setSecs(s => s + 1), 1000)
      // Words written out where the browser can (Chrome, Safari).
      const W = window as unknown as { SpeechRecognition?: new () => SpeechRec; webkitSpeechRecognition?: new () => SpeechRec }
      const SR = W.SpeechRecognition || W.webkitSpeechRecognition
      if (SR) {
        const sr = new SR(); sr.lang = navigator.language; sr.continuous = true; sr.interimResults = false
        sr.onresult = e => { let t = ''; for (let i = 0; i < e.results.length; i++) t += e.results[i][0].transcript + ' '; setWords(t.trim()) }
        try { sr.start(); speech.current = sr } catch {}
      }
      setState('rec')
    } catch (e) { setErr('The microphone could not be used: ' + (e as Error).message) }
  }
  const stop = () => { clearInterval(timer.current); rec.current?.stop(); speech.current?.stop() }
  const save = async () => {
    if (!blob) return
    setState('saving')
    const ext = blob.type.includes('mp4') ? 'm4a' : blob.type.includes('ogg') ? 'ogg' : 'webm'
    const name = `Voice memo ${new Date().toISOString().slice(0, 16).replace('T', ' ').replace(':', '.')}.${ext}`
    try {
      const md = await upload(new File([blob], name, { type: blob.type }))
      const minutes = `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}`
      onDone(`🎙 ${md} (${minutes})` + (words ? `\n\n> ${words}` : ''))
    } catch (e) { setErr((e as Error).message); setState('done') }
  }
  return (
    <Modal title="Voice memo" onClose={onClose}>
      <div className="voice">
        <div className="voice-time">{Math.floor(secs / 60)}:{String(secs % 60).padStart(2, '0')}</div>
        {state === 'idle' && <button className="btn primary lg" onClick={start}>● Record</button>}
        {state === 'rec' && <button className="btn danger lg" onClick={stop}>■ Stop</button>}
        {(state === 'done' || state === 'saving') && blob && <audio controls src={useMemoURL(blob)} />}
        {words && <blockquote className="muted">{words}</blockquote>}
        {err && <div className="callout danger">{err}</div>}
        <p className="muted small">The recording is stored with the note{('webkitSpeechRecognition' in window || 'SpeechRecognition' in window) ? ', with its words written out' : ''}.</p>
      </div>
      <div className="form-actions"><button className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={state !== 'done'} onClick={save}>{state === 'saving' ? 'Saving…' : 'Add to the note'}</button></div>
    </Modal>
  )
}

function useMemoURL(b: Blob) {
  const url = useMemo(() => URL.createObjectURL(b), [b])
  useEffect(() => () => URL.revokeObjectURL(url), [url])
  return url
}

// ---------------------------------------------------------------- own templates

export function TemplatePicker({ onPick, onClose }: { onPick: (md: string) => void; onClose: () => void }) {
  const [list, setList] = useState<Note[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => { api.get<Note[]>(`${V1}/templates`).then(setList).catch(e => setErr((e as Error).message)) }, [])
  const pick = async (n: Note) => {
    const full = await api.get<Note>(`${V1}/notes/${n.id}?peek=1`)
    onPick(full.content || '')
  }
  return (
    <Modal title="My templates" onClose={onClose}>
      {err && <div className="callout danger">{err}</div>}
      {!list && !err && <Loading />}
      {list && !list.length && <p className="muted">No templates yet. Notes in your templates folder (Settings → Writing) show up here; <code>{'{{date}}'}</code>, <code>{'{{time}}'}</code>, <code>{'{{weekday}}'}</code> and <code>{'{{title}}'}</code> are filled in.</p>}
      {list && <div className="tool-results">{list.map(n => <button key={n.id} className="tool-row" onClick={() => pick(n)}><span className="ico">📋</span><span className="grow"><b>{n.title}</b><small className="muted">{n.excerpt}</small></span></button>)}</div>}
    </Modal>
  )
}

// ---------------------------------------------------------------- diagram

export function DiagramViewer({ svg, onClose }: { svg: string; onClose: () => void }) {
  const [zoom, setZoom] = useState(1)
  return (
    <Modal title="Diagram" onClose={onClose} size="xl">
      <div className="row mb"><button className="btn sm" onClick={() => setZoom(z => Math.max(0.25, z / 1.25))}>−</button><span className="muted small">{Math.round(zoom * 100)}%</span><button className="btn sm" onClick={() => setZoom(z => Math.min(6, z * 1.25))}>+</button><button className="btn ghost sm" onClick={() => setZoom(1)}>Fit</button></div>
      <div className="diagram-view"><div style={{ transform: `scale(${zoom})`, transformOrigin: '0 0' }} dangerouslySetInnerHTML={{ __html: svg }} /></div>
    </Modal>
  )
}
