// Templates: notes in the templates folder, made into new notes in one step. The chooser
// ("New note from template"), the form that saves one (from a note, or blank) and changes it.
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Note, Template } from '../lib/types'
import { Field, Loading, Modal, Switch, useDebounce, useToast } from './ui'
import { Icon } from './icons'
import { Select } from './Select'

/** Make a note from a template and open it. `here`: the folder being looked at. */
export async function makeFromTemplate(t: Template, here: string | null, navigate: (to: string) => void, bump: () => void, toast: (m: string, k?: 'ok' | 'err') => void) {
  try {
    const n = await api.post<Note>(`${V1}/templates/${t.id}/use`, here != null ? { here } : {})
    bump()
    navigate(`/note/${n.id}?in=${encodeURIComponent(n.category)}`)
  } catch (e) { toast((e as Error).message, 'err') }
}

/** Open the chooser from anywhere (the sidebar, the list, ⇧N). */
export const chooseTemplate = () => window.dispatchEvent(new CustomEvent('pn:from-template'))

export const ownerName = (t: Template) => t.owner ? (t.owner.name || t.owner.email) : ''

/** "New note from template": one click makes the note. */
export function TemplateChooser({ here, onClose }: { here: string | null; onClose: () => void }) {
  const { bump } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [list, setList] = useState<Template[] | null>(null)
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(0)
  useEffect(() => { api.get<Template[]>(`${V1}/templates`).then(setList).catch(e => { toast((e as Error).message, 'err'); setList([]) }) }, [toast])
  const shown = (list || []).filter(t => !q.trim() || `${t.name} ${t.excerpt} ${ownerName(t)}`.toLowerCase().includes(q.trim().toLowerCase()))
  const mine = shown.filter(t => !t.owner), theirs = shown.filter(t => t.owner)
  const pick = async (t: Template) => { setBusy(t.id); await makeFromTemplate(t, here, navigate, bump, toast); setBusy(0); onClose() }
  const row = (t: Template) => (
    <button type="button" key={t.id} className="tool-row" disabled={!!busy} onClick={() => pick(t)}>
      <span className="ico"><Icon name="template" size={16} /></span>
      <span className="grow"><b>{t.name}</b><small className="muted">{t.next_title}{t.folder ? ` · in ${t.folder}` : ''}{t.owner ? ` · by ${ownerName(t)}` : ''}</small></span>
      {busy === t.id && <span className="spinner" />}
    </button>
  )
  return (
    <Modal title="New note from a template" onClose={onClose}>
      {!list ? <Loading /> : list.length === 0 ? (
        <div className="stack tight">
          <p className="muted">No templates yet. Open a note you write again and again and choose <b>Save as template…</b> in its <Icon name="more" size={13} /> menu, or make one on the Templates page.</p>
          <div className="form-actions"><Link className="btn primary" to="/templates" onClick={onClose}><Icon name="template" size={15} />Templates</Link></div>
        </div>
      ) : <>
        {list.length > 6 && <div className="tool-search"><Icon name="search" size={15} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a template…" aria-label="Find a template" /></div>}
        <div className="tool-results">
          {mine.map(row)}
          {theirs.length > 0 && <div className="list-group">Shared with everyone</div>}
          {theirs.map(row)}
          {shown.length === 0 && <p className="muted small">No template has those words.</p>}
        </div>
        <div className="row mt small"><span className="muted">{here ? <>Templates without a folder of their own make the note in <b>{here || 'Notes'}</b>.</> : 'Notes go to the template’s folder, else your default one.'}</span><span className="spacer" /><Link to="/templates" onClick={onClose}>Manage</Link></div>
      </>}
    </Modal>
  )
}

const PLACEHOLDERS: [string, string][] = [
  ['{{date}}', 'today, as set in Settings'], ['{{date:YYMMDD}}', 'today as 261104'], ['{{date:DD.MM.YYYY}}', '04.11.2026'],
  ['{{time}}', 'the time'], ['{{weekday}}', 'day of the week'], ['{{week}}', 'week number'], ['{{name}}', 'the template’s name'],
]

/** What {{…}} can say: shown under the title field and on the Templates page. */
export function PlaceholderHelp({ onPick }: { onPick?: (p: string) => void }) {
  return (
    <div className="tpl-chips">
      {PLACEHOLDERS.map(([p, what]) => onPick
        ? <button type="button" key={p} className="tpl-chip" title={what} onClick={() => onPick(p)}><code>{p}</code></button>
        : <span key={p} className="tpl-chip" title={what}><code>{p}</code> {what}</span>)}
    </div>
  )
}

/** Save a template (from a note, or blank) or change one's name, title, folder and sharing. */
export function TemplateForm({ template, fromNote, onClose, onSaved }: { template?: Template; fromNote?: Note; onClose: () => void; onSaved?: (t: Template) => void }) {
  const { folders, settings, bump } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const startName = template?.name ?? fromNote?.title ?? ''
  const [name, setName] = useState(startName)
  const [pattern, setPattern] = useState(template ? template.title_pattern : fromNote ? '{{name}} {{date}}' : '{{name}} {{date}}')
  const [folder, setFolder] = useState(template?.folder ?? (fromNote && fromNote.category !== settings?.templates_folder ? fromNote.category : ''))
  const [everyone, setEveryone] = useState(template?.everyone ?? false)
  const [preview, setPreview] = useState('')
  const [busy, setBusy] = useState(false)
  const dp = useDebounce(`${pattern}\u0000${name}`, 250)
  useEffect(() => {
    const [p, n] = dp.split('\u0000')
    api.post<{ title: string }>(`${V1}/templates/preview`, { pattern: p, name: n }).then(r => setPreview(r.title)).catch(() => setPreview(''))
  }, [dp])
  const tplFolder = settings?.templates_folder || 'Templates'
  const folderOptions = useMemo(() => {
    const paths = Array.from(new Set(folders.map(f => f.path).filter(p => p && p !== tplFolder && !p.startsWith(tplFolder + '/') && p !== settings?.attachments_folder)))
    if (folder && !paths.includes(folder)) paths.push(folder)
    paths.sort((a, b) => a.localeCompare(b))
    return [{ value: '', label: 'Where I am, else my default folder' }, ...paths.map(p => ({ value: p, label: p }))]
  }, [folders, folder, tplFolder, settings?.attachments_folder])

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    setBusy(true)
    try {
      const body = { name: name.trim(), title_pattern: pattern, folder, everyone }
      const t = template
        ? await api.put<Template>(`${V1}/templates/${template.id}`, body)
        : await api.post<Template>(`${V1}/templates`, fromNote ? { ...body, from_note_id: fromNote.id } : { ...body, content: '# {{title}}\n\n' })
      bump()
      onSaved?.(t)
      onClose()
      if (!template) toast(fromNote ? `Saved as the template “${t.name}”` : 'Template made: write what every note starts with', 'ok')
      if (!template && !fromNote) navigate(`/note/${t.id}?in=${encodeURIComponent(t.category)}`)
    } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }

  return (
    <Modal title={template ? `Template “${template.name}”` : fromNote ? 'Save as template' : 'New template'} onClose={onClose}>
      <form className="stack" onSubmit={save}>
        {fromNote && <p className="muted small">A copy of “{fromNote.title}” goes to your <b>{tplFolder}</b> folder; the note itself stays as it is. Change the copy later like any note.</p>}
        <Field label="Template name"><input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="Meeting notes" required /></Field>
        <Field label="Title of each new note" help={<>Today it would be <b>{preview || '…'}</b></>}>
          <input value={pattern} onChange={e => setPattern(e.target.value)} placeholder="{{date:YYMMDD}} {{name}}" spellCheck={false} />
        </Field>
        <PlaceholderHelp onPick={p => setPattern(v => (v && !v.endsWith(' ') ? v + ' ' : v) + p)} />
        <Field label="New notes go to"><Select value={folder} onChange={setFolder} options={folderOptions} label="New notes go to" searchable width={320} /></Field>
        <label className="check-row"><Switch checked={everyone} onChange={setEveryone} label="Everyone here can use it" />Everyone on this server can use it <span className="muted small">(read only for them)</span></label>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary" disabled={busy || !name.trim()}>{template ? 'Save' : fromNote ? 'Save template' : 'Make and edit'}</button></div>
      </form>
    </Modal>
  )
}

