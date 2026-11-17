import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Template } from '../lib/types'
import { ago, stamp } from '../lib/format'
import { Confirm, Empty, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { PlaceholderHelp, TemplateForm, makeFromTemplate, ownerName } from '../components/Templates'

// Your templates and the ones others share with everyone: use one, change its text (it is a
// note in the templates folder), its settings, or delete it.
export default function Templates() {
  const { settings, bump, version } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const { data, reload } = useAsync(() => api.get<Template[]>(`${V1}/templates`), [version])
  const [form, setForm] = useState<{ template?: Template } | null>(null)
  const [del, setDel] = useState<Template | null>(null)
  const [copying, setCopying] = useState(0)
  const mine = (data || []).filter(t => !t.owner), theirs = (data || []).filter(t => t.owner)
  const folder = settings?.templates_folder || 'Templates'

  const copy = async (t: Template) => {
    setCopying(t.id)
    try { await api.post<Template>(`${V1}/templates`, { from_note_id: t.id }); bump(); reload(); toast(`“${t.name}” is now one of your templates`) } catch (e) { toast((e as Error).message, 'err') } finally { setCopying(0) }
  }

  const card = (t: Template) => (
    <li key={t.id} className="tpl-card">
      <div className="tpl-main">
        <div className="row"><Icon name="template" size={16} /><b className="ellipsis">{t.name}</b>
          {t.everyone && !t.owner && <span className="folder-chip shared-chip" title="Everyone on this server can use it">everyone</span>}
          {t.owner && <span className="folder-chip shared-chip">by {ownerName(t)}</span>}
        </div>
        <div className="muted small tpl-meta">
          <span title="The title a new note gets today"><Icon name="file-plus" size={12} />{t.next_title}</span>
          <span><Icon name="folder" size={12} />{t.folder || 'where you are'}</span>
          <span title={stamp(t.modified)}>edited {ago(t.modified)}</span>
        </div>
        {t.excerpt && <div className="excerpt small">{t.excerpt}</div>}
      </div>
      <div className="tpl-actions">
        <button type="button" className="btn primary sm" onClick={() => makeFromTemplate(t, null, navigate, bump, toast)}><Icon name="plus" size={14} />New note</button>
        {t.owner
          ? <button type="button" className="btn sm" disabled={copying === t.id} onClick={() => copy(t)}><Icon name="copy" size={14} />Copy to mine</button>
          : <>
              <Link className="btn sm" to={`/note/${t.id}?in=${encodeURIComponent(t.category)}`} title="Change what notes made from it start with"><Icon name="edit" size={14} />Edit text</Link>
              <button type="button" className="btn ghost sm icon-only" title="Name, title, folder, sharing" aria-label={`Settings of ${t.name}`} onClick={() => setForm({ template: t })}><Icon name="settings" size={15} /></button>
              <button type="button" className="btn ghost sm icon-only danger-text" title="Delete" aria-label={`Delete ${t.name}`} onClick={() => setDel(t)}><Icon name="trash" size={15} /></button>
            </>}
      </div>
    </li>
  )

  return (
    <div className="page"><div className="page-inner">
      <PageHeader title="Templates" sub={<>Start a note from one in a single step: the template button beside New note, or <b>⇧N</b>. Templates are notes in your <Link to={`/?in=${encodeURIComponent(folder)}`}>{folder}</Link> folder.</>}
        actions={<button type="button" className="btn primary" onClick={() => setForm({})}><Icon name="plus" size={15} />New template</button>} />
      {!data ? <Loading /> : <>
        {mine.length === 0
          ? <Empty icon="template" title="No templates yet">Open a note you write again and again (meeting notes, a ticket, a customer call) and choose <b>Save as template…</b> in its <Icon name="more" size={13} /> menu — or make a blank one with <b>New template</b>.</Empty>
          : <ul className="tpl-list">{mine.map(card)}</ul>}
        {theirs.length > 0 && <><h3 className="tpl-h">Shared with everyone</h3><ul className="tpl-list">{theirs.map(card)}</ul></>}
        <h3 className="tpl-h">Filled in when a note is made</h3>
        <p className="muted small">In a template’s title and text. <code>{'{{title}}'}</code> in the text is the new note’s title. Dates take your own format too: <code>{'{{date:YYYY-MM-DD}}'}</code>, <code>{'{{date:ddd D MMM}}'}</code>, <code>{'{{tomorrow:DD.MM.}}'}</code>.</p>
        <PlaceholderHelp />
      </>}
      {form && <TemplateForm template={form.template} onClose={() => setForm(null)} onSaved={() => reload()} />}
      {del && <Confirm title={`Delete the template “${del.name}”?`} confirmLabel="Delete" message={<>It goes to the trash for {settings?.trash_days ?? 30} days. Notes made from it stay as they are{del.everyone ? '; nobody else can use it any more' : ''}.</>}
        onCancel={() => setDel(null)} onConfirm={async () => { const t = del; setDel(null); try { await api.del(`${V1}/templates/${t.id}`); bump(); reload(); toast('Template moved to the trash') } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div></div>
  )
}
