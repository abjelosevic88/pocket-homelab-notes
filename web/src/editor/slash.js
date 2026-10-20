// The `/` menu: the editor's own entries (text, headings, quote, divider, lists, code, table)
// plus what this app adds: callouts, diagrams, folding sections, a table of contents, formulas,
// footnotes, dates, links to other notes, emoji, templates, code in a given language, files.
//
// The menu narrows by an entry's `label`, which is not shown (the visible name and description
// are drawn from `title` and `description`), so a label also carries the other words an entry
// should answer to: `/bash` finds "Shell", `/toc` the table of contents.
import { commandsCtx, editorViewCtx } from '@milkdown/kit/core'
import { clearTextInCurrentBlockCommand, wrapInBlockTypeCommand, codeBlockSchema } from '@milkdown/kit/preset/commonmark'
import { TextSelection } from '@milkdown/kit/prose/state'
import { callout } from './callouts.js'
import { svg, clearSlash, post } from './shared.js'
import { detailsItem } from './details.js'
import { contentsItem } from './toc.js'
import { mathBlockItem, mathInlineItem } from './math.js'
import { stampItem, stamps, footnoteItem } from './inserts.js'
import { noteLinkItem } from './suggest.js'
import { emojiItem } from './emoji.js'
import { templateItem, ownTemplatesItem } from './templates.js'
import { addHomelabGroup } from './homelab.js'
import { openExternalLink } from './links.js'

const icons = {
  info: svg('<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>'),
  success: svg('<circle cx="12" cy="12" r="9"/><path d="M8 12.5l2.8 2.8L16 9.5"/>'),
  warning: svg('<path d="M12 4l9 15.5H3z"/><path d="M12 10v4M12 17h.01"/>'),
  error: svg('<circle cx="12" cy="12" r="9"/><path d="M9 9l6 6M15 9l-6 6"/>'),
  question: svg('<circle cx="12" cy="12" r="9"/><path d="M9.6 9.5a2.5 2.5 0 1 1 3.4 2.3c-.7.3-1 .9-1 1.7M12 16.5h.01"/>'),
  diagram: svg('<rect x="3" y="4" width="7" height="5" rx="1"/><rect x="14" y="15" width="7" height="5" rx="1"/><path d="M6.5 9v5.5a2 2 0 0 0 2 2H14"/>'),
  sequence: svg('<path d="M6 4v16M18 4v16"/><path d="M6 9h12M14.5 6.5L18 9l-3.5 2.5M18 15H6M9.5 12.5L6 15l3.5 2.5"/>'),
  gantt: svg('<path d="M4 5v14h16"/><path d="M7 8h7M10 12h8M8 16h5"/>'),
  state: svg('<circle cx="6" cy="12" r="2.5"/><rect x="13" y="8" width="8" height="8" rx="2"/><path d="M8.5 12H13"/>'),
  pie: svg('<circle cx="12" cy="12" r="9"/><path d="M12 3v9l7.5 5"/>'),
  mindmap: svg('<circle cx="12" cy="12" r="2.5"/><path d="M14.5 12H20M9.5 12H4M13.5 10l3-4M10.5 14l-3 4M13.5 14l3 4M10.5 10l-3-4"/>'),
  fold: svg('<path d="M8 5l5 4.5L8 14"/><path d="M4 19h16M14 9.5h6"/>'),
  contents: svg('<path d="M4 6h10M4 12h16M8 18h12M4 18h.01"/>'),
  formula: svg('<path d="M18 5H7l6 7-6 7h11"/>'),
  inlineFormula: svg('<path d="M4 17l4-5-4-5M12 19l3-14M18 8l3 4-3 4"/>'),
  footnote: svg('<path d="M4 18h10M4 13h16M4 8h8"/><path d="M17 5l2-1v6"/>'),
  calendar: svg('<rect x="3.5" y="5" width="17" height="15" rx="2"/><path d="M3.5 10h17M8 3v4M16 3v4"/>'),
  clock: svg('<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>'),
  noteLink: svg('<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>'),
  emoji: svg('<circle cx="12" cy="12" r="9"/><path d="M8.5 14.5a4.5 4.5 0 0 0 7 0M9 9.5h.01M15 9.5h.01"/>'),
  template: svg('<rect x="4" y="3.5" width="16" height="17" rx="2"/><path d="M8 8h8M8 12h8M8 16h4"/>'),
  code: svg('<path d="M9 8l-4 4 4 4M15 8l4 4-4 4"/>'),
}