/** Is this note one of the person's templates (in their templates folder)? */
export function isTemplateNote(n: { category: string; shared_from?: unknown }, templatesFolder?: string): boolean {
  const f = templatesFolder ?? ''
  return !n.shared_from && !!f && (n.category === f || n.category.startsWith(f + '/'))
}

/** The settings of the template that note id is: loads it, then the form. */
export function TemplateSettings({ id, onClose }: { id: number; onClose: () => void }) {
  const { toast } = useToast()
  const [t, setT] = useState<Template | null>(null)
  useEffect(() => {
    api.get<Template[]>(`${V1}/templates?mine=1`).then(l => { const x = l.find(y => y.id === id); if (x) setT(x); else { toast('This note is not a template any more', 'err'); onClose() } })
      .catch(e => { toast((e as Error).message, 'err'); onClose() })
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  return t ? <TemplateForm template={t} onClose={onClose} /> : null
}

/** Above a template's text: what it is, and making a note from it. */
export function TemplateBanner({ note }: { note: Note }) {
  const { bump } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const use = async () => {
    try { const n = await api.post<Note>(`${V1}/templates/${note.id}/use`, {}); bump(); navigate(`/note/${n.id}?in=${encodeURIComponent(n.category)}`) } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="callout info tpl-banner">
      <Icon name="template" size={15} />
      <span className="grow">A template: notes made from it start with this text. <code>{'{{date}}'}</code>, <code>{'{{title}}'}</code> and the other placeholders are filled in then.</span>
      <span className="row">
        <button type="button" className="btn sm primary" onClick={use}><Icon name="plus" size={14} />New note from it</button>
        <button type="button" className="btn sm ghost icon-only" title="Name, title, folder, sharing" aria-label="Template settings" onClick={() => setOpen(true)}><Icon name="settings" size={15} /></button>
      </span>
      {open && <TemplateSettings id={note.id} onClose={() => setOpen(false)} />}
    </div>
  )
}
