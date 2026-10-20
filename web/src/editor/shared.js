// What the editor's tools share: the way to the app, icons, a short notice, and putting blocks in.
import { editorViewCtx, parserCtx, commandsCtx } from '@milkdown/kit/core'
import { clearTextInCurrentBlockCommand } from '@milkdown/kit/preset/commonmark'
import { TextSelection, NodeSelection } from '@milkdown/kit/prose/state'

// The editor runs in a frame of Pocket Notes' own page (the same site), which plays the part the
// app plays on the phone: it hears what happens here and answers what is asked.
export const post = (name, body) => {
  try { window.parent?.__noteHost?.(Object.assign({ name }, body || {})) } catch (e) {}
}
export const request = (name, body) => {
  try {
    const ask = window.parent?.__noteHostRequest
    if (ask) { return Promise.resolve(ask(Object.assign({ name }, body || {}))) }
  } catch (e) {}
  return Promise.reject(new Error('No page to ask'))
}

export const svg = (body) => `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${body}</svg>`

// A line at the bottom that goes away by itself: for a tool that had nothing to do.
let noticeTimer = null
export function notice(text) {
  let box = document.getElementById('editor-notice')
  if (!box) { box = document.createElement('div'); box.id = 'editor-notice'; box.setAttribute('role', 'status'); document.body.append(box) }
  box.textContent = text; box.classList.add('shown')
  clearTimeout(noticeTimer); noticeTimer = setTimeout(() => box.classList.remove('shown'), 3200)
}

// The `/` menu leaves its own text (`/toc`) on the line; every tool starts by taking it away.
export const clearSlash = (ctx) => ctx.get(commandsCtx).call(clearTextInCurrentBlockCommand.key)

// Words at the cursor, as typing would put them.
export function insertText(ctx, text) {
  const view = ctx.get(editorViewCtx)
  view.dispatch(view.state.tr.insertText(text).scrollIntoView()); view.focus()
}

// Blocks as blocks of their own: in place of the empty line the cursor is on, or after the block
// it is in. Dropped into the middle of a heading they would cut the heading in two.
// `select`: 'node' keeps the last block selected (the next file follows it), 'start' and 'end'
// put the text cursor at that end of what went in.
export function insertBlocks(ctx, content, select) {
  const view = ctx.get(editorViewCtx)
  const { state } = view, $from = state.selection.$from
  const top = $from.depth >= 1 ? $from.node(1) : null
  const emptyLine = top && top.type.name === 'paragraph' && top.content.size === 0
  // A selected block (the picture put in a moment ago) has no depth: what comes next goes right behind it.
  const at = emptyLine ? $from.before(1) : ($from.depth >= 1 ? $from.after(1) : state.selection.to)
  const tr = emptyLine ? state.tr.replaceWith(at, $from.after(1), content) : state.tr.insert(at, content)
  const size = content.size != null ? content.size : content.nodeSize
  try {
    if (select === 'node') {
      const last = content.lastChild || content
      tr.setSelection(NodeSelection.create(tr.doc, at + size - last.nodeSize))
    } else if (select === 'start') { tr.setSelection(TextSelection.near(tr.doc.resolve(at + 1), 1)) }
    else { tr.setSelection(TextSelection.near(tr.doc.resolve(at + size - 1), -1)) }
  } catch (e) {}
  view.dispatch(tr.scrollIntoView()); view.focus()
  return at
}

export const insertMarkdownBlocks = (ctx, markdown, select) => {
  const parsed = ctx.get(parserCtx)(markdown)
  if (!parsed || !parsed.content.size) { return false }
  insertBlocks(ctx, parsed.content, select); return true
}