const calloutItem = (kind) => (ctx) => {
  const commands = ctx.get(commandsCtx)
  commands.call(clearTextInCurrentBlockCommand.key)
  commands.call(wrapInBlockTypeCommand.key, { nodeType: callout.type(ctx), attrs: { kind } })
}

// A diagram starts with something to look at: an empty Mermaid block draws nothing.
const starters = {
  flowchart: 'flowchart LR\n  A[Start] --> B{Decision}\n  B -->|yes| C[Done]\n  B -->|no| A',
  sequence: 'sequenceDiagram\n  participant App\n  participant Server\n  App->>Server: Request\n  Server-->>App: Answer',
  gantt: 'gantt\n  dateFormat YYYY-MM-DD\n  title Plan\n  section First\n  Prepare :a1, ' + stamps.date() + ', 3d\n  Build   :a2, after a1, 5d\n  section Then\n  Test    :after a2, 2d',
  state: 'stateDiagram-v2\n  [*] --> Idle\n  Idle --> Running: start\n  Running --> Idle: stop\n  Running --> [*]',
  pie: 'pie title Disk use\n  "Media" : 62\n  "Backups" : 28\n  "Free" : 10',
  mindmap: 'mindmap\n  root((Topic))\n    First idea\n      Detail\n    Second idea\n    Third idea',
}

// A code block in place of the empty line the menu was typed on, or after the block it was typed in.
function putCodeBlock(ctx, language, text, cursorInside) {
  clearSlash(ctx)
  const view = ctx.get(editorViewCtx)
  const { state } = view
  const node = codeBlockSchema.type(ctx).create({ language }, text ? state.schema.text(text) : undefined)
  const $from = state.selection.$from
  const at = $from.parent.content.size === 0 ? $from.before() : $from.after()
  const tr = $from.parent.content.size === 0 ? state.tr.replaceWith(at, $from.after(), node) : state.tr.insert(at, node)
  if (cursorInside) { tr.setSelection(TextSelection.create(tr.doc, at + 1)) }
  view.dispatch(tr.scrollIntoView())
  view.focus()
}
const diagramItem = (kind) => (ctx) => putCodeBlock(ctx, 'mermaid', starters[kind], false)
// `/swift`, `/bash`, `/yaml`: the block already knows its language.
const codeItem = (language) => (ctx) => putCodeBlock(ctx, language, '', true)
const codeLanguages = [
  ['bash', 'Shell', 'bash sh zsh terminal command'], ['yaml', 'YAML', 'yml compose kubernetes'], ['json', 'JSON', ''], ['swift', 'Swift', ''],
  ['python', 'Python', 'py'], ['javascript', 'JavaScript', 'js node'], ['typescript', 'TypeScript', 'ts'], ['sql', 'SQL', 'postgres mysql sqlite query'],
  ['dockerfile', 'Dockerfile', 'docker container'], ['nginx', 'Nginx', 'proxy conf'], ['toml', 'TOML', 'ini conf config'], ['diff', 'Diff', 'patch'],
  ['html', 'HTML', ''], ['css', 'CSS', ''], ['go', 'Go', 'golang'], ['rust', 'Rust', 'rs'],
]

// The app shows its own pickers: this device's files, or the ones in Nextcloud.
const attachItem = (source) => (ctx) => {
  ctx.get(commandsCtx).call(clearTextInCurrentBlockCommand.key)
  post('attach', { source })
}
icons.device = svg('<path d="M12 16V5M8 9l4-4 4 4"/><path d="M5 15v3a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-3"/>')
icons.cloud = svg('<path d="M7 18a4 4 0 0 1-.6-7.96A5.5 5.5 0 0 1 17.2 9.1 4.5 4.5 0 0 1 17 18z"/>')

