// Milkdown keeps an empty line by writing `<br />` for it, and an empty list item as `- <br />`.
// Those tags pile up in the note and show as text elsewhere, so this editor does not write them:
// an empty line is simply not part of the Markdown. The ones already in a note (on a line, or as
// all there is in a list item, quote or footnote) are dropped when it is read, so the next save is clean.
// A `<br />` in the middle of text, a line break inside a table cell for example, is left alone.
import { $remark } from '@milkdown/kit/utils'
import { remarkPreserveEmptyLinePlugin } from '@milkdown/kit/preset/commonmark'
import { extendListItemSchemaForTask } from '@milkdown/kit/preset/gfm'

const BREAK = /^<br\s*\/?>$/i
const isBreak = (node) => node.type === 'html' && BREAK.test(String(node.value || '').trim())

const FLOW = new Set(['root', 'blockquote', 'listItem', 'containerDirective', 'details', 'footnoteDefinition'])

function clean(parent) {
  if (!Array.isArray(parent.children)) { return }
  parent.children = parent.children.filter((node) => {
    // Only where a block could stand. Inside a sentence or a table cell it is a line break someone wanted.
    if (isBreak(node) && FLOW.has(parent.type)) { return false }
    if (node.type === 'paragraph' && FLOW.has(parent.type) && node.children.length && node.children.every((child) => isBreak(child) || (child.type === 'text' && !child.value.trim()))) { return false }
    return true
  })
  for (const node of parent.children) {
    clean(node)
    // A box with nothing behind it (`- [ ]`) is a bullet that says "[ ]" to Markdown, which wants
    // text after a box. It is what an empty task looks like once no `<br />` props it up.
    if (node.type === 'listItem' && node.checked == null && node.children.length === 1 && node.children[0].type === 'paragraph') {
      const only = node.children[0].children
      const box = only.length === 1 && only[0].type === 'text' ? only[0].value.trim().match(/^\[( |x|X)\]$/) : null
      if (box) { node.checked = box[1] !== ' '; node.children = [] }
    }
  }
}

// Markdown's writer only puts a box in front of text. An empty task gets a mark to stand in
// for its text, which `tidyEmptyLines` takes away again: `- [ ]`.
const HOLD = '\uE000'
export const emptyTaskKeepsItsBox = extendListItemSchemaForTask.extendSchema((prev) => (ctx) => {
  const base = prev(ctx)
  return {
    ...base,
    toMarkdown: {
      match: base.toMarkdown.match,
      runner: (state, node) => {
        const empty = node.attrs.checked != null && node.childCount === 1 && node.firstChild.type.name === 'paragraph' && node.firstChild.content.size === 0
        if (!empty) { base.toMarkdown.runner(state, node); return }
        const { label, listType, spread, checked } = node.attrs
        state.openNode('listItem', undefined, { label, listType, spread, checked })
        state.openNode('paragraph'); state.addNode('text', undefined, HOLD); state.closeNode()
        state.closeNode()
      },
    },
  }
})

export const remarkNoEmptyLineTags = $remark('remarkNoEmptyLineTags', () => () => (tree) => { clean(tree) })

// Before the editor is created: without Milkdown's plugin its paragraphs stop writing the tag.
export const withoutEmptyLineTags = async (editor) => { await editor.remove(remarkPreserveEmptyLinePlugin); return editor.use(remarkNoEmptyLineTags).use(emptyTaskKeepsItsBox) }

// An empty paragraph now writes nothing, which leaves a run of empty lines; outside code, one is enough.
// A box left without text ends in a space Markdown does not need.
export function tidyEmptyLines(markdown) {
  const out = []
  let fence = null, blank = 0
  for (const line of markdown.split(HOLD).join('').split('\n')) {
    // A `$$` formula is kept as written too, empty lines and all.
    const mark = line.match(/^\s*(```+|~~~+|\$\$\s*$)/)
    if (mark) { if (!fence) { fence = mark[1][0] } else if (mark[1][0] === fence) { fence = null } }
    if (!fence && !mark && !line.trim()) { if (++blank > 1) { continue } out.push(''); continue }
    blank = 0
    out.push(fence || mark ? line : line.replace(/^(\s*(?:(?:[-*+]|\d+[.)])(?: \[[ xX]\])?|\[\^[^\]]+\]:))\s+$/, '$1'))
  }
  while (out.length && out[0] === '') { out.shift() }
  return out.join('\n')
}
