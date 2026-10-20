// A section that folds away: `<details><summary>…</summary> … </details>`, the form Nextcloud's
// own editor reads and writes. Whether it is open is part of the note (`<details open>`), as there.
import { $node, $remark, $view, $prose } from '@milkdown/kit/utils'
import { Plugin, TextSelection } from '@milkdown/kit/prose/state'
import { editorViewCtx } from '@milkdown/kit/core'
import { clearSlash } from './shared.js'

const OPEN = /^<details(\s+open(?:=(?:""|''|"open"|open))?)?\s*>/i
const CLOSE = /^<\/details>$/i

// Milkdown wraps a block of HTML into a paragraph; whichever ran first, this is the HTML.
const htmlOf = (node) => node.type === 'html' ? node.value
  : node.type === 'paragraph' && node.children && node.children.length === 1 && node.children[0].type === 'html' ? node.children[0].value : null

function fold(parent) {
  const kids = parent.children
  if (!Array.isArray(kids)) { return }
  for (let i = 0; i < kids.length; i++) {
    const head = htmlOf(kids[i]); const m = head && head.trim().match(OPEN)
    if (!m) { continue }
    let rest = head.trim().slice(m[0].length).trim(), start = i + 1
    if (!rest && start < kids.length) {
      const next = htmlOf(kids[start])
      if (next && /^<summary>/i.test(next.trim())) { rest = next.trim(); start++ }
    }
    const summary = rest.match(/^<summary>([\s\S]*?)<\/summary>$/i)
    if (!summary) { continue }
    let depth = 0, end = -1
    for (let j = start; j < kids.length; j++) {
      const value = htmlOf(kids[j]); if (value == null) { continue }
      const text = value.trim()
      if (OPEN.test(text)) { depth++ } else if (CLOSE.test(text)) { if (depth === 0) { end = j; break } depth-- }
    }
    // Not closed, or no title: it stays the HTML it is, untouched.
    if (end < 0) { continue }
    kids.splice(i, end - i + 1, { type: 'details', open: !!m[1], summary: summary[1].replace(/\s+/g, ' ').trim(), children: kids.slice(start, end) })
  }
  for (const kid of kids) { fold(kid) }
}

export const remarkDetails = $remark('remarkDetails', () => () => (tree) => { fold(tree) })

export const detailsSummary = $node('details_summary', () => ({
  content: 'text*', marks: '', defining: true, selectable: false,
  parseDOM: [{ tag: 'summary' }, { tag: 'div.details-summary' }],
  toDOM: () => ['div', { class: 'details-summary' }, 0],
  parseMarkdown: { match: () => false, runner: () => {} },
  // Written by the section it belongs to.
  toMarkdown: { match: (node) => node.type.name === 'details_summary', runner: () => {} },
}))

