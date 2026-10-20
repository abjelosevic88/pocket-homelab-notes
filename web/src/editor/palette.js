// One command catalogue for the slash menu, phone footer and desktop header.
// Toolbar commands use the editor's selection and history, never append to the document.
import { commandsCtx, editorViewCtx } from '@milkdown/kit/core'
import { toggleStrongCommand, toggleEmphasisCommand, toggleInlineCodeCommand } from '@milkdown/kit/preset/commonmark'
import { toggleStrikethroughCommand } from '@milkdown/kit/preset/gfm'
import { undo, redo } from '@milkdown/kit/prose/history'
import { TextSelection } from '@milkdown/kit/prose/state'
import { notice, svg, post } from './shared.js'
import { openExternalLink } from './links.js'
import { toggleCodeMode, codeModeOn } from './code-mode.js'

const formats = [
  { key: 'format-bold', mark: 'strong', title: 'Bold', glyph: 'B', command: toggleStrongCommand, group: 'Formatting' },
  { key: 'format-italic', mark: 'emphasis', title: 'Italic', glyph: 'I', command: toggleEmphasisCommand, group: 'Formatting' },
  { key: 'format-code', mark: 'inlineCode', title: 'Inline code', glyph: '</>', command: toggleInlineCodeCommand, group: 'Formatting' },
  { key: 'format-strike', mark: 'strike_through', title: 'Strikethrough', glyph: 'S̶', command: toggleStrikethroughCommand, group: 'Formatting' },
  { key: 'undo', title: 'Undo', glyph: '↶', group: 'History' },
  { key: 'redo', title: 'Redo', glyph: '↷', group: 'History' },
]

/// The formats in force at the cursor (or across the selection), each on its own, so bold
/// and italic can both show as on.
export function activeFormats(ctx) {
  const { state } = ctx.get(editorViewCtx)
  const { empty, from, to, $from } = state.selection
  const marks = empty ? (state.storedMarks || $from.marks()) : null
  const on = new Set()
  for (const format of formats) {
    const type = format.mark && state.schema.marks[format.mark]
    if (!type) continue
    if (empty ? type.isInSet(marks) || (format.mark === 'inlineCode' && codeModeOn(state)) : state.doc.rangeHasMark(from, to, type)) on.add(format.key)
  }
  return on
}

export function runPaletteCommand(ctx, key, runners) {
  const view = ctx.get(editorViewCtx)
  if (!view.editable) return false
  if (key === 'external-link') return openExternalLink(ctx)
  const format = formats.find(item => item.key === key)
  if (format) {
    if (key === 'undo' || key === 'redo') (key === 'undo' ? undo : redo)(view.state, view.dispatch)
    else if (key === 'format-code' && view.state.selection.empty && toggleCodeMode(view)) { /* typed text becomes code */ }
    else ctx.get(commandsCtx).call(format.command.key)
  } else {
    if (!runners[key]) return false
    // Slash commands clear their trigger's paragraph. Give them an empty block beside
    // the cursor, so choosing Table/Diagram/etc. never erases an existing paragraph.
    const { state } = view, { $from } = state.selection
    const empty = $from.depth === 1 && $from.parent.type.name === 'paragraph' && !$from.parent.content.size
    if (!empty) {
      const at = $from.depth ? $from.after(1) : state.selection.to
      const tr = state.tr.insert(at, state.schema.nodes.paragraph.create())
      tr.setSelection(TextSelection.create(tr.doc, at + 1))
      view.dispatch(tr)
    }
    runners[key](ctx)
  }
  view.focus()
  return true
}

