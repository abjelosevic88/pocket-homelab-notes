// Small things put into a sentence: a date, a footnote. Each has an entry in the `/` menu (which
// opens on a fresh line) and something to type in the middle of one: `@today`, `[^1]`.
// (A link to another note is notelinks.js.)
import { $inputRule } from '@milkdown/kit/utils'
import { InputRule } from '@milkdown/kit/prose/inputrules'
import { editorViewCtx } from '@milkdown/kit/core'
import { footnoteReferenceSchema, footnoteDefinitionSchema } from '@milkdown/kit/preset/gfm'
import { TextSelection } from '@milkdown/kit/prose/state'
import { clearSlash, insertText } from './shared.js'

// MARK: Dates

const day = (offset) => { const d = new Date(); d.setDate(d.getDate() + offset); return d }
const long = (d) => new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }).format(d)
const clock = (d) => new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(d)
const iso = (d) => d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0')
export const stamps = {
  today: () => long(day(0)), tomorrow: () => long(day(1)), yesterday: () => long(day(-1)),
  now: () => new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date()), time: () => clock(new Date()), date: () => iso(day(0)),
}
export const weekday = () => new Intl.DateTimeFormat(undefined, { weekday: 'long' }).format(new Date())

export const stampItem = (kind) => (ctx) => { clearSlash(ctx); insertText(ctx, stamps[kind]() + ' ') }

// `@today` and a space, anywhere text is typed.
export const stampRule = $inputRule(() => new InputRule(/(?:^|\s)@(today|tomorrow|yesterday|now|time|date)\s$/, (state, match, start, end) => {
  if (state.doc.resolve(start).parent.type.spec.code) { return null }
  const lead = match[0].startsWith('@') ? 0 : 1
  return state.tr.insertText(stamps[match[1]]() + ' ', start + lead, end)
}))

// MARK: Footnotes

function nextLabel(doc) {
  let highest = 0
  doc.descendants((node) => {
    if (node.type.name === 'footnote_reference' || node.type.name === 'footnote_definition') { highest = Math.max(highest, parseInt(node.attrs.label, 10) || 0) }
  })
  return String(highest + 1)
}

// The mark in the sentence, its text at the end of the note, and the cursor in that text.
function addFootnote(ctx, tr, label, moveCursor) {
  const reference = footnoteReferenceSchema.type(ctx), definition = footnoteDefinitionSchema.type(ctx)
  tr = tr.replaceSelectionWith(reference.create({ label }))
  let exists = false
  tr.doc.forEach((node) => { if (node.type === definition && node.attrs.label === label) { exists = true } })
  if (!exists) {
    const end = tr.doc.content.size
    tr = tr.insert(end, definition.create({ label }, tr.doc.type.schema.nodes.paragraph.create()))
    if (moveCursor) { tr = tr.setSelection(TextSelection.create(tr.doc, end + 2)) }
  }
  return tr
}

export const footnoteItem = (ctx) => {
  clearSlash(ctx)
  const view = ctx.get(editorViewCtx)
  view.dispatch(addFootnote(ctx, view.state.tr, nextLabel(view.state.doc), true).scrollIntoView()); view.focus()
}

// `[^1]` typed out becomes the mark, and gets its text at the end when it has none yet.
export const footnoteRule = $inputRule((ctx) => new InputRule(/\[\^([^\s\]]+)\]$/, (state, match, start, end) => {
  if (state.doc.resolve(start).parent.type.spec.code) { return null }
  const tr = state.tr.delete(start, end)
  return addFootnote(ctx, tr, match[1], false)
}))

export function findFootnote(doc, label) {
  let found = null
  doc.forEach((node, pos) => { if (!found && node.type.name === 'footnote_definition' && node.attrs.label === label) { found = pos } })
  return found
}

export const insertPlugins = [stampRule, footnoteRule]