export const details = $node('details', () => ({
  content: 'details_summary block+', group: 'block', defining: true, isolating: true,
  attrs: { open: { default: false } },
  parseDOM: [{ tag: 'details', getAttrs: (dom) => ({ open: dom.hasAttribute('open') }) },
    { tag: 'div[data-details]', getAttrs: (dom) => ({ open: dom.getAttribute('data-details') === 'open' }) }],
  toDOM: (node) => ['div', { 'data-details': node.attrs.open ? 'open' : 'closed', class: 'details' }, 0],
  parseMarkdown: {
    match: (node) => node.type === 'details',
    runner: (state, node, type) => {
      state.openNode(type, { open: !!node.open })
      state.openNode(state.schema.nodes.details_summary)
      if (node.summary) { state.addText(node.summary) }
      state.closeNode()
      if (node.children && node.children.length) { state.next(node.children) } else { state.addNode(state.schema.nodes.paragraph) }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'details',
    runner: (state, node) => {
      const title = node.firstChild ? node.firstChild.textContent : ''
      state.addNode('html', undefined, `<details${node.attrs.open ? ' open' : ''}>\n<summary>${title}</summary>`)
      node.forEach((child, _, index) => { if (index > 0) { state.next(child) } })
      state.addNode('html', undefined, '</details>')
    },
  },
}))

export const detailsView = $view(details, () => (initial, view, getPos) => {
  let node = initial
  const dom = document.createElement('div'); dom.className = 'details'
  const toggle = document.createElement('button'); toggle.type = 'button'; toggle.className = 'details-toggle'; toggle.contentEditable = 'false'
  toggle.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>'
  const body = document.createElement('div'); body.className = 'details-body'
  dom.append(toggle, body)
  const show = (open) => { dom.classList.toggle('open', open); toggle.setAttribute('aria-expanded', String(open)); toggle.setAttribute('aria-label', open ? 'Fold this section' : 'Unfold this section') }
  show(node.attrs.open)
  toggle.addEventListener('mousedown', (event) => event.preventDefault())
  toggle.addEventListener('click', (event) => {
    event.preventDefault()
    // A note that cannot be changed still folds and unfolds, only on screen.
    if (!view.editable) { show(!dom.classList.contains('open')); return }
    const pos = getPos(); if (pos == null) { return }
    const open = !node.attrs.open
    let tr = view.state.tr.setNodeMarkup(pos, undefined, { ...node.attrs, open })
    // Folding it with the cursor inside would leave the cursor in what cannot be seen.
    const { from } = view.state.selection
    if (!open && from > pos + 1 + node.firstChild.nodeSize && from < pos + node.nodeSize) { tr = tr.setSelection(TextSelection.create(tr.doc, pos + 1 + node.firstChild.nodeSize - 1)) }
    view.dispatch(tr)
  })
  return {
    dom, contentDOM: body,
    update(next) { if (next.type !== node.type) { return false } node = next; show(node.attrs.open); return true },
    stopEvent: (event) => toggle.contains(event.target),
    ignoreMutation: (mutation) => mutation.type !== 'selection' && !body.contains(mutation.target),
  }
})

export const detailsBehaviour = $prose(() => new Plugin({
  // A cursor that lands in a folded section (arrow keys, a search, a jump from the contents) unfolds it.
  appendTransaction(transactions, _, state) {
    if (!transactions.some((tr) => tr.selectionSet || tr.docChanged)) { return null }
    const { $from } = state.selection
    let tr = null
    for (let depth = $from.depth; depth > 0; depth--) {
      const node = $from.node(depth)
      if (node.type.name === 'details' && !node.attrs.open && $from.index(depth) > 0) {
        tr = (tr || state.tr).setNodeMarkup($from.before(depth), undefined, { ...node.attrs, open: true })
      }
    }
    return tr
  },
  props: {
    // Backspace at the very start of the title takes the section apart: its title and its blocks stay.
    handleKeyDown(view, event) {
      if (event.key !== 'Backspace') { return false }
      const { $from, empty } = view.state.selection
      if (!empty || $from.parent.type.name !== 'details_summary' || $from.parentOffset > 0) { return false }
      const depth = $from.depth - 1, section = $from.node(depth), schema = view.state.schema
      const blocks = []
      if (section.firstChild.content.size) { blocks.push(schema.nodes.paragraph.create(null, section.firstChild.content)) }
      section.forEach((child, _, index) => { if (index > 0) { blocks.push(child) } })
      const at = $from.before(depth)
      const tr = view.state.tr.replaceWith(at, at + section.nodeSize, blocks)
      tr.setSelection(TextSelection.near(tr.doc.resolve(at + 1)))
      view.dispatch(tr.scrollIntoView()); return true
    },
  },
}))

export const detailsPlugins = [remarkDetails, detailsSummary, details, detailsView, detailsBehaviour]

// `/` → Collapsible section: open, with the cursor in its title.
export const detailsItem = (ctx) => {
  clearSlash(ctx)
  const view = ctx.get(editorViewCtx), { state } = view, schema = state.schema
  const node = schema.nodes.details.create({ open: true }, [schema.nodes.details_summary.create(), schema.nodes.paragraph.create()])
  const $from = state.selection.$from
  const empty = $from.parent.content.size === 0 && $from.depth >= 1
  const at = empty ? $from.before() : $from.after()
  const tr = empty ? state.tr.replaceWith(at, $from.after(), node) : state.tr.insert(at, node)
  tr.setSelection(TextSelection.create(tr.doc, at + 2))
  view.dispatch(tr.scrollIntoView()); view.focus()
}

// Nextcloud writes the title and the closing tag right against the text. Markdown reads HTML up
// to the next empty line, so without one the section's text would be taken for HTML as well.
export const detailsFromNextcloud = (line) => {
  if (/^\s*(<details[^>]*>\s*)?<summary>.*<\/summary>\s*$/i.test(line)) { return line + '\n' }
  if (/^\s*<\/details>\s*$/i.test(line)) { return '\n' + line + '\n' }
  return line
}