icons.share = svg('<path d="M12 16V4"/><path d="M8 8l4-4 4 4"/><path d="M4 14v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4"/>')
// The app makes the file: the page has no note pictures of its own and no way to write a file.
const shareItem = (kind) => (ctx) => { clearSlash(ctx); post('share', { kind }) }
icons.photos = svg('<rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="M4 17l5-4.5 4 3.5 3-2.5 4 3.5"/>')

// Always offered: when the photo library cannot be used yet, the app says what is missing.

// Every entry by its key, for the app's scripted checks (`debugRun`).
export const runners = {}
export const paletteItems = []

export function buildMenu(builder) {
  const insert = builder.addGroup('insert', 'Insert')
  insert.addItem('date-today', { title: 'Today', label: 'Today date day', description: stamps.today() + '. Or type @today', icon: icons.calendar, onRun: stampItem('today') })
  insert.addItem('date-now', { title: 'Now', label: 'Now date time timestamp', description: 'Date and time. Or type @now', icon: icons.clock, onRun: stampItem('now') })
  insert.addItem('date-tomorrow', { title: 'Tomorrow', label: 'Tomorrow date day', description: stamps.tomorrow() + '. Or type @tomorrow', icon: icons.calendar, onRun: stampItem('tomorrow') })
  insert.addItem('date-iso', { title: 'Date, short', label: 'Date short ISO today log', description: stamps.date() + ', for logs. Or type @date', icon: icons.calendar, onRun: stampItem('date') })
  insert.addItem('external-link', { title: 'Insert link', label: 'Insert link external URL website hyperlink', description: 'Link selected text, or insert a web address', icon: icons.noteLink, onRun: ctx => { clearSlash(ctx); openExternalLink(ctx) } })
  insert.addItem('note-link', { title: 'Link to a note', label: 'Link to a note page mention backlink', description: 'Type [[ anywhere and the note’s name', icon: icons.noteLink, onRun: noteLinkItem })
  insert.addItem('emoji', { title: 'Emoji', label: 'Emoji smiley icon symbol', description: 'Search by name. Or type :rocket:', icon: icons.emoji, onRun: emojiItem })
  const files = builder.addGroup('attach', 'Attach')
  files.addItem('attach-device', { label: 'Upload a file', icon: icons.device, onRun: attachItem('device') })
  files.addItem('attach-nextcloud', { label: 'From Nextcloud', icon: icons.cloud, onRun: attachItem('nextcloud') })
  files.addItem('attach-immich', { label: 'From Photos', icon: icons.photos, onRun: attachItem('immich') })
  addHomelabGroup(builder)
  const share = builder.addGroup('share', 'Share')
  share.addItem('share-pdf', { title: 'Share as PDF', label: 'Share as PDF export print send file', description: 'The note as a PDF, pictures and diagrams included', icon: icons.share, onRun: shareItem('pdf') })
  share.addItem('share-html', { title: 'Share as HTML', label: 'Share as HTML export web page send file', description: 'The note as one web page that needs nothing else', icon: icons.share, onRun: shareItem('html') })
  const callouts = builder.addGroup('callout', 'Callout')
  callouts.addItem('callout-info', { label: 'Info', icon: icons.info, onRun: calloutItem('info') })
  callouts.addItem('callout-success', { label: 'Success', icon: icons.success, onRun: calloutItem('success') })
  callouts.addItem('callout-warning', { label: 'Warning', icon: icons.warning, onRun: calloutItem('warning') })
  callouts.addItem('callout-error', { label: 'Error', icon: icons.error, onRun: calloutItem('error') })
  callouts.addItem('callout-question', { label: 'Question', icon: icons.question, onRun: calloutItem('question') })
  const diagrams = builder.addGroup('diagrams', 'Diagrams')
  diagrams.addItem('diagram', { title: 'Diagram', label: 'Diagram flowchart flow chart mermaid', description: 'A flowchart to start from, drawn with Mermaid', icon: icons.diagram, onRun: diagramItem('flowchart') })
  diagrams.addItem('diagram-sequence', { title: 'Sequence diagram', label: 'Sequence diagram mermaid messages', description: 'Who sends what to whom, in order', icon: icons.sequence, onRun: diagramItem('sequence') })
  diagrams.addItem('diagram-gantt', { title: 'Gantt chart', label: 'Gantt chart diagram mermaid plan timeline schedule', description: 'Tasks over time', icon: icons.gantt, onRun: diagramItem('gantt') })
  diagrams.addItem('diagram-state', { title: 'State diagram', label: 'State diagram mermaid machine', description: 'States and what moves between them', icon: icons.state, onRun: diagramItem('state') })
  diagrams.addItem('diagram-pie', { title: 'Pie chart', label: 'Pie chart diagram mermaid share', description: 'Shares of a whole', icon: icons.pie, onRun: diagramItem('pie') })
  diagrams.addItem('diagram-mindmap', { title: 'Mind map', label: 'Mind map mindmap diagram mermaid ideas', description: 'Ideas branching from one topic', icon: icons.mindmap, onRun: diagramItem('mindmap') })
  const templates = builder.addGroup('templates', 'Templates')
  templates.addItem('template-meeting', { title: 'Meeting notes', label: 'Meeting notes template agenda', description: 'Agenda, notes, decisions, to do', icon: icons.template, onRun: templateItem('meeting') })
  templates.addItem('template-daily', { title: 'Daily note', label: 'Daily note template journal today', description: 'Today, notes, tomorrow', icon: icons.template, onRun: templateItem('daily') })
  templates.addItem('template-checklist', { title: 'Checklist', label: 'Checklist template todo tasks', description: 'A title and boxes to tick', icon: icons.template, onRun: templateItem('checklist') })
  templates.addItem('template-incident', { title: 'Incident log', label: 'Incident log template outage postmortem', description: 'What happened, timeline, cause, fix', icon: icons.template, onRun: templateItem('incident') })
  templates.addItem('template-runbook', { title: 'Runbook', label: 'Runbook template steps commands server ssh procedure', description: 'Steps with the commands to run on the server', icon: icons.template, onRun: templateItem('runbook') })
  templates.addItem('template-ports', { title: 'Open ports', label: 'Open ports listening docker containers ss netstat server', description: 'A command that lists what listens on the server', icon: icons.code, onRun: templateItem('ports') })
  templates.addItem('template-compose', { title: 'Compose service', label: 'Compose service docker yaml container snippet', description: 'A docker compose service to fill in', icon: icons.code, onRun: templateItem('compose') })
  templates.addItem('template-own', { title: 'My templates…', label: 'My templates own saved', description: 'The notes in your Templates folder', icon: icons.template, onRun: ownTemplatesItem })
  const languages = builder.addGroup('languages', 'Code in a language')
  for (const [language, title, words] of codeLanguages) {
    languages.addItem('code-' + language, { title: title + ' code', label: `${title} code ${language} ${words}`, description: 'A code block set to ' + title, icon: icons.code, onRun: codeItem(language) })
  }
  const advanced = (() => { try { return builder.getGroup('advanced') } catch (e) { return builder.addGroup('advanced', 'Advanced') } })()
  advanced.addItem('details', { title: 'Collapsible section', label: 'Collapsible section toggle details fold spoiler', description: 'A title with text that folds away', icon: icons.fold, onRun: detailsItem })
  advanced.addItem('contents', { title: 'Table of contents', label: 'Table of contents toc outline headings', description: 'Links to this note’s headings. Ask again to update it', icon: icons.contents, onRun: contentsItem })
  advanced.addItem('footnote', { title: 'Footnote', label: 'Footnote reference note citation', description: 'A numbered mark, its text at the end. Or type [^1]', icon: icons.footnote, onRun: footnoteItem })
  advanced.addItem('formula', { title: 'Formula', label: 'Formula math latex equation katex block', description: 'A LaTeX formula on lines of its own. Or type $$ and a space', icon: icons.formula, onRun: mathBlockItem })
  advanced.addItem('formula-inline', { title: 'Formula in a sentence', label: 'Formula in a sentence inline math latex equation', description: 'A small formula within text. Or type $$x$$', icon: icons.inlineFormula, onRun: mathInlineItem })
  // One continuous, searchable list, with section labels instead of category tabs.
  const groups = builder.build()
  const descriptions = {
    text: 'Write a plain paragraph', h1: 'Large section heading', h2: 'Medium section heading',
    h3: 'Small section heading', h4: 'Heading level four', h5: 'Heading level five', h6: 'Heading level six',
    quote: 'Highlight a quotation', divider: 'Separate sections with a line',
    'bullet-list': 'Create a simple bulleted list', 'ordered-list': 'Create a numbered sequence',
    'task-list': 'Track items with checkboxes', code: 'Code with syntax highlighting',
    table: 'Organize information in rows and columns', image: 'A picture, uploaded or from a link',
    'callout-info': 'Add context or helpful information', 'callout-success': 'Highlight a positive result',
    'callout-warning': 'Draw attention to a caution', 'callout-error': 'Flag a problem to resolve',
    'callout-question': 'Keep an open question visible',
  }
  const textItems = groups.filter(group => ['text', 'list'].includes(group.key)).flatMap(group => group.items)
  const order = ['text', 'task-list', 'h1', 'h2', 'h3', 'bullet-list', 'ordered-list', 'quote', 'divider']
  const basic = order.map(key => textItems.find(item => item.key === key)).filter(Boolean)
  const extraHeadings = textItems.filter(item => !order.includes(item.key))
  const other = groups.filter(group => !['text', 'list'].includes(group.key))
    .map(group => group.key === 'advanced' ? { ...group, items: [...group.items, ...extraHeadings] } : group)
  builder.clear()
  // The order they are reached for: blocks, things put into text, files, then the longer lists.
  const rank = ['advanced', 'insert', 'attach', 'homelab', 'callout', 'diagrams', 'templates', 'share', 'languages']
  other.sort((a, b) => (rank.indexOf(a.key) + 1 || 99) - (rank.indexOf(b.key) + 1 || 99))
  paletteItems.length = 0
  const sections = [{ key: 'basic', label: 'Basic blocks', items: basic }, ...other]
  const escape = value => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;')
  for (const section of sections) {
    const group = builder.addGroup(section.key, section.label === 'Callout' ? 'Callouts' : section.label)
    for (const item of section.items) {
      const title = item.title || item.label
      runners[item.key] = item.onRun
      const description = item.description || descriptions[item.key] || ('Insert ' + title.toLowerCase())
      paletteItems.push({ key: item.key, title, description, icon: item.icon, group: section.label, keywords: item.label })
      group.addItem(item.key, { ...item, icon: `<span class="block-preview">${item.icon}</span><span class="block-copy"><strong>${escape(title)}</strong><small>${escape(description)}</small></span>` })
    }
  }

}

// Moving through the `/` menu with the arrow keys: Crepe sets the list's scroll so the chosen
// row sits at the very top on every step. Scroll as a list does instead, only as far as needed
// to keep the row in view. Registered before Crepe's own listener, so it runs first: it notes
// where the list was, and just before the next frame puts it back and brings the row in view.
export function slashScrollsAsAList() {
  window.addEventListener('keydown', (event) => {
    if (!['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight'].includes(event.key)) return
    const list = document.querySelector('.milkdown-slash-menu[data-show="true"] .menu-groups')
    if (!list) return
    const before = list.scrollTop
    requestAnimationFrame(() => {
      list.scrollTop = before
      const row = list.querySelector('li.hover')
      if (row) row.scrollIntoView({ block: 'nearest' })
    })
  }, { capture: true })
}