export function mountPalette({ items, run, capture, restore, table, active }) {
  const bar = document.createElement('div')
  bar.className = 'note-command-bar'
  bar.setAttribute('role', 'toolbar'); bar.setAttribute('aria-label', 'Note formatting and inserts')
  let dialog = null, bookmark = null
  const allItems = [...formats, ...items]
  const button = (item, action, compact = false) => {
    const b = document.createElement('button'); b.type = 'button'
    b.dataset.command = item.key; b.title = item.title; b.setAttribute('aria-label', item.title)
    if (item.destructive) b.classList.add('destructive')
    if (item.icon) { const icon = document.createElement('span'); icon.className = 'palette-icon'; icon.innerHTML = item.icon; b.append(icon) }
    const label = document.createElement('span'); label.textContent = compact ? item.glyph || item.title : item.title
    if (!item.iconOnly) b.append(label)
    // Mouse clicks must not discard the editor selection. Touch remains scrollable.
    b.addEventListener('mousedown', e => e.preventDefault())
    b.addEventListener('click', action)
    return b
  }
  const close = (focus = false) => {
    if (dialog) { post('picker', { open: false }); document.body.classList.remove('picker-open'); document.dispatchEvent(new Event('note-picker-closed')) }
    dialog?.remove(); dialog = null; all.setAttribute('aria-expanded', 'false')
    if (focus) { restore(bookmark); all.focus() }
  }
  const invoke = key => {
    if (dialog) restore(bookmark)
    close()
    try { run(key) } catch { notice('This item could not be inserted here. Try an empty paragraph.') }
  }
  const all = button({ key: 'all', title: 'All inserts', iconOnly: true, icon: svg('<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><path d="M17.5 14v7M14 17.5h7"/>') }, () => {
    if (dialog) { close(true); return }
    bookmark = capture()
    dialog = document.createElement('section'); dialog.className = 'note-command-picker'
    dialog.setAttribute('role', 'dialog'); dialog.setAttribute('aria-label', 'Insert into note')
    all.setAttribute('aria-expanded', 'true')
    post('picker', { open: true }); document.body.classList.add('picker-open')
    const header = document.createElement('div'); header.className = 'palette-search'
    const search = document.createElement('input'); search.type = 'search'; search.placeholder = 'Find a block, attachment, or action…'
    search.setAttribute('aria-label', 'Search note commands')
    const cancel = button({ key: 'close', title: 'Close' }, () => close(true))
    header.append(search, cancel)
    const list = document.createElement('div'); list.className = 'palette-results'
    const render = () => {
      list.replaceChildren()
      const words = search.value.toLowerCase().trim().split(/\s+/).filter(Boolean)
      let group = ''
      for (const item of allItems.filter(item => words.every(word => `${item.title} ${item.group} ${item.description || ''} ${item.keywords || ''}`.toLowerCase().includes(word)))) {
        if (item.group !== group) { group = item.group; const heading = document.createElement('h3'); heading.textContent = group; list.append(heading) }
        const row = button(item, () => invoke(item.key))
        if (item.description) { const detail = document.createElement('small'); detail.textContent = item.description; row.append(detail) }
        list.append(row)
      }
      if (!list.childElementCount) { const empty = document.createElement('p'); empty.textContent = 'No matching commands'; empty.setAttribute('role', 'status'); list.append(empty) }
    }
    search.addEventListener('input', render)
    dialog.addEventListener('keydown', e => {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(true) }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        const rows = [...list.querySelectorAll('button')]
        if (!rows.length) return
        e.preventDefault()
        const i = rows.indexOf(document.activeElement)
        rows[(i + (e.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length].focus()
      }
      if (e.key === 'Enter' && document.activeElement === search) { e.preventDefault(); list.querySelector('button')?.click() }
    })
    dialog.append(header, list); document.body.append(dialog); render(); search.focus()
  })
  all.className = 'palette-all'; all.setAttribute('aria-haspopup', 'dialog'); all.setAttribute('aria-expanded', 'false')
  bar.append(all)
  const scroll = document.createElement('div'); scroll.className = 'palette-strip'
  for (const item of formats.slice(0, 4)) scroll.append(button(item, () => invoke(item.key), true))
  const quick = ['external-link', 'h1', 'h2', 'task-list', 'bullet-list', 'ordered-list', 'table', 'code', 'note-link', 'attach-device', 'diagram', 'date-today', 'template-own']
  for (const key of quick) { const item = items.find(item => item.key === key); if (item) scroll.append(button(item, () => invoke(key))) }
  for (const item of formats.slice(4)) scroll.append(button(item, () => invoke(item.key), true))
  const dismissOutside = event => {
    if (dialog && !dialog.contains(event.target) && !bar.contains(event.target)) close()
  }
  document.addEventListener('pointerdown', dismissOutside)
  // Touch screens have no right-click, so a table's rows and columns are edited from here:
  // with the cursor in a table the strip shows its actions, and the usual ones come back after.
  const tableStrip = document.createElement('div'); tableStrip.className = 'palette-strip palette-table'
  tableStrip.setAttribute('aria-label', 'Table actions'); tableStrip.hidden = true
  let tableShown = ''
  const refreshFormats = () => {
    const on = active?.() || new Set()
    for (const format of formats) {
      if (!format.mark) continue
      for (const b of bar.querySelectorAll(`[data-command="${format.key}"]`)) {
        b.classList.toggle('active', on.has(format.key)); b.setAttribute('aria-pressed', String(on.has(format.key)))
      }
    }
  }
  const refreshTable = () => {
    const actions = table?.() || null
    const signature = actions ? actions.map(a => a.key + (a.enabled === false ? '0' : '1')).join() : ''
    if (signature === tableShown) return
    tableShown = signature
    tableStrip.replaceChildren()
    if (actions) {
      const label = document.createElement('span'); label.className = 'palette-table-label'; label.textContent = 'Table'
      tableStrip.append(label)
      for (const action of actions) {
        const b = button(action, () => {
          try { action.run() } catch { notice('That table change is not possible here.') }
          refreshTable()
        })
        b.disabled = action.enabled === false
        tableStrip.append(b)
      }
    }
    tableStrip.hidden = !actions; scroll.hidden = !!actions
    bar.classList.toggle('in-table', !!actions)
  }
  bar.append(scroll, tableStrip); document.body.append(bar); document.body.classList.add('has-command-palette')
  refreshTable(); refreshFormats()
  return { refresh: () => { refreshTable(); refreshFormats() }, toggle: () => all.click(), remove: () => { close(); document.removeEventListener('pointerdown', dismissOutside); bar.remove(); document.body.classList.remove('has-command-palette') } }
}
