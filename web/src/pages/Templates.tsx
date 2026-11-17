import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Template } from '../lib/types'
import { ago, stamp } from '../lib/format'
import { placeholders } from '../lib/placeholders'
import { Confirm, Loading, MenuButton, type MenuItem, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { TemplateForm, makeFromTemplate, ownerName } from '../components/Templates'

// Your templates and the ones others share with everyone, as cards with a glimpse of how
// each starts: make a note from one, change its text (it is a note in the templates folder)
// or its settings, or delete it.
export default function Templates() {
  const { settings, bump, version } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const { data, reload } = useAsync(() => api.get<Template[]>(`${V1}/templates`), [version])
  const [form, setForm] = useState<{ template?: Template } | null>(null)
  const [del, setDel] = useState<Template | null>(null)
  const [busy, setBusy] = useState(0)
  const [helpOpen, setHelpOpen] = useState(() => localStorage.getItem('pn-tpl-help') === '1')
  const mine = (data || []).filter(t => !t.owner), theirs = (data || []).filter(t => t.owner)
  const folder = settings?.templates_folder || 'Templates'
  const toggleHelp = () => setHelpOpen(o => { localStorage.setItem('pn-tpl-help', o ? '0' : '1'); return !o })

  const use = async (t: Template) => { setBusy(t.id); await makeFromTemplate(t, null, navigate, bump, toast); setBusy(0) }
  const copy = async (t: Template) => {
    setBusy(t.id)
    try { await api.post<Template>(`${V1}/templates`, { from_note_id: t.id }); bump(); reload(); toast(`“${t.name}” is now one of your templates`) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(0) }
  }
  const edit = (t: Template) => navigate(`/note/${t.id}?in=${encodeURIComponent(t.category)}`)

  const card = (t: Template) => {
    const menu: MenuItem[] = t.owner
      ? [{ label: 'Copy to my templates', icon: 'copy', onSelect: () => copy(t) }]
      : [
          { label: 'Edit text', icon: 'edit', onSelect: () => edit(t) },
          { label: 'Name, title and folder…', icon: 'settings', onSelect: () => setForm({ template: t }) },
          'sep',
          { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => setDel(t) },
        ]
    return (
      <article key={t.id} className="tpl-card">
        <button type="button" className="tpl-paper" onClick={() => (t.owner ? use(t) : edit(t))} title={t.owner ? 'Make a note from it' : 'Edit what notes made from it start with'}>
          <span className="tpl-sheet">{t.preview.length ? t.preview.map((l, i) => <PreviewLine key={i} line={l.replace(/\{\{\s*title\s*\}\}/g, t.next_title)} />) : <span className="tpl-blank">Empty — write what every note starts with</span>}</span>
        </button>
        <div className="tpl-body">
          <div className="tpl-name">
            <b className="ellipsis" title={t.name}>{t.name}</b>
            {t.owner ? <span className="tpl-badge" title={t.owner.email}>{ownerName(t)}</span> : t.everyone && <span className="tpl-badge" title="Everyone on this server can use it"><Icon name="users" size={11} />Shared</span>}
          </div>
          <dl className="tpl-facts">
            <div><dt>Title</dt><dd className="ellipsis" title={t.title_pattern}>{t.next_title}</dd></div>
            <div><dt>Goes to</dt><dd className="ellipsis">{t.folder || 'The folder you’re in'}</dd></div>
          </dl>
          <div className="tpl-foot">
            <button type="button" className="btn primary sm" disabled={busy === t.id} onClick={() => use(t)}><Icon name="plus" size={14} strokeWidth={2.2} />New note</button>
            <span className="muted small tpl-when" title={stamp(t.modified)}>{t.owner ? '' : `Edited ${ago(t.modified)}`}</span>
            <MenuButton label={`More for ${t.name}`} align="right" width={220} items={menu}><Icon name="more" size={17} /></MenuButton>
          </div>
        </div>
      </article>
    )
  }

  return (
    <div className="page tpl-page"><div className="page-inner">
      <header className="tpl-head">
        <div>
          <h2>Templates</h2>
          <p className="muted">Notes you write again and again, one click away: the <Icon name="template" size={14} className="inline-ico" /> button beside <b>New note</b>, or <kbd>⇧</kbd><kbd>N</kbd>. They live in your <Link to={`/?in=${encodeURIComponent(folder)}`}>{folder}</Link> folder.</p>
        </div>
        <button type="button" className="btn primary" onClick={() => setForm({})}><Icon name="plus" size={15} strokeWidth={2.2} />New template</button>
      </header>

      {!data ? <Loading /> : <>
        {mine.length === 0
          ? <div className="tpl-empty">
              <div className="tpl-empty-ico"><Icon name="template" size={26} /></div>
              <h3>No templates yet</h3>
              <p className="muted">Open a note you write often — meeting notes, a ticket, a customer call — and choose <b>Save as template…</b> in its <Icon name="more" size={13} className="inline-ico" /> menu. Or start blank.</p>
              <button type="button" className="btn primary" onClick={() => setForm({})}><Icon name="plus" size={15} />New template</button>
            </div>
          : <section className="tpl-grid">{mine.map(card)}</section>}

        {theirs.length > 0 && <>
          <h3 className="tpl-section">Shared with everyone</h3>
          <section className="tpl-grid">{theirs.map(card)}</section>
        </>}

        <section className={`tpl-help ${helpOpen ? 'open' : ''}`}>
          <button type="button" className="tpl-help-head" aria-expanded={helpOpen} onClick={toggleHelp}>
            <Icon name="code" size={15} /><span className="grow"><b>Placeholders</b><span className="muted"> — filled in when a note is made. Type <code>{'{{'}</code> in a template for the list.</span></span>
            <Icon name={helpOpen ? 'chevron-up' : 'chevron-down'} size={16} />
          </button>
          {helpOpen && <div className="tpl-help-body">
            <table className="tpl-ph">
              <tbody>{placeholders().map(p => <tr key={p.text}><td><code>{p.text}</code></td><td className="muted">{p.detail}</td></tr>)}</tbody>
            </table>
            <p className="muted small">Any date format works: <code>YYYY YY MM M DD D</code>, <code>MMMM MMM dddd ddd</code>, <code>HH mm</code>; text in <code>[brackets]</code> stays as written — <code>{'{{date:YYYY-[W]MM}}'}</code>.</p>
          </div>}
        </section>
      </>}

      {form && <TemplateForm template={form.template} onClose={() => setForm(null)} onSaved={() => reload()} />}
      {del && <Confirm title={`Delete the template “${del.name}”?`} confirmLabel="Delete" message={<>It goes to the trash for {settings?.trash_days ?? 30} days. Notes made from it stay as they are{del.everyone ? '; nobody else can use it any more' : ''}.</>}
        onCancel={() => setDel(null)} onConfirm={async () => { const t = del; setDel(null); try { await api.del(`${V1}/templates/${t.id}`); bump(); reload(); toast('Template moved to the trash') } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div></div>
  )
}

/** One line of a template as a glimpse: headings bold, boxes and bullets drawn, placeholders marked. */
function PreviewLine({ line }: { line: string }) {
  let text = line.trim(), cls = 'tpl-l'
  const h = /^(#{1,6})\s+(.*)$/.exec(text)
  if (h) { text = h[2]; cls += h[1].length === 1 ? ' h1' : ' h2' }
  else if (/^[-*+]\s+\[[ xX]?\]\s*/.test(text)) { text = '☐ ' + text.replace(/^[-*+]\s+\[[ xX]?\]\s*/, ''); cls += ' li' }
  else if (/^[-*+](\s+|$)/.test(text)) { text = '• ' + text.replace(/^[-*+]\s*/, ''); cls += ' li' }
  else if (/^\d+[.)]\s+/.test(text)) cls += ' li'
  text = text.replace(/\*\*(.+?)\*\*/g, '$1').replace(/(^|\s)[*_](.+?)[*_](?=\s|$)/g, '$1$2')
  const parts = text.split(/(\{\{[^{}]*\}\})/g)
  return <span className={cls}>{parts.map((p, i) => /^\{\{.*\}\}$/.test(p) ? <mark key={i}>{p}</mark> : p || null)}</span>
}
