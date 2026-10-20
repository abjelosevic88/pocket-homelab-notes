// A table of contents: a list of links to the note's headings. The anchors are the ones
// Nextcloud's editor gives its headings (`h-` and the slug, `--1` for a repeat), so the list
// works on the web as well. Asking for it again brings the list that is there up to date.
import slug from 'slug'
import { editorViewCtx, parserCtx } from '@milkdown/kit/core'
import { TextSelection } from '@milkdown/kit/prose/state'
import { clearSlash, insertBlocks, notice } from './shared.js'

export function headings(doc) {
  const seen = new Map(), found = []
  doc.descendants((node, pos) => {
    // As in Nextcloud: headings inside a folded section or a quote are not part of the outline.
    if (node.type.name === 'details' || node.type.name === 'blockquote') { return false }
    if (node.type.name !== 'heading') { return }
    const text = node.textContent.trim(); if (!text) { return false }
    const base = slug(text), count = seen.get(base) || 0
    seen.set(base, count + 1)
    found.push({ level: node.attrs.level, text, pos, id: count ? `h-${base}--${count}` : `h-${base}` })
    return false
  })
  return found
}

// `#h-rack-plan`, and the plainer `#rack-plan` other tools write.
export function findHeading(doc, anchor) {
  const wanted = decodeURIComponent(anchor.replace(/^#/, '')).toLowerCase(), all = headings(doc)
  return all.find((h) => h.id === wanted) || all.find((h) => h.id === 'h-' + wanted) || all.find((h) => slug(h.text) === slug(wanted)) || null
}

export function contentsMarkdown(all) {
  if (!all.length) { return '' }
  let list = all
  // The note's own title, alone at the top, is not an entry of its contents.
  const top = Math.min(...all.map((h) => h.level))
  if (all[0].level === top && all.filter((h) => h.level === top).length === 1 && all.length > 1) { list = all.slice(1) }
  const least = Math.min(...list.map((h) => h.level))
  let indent = 0, previous = least
  return list.map((h, index) => {
    // A jump from a second-level heading to a fourth is still one step in.
    indent = index === 0 ? 0 : h.level > previous ? indent + 1 : h.level < previous ? Math.max(0, indent - (previous - h.level)) : indent
    if (h.level === least) { indent = 0 }
    previous = h.level
    return '  '.repeat(indent) + '- [' + h.text.replace(/([\[\]\\])/g, '\\$1') + '](#' + h.id + ')'
  }).join('\n')
}

// The list a contents entry made earlier: every entry is one link to an anchor.
function existingContents(doc) {
  let found = null
  doc.forEach((node, pos) => {
    if (found || node.type.name !== 'bullet_list') { return }
    let links = 0, other = false
    node.descendants((child) => {
      if (!child.isText) { return }
      const link = child.marks.find((mark) => mark.type.name === 'link')
      if (link && String(link.attrs.href || '').startsWith('#')) { links++ } else if (child.text.trim()) { other = true }
    })
    if (links > 0 && !other) { found = { pos, node } }
  })
  return found
}

export const contentsItem = (ctx) => {
  clearSlash(ctx)
  const view = ctx.get(editorViewCtx)
  const markdown = contentsMarkdown(headings(view.state.doc))
  if (!markdown) { notice('This note has no headings yet. Add some, then ask for the contents again.'); return }
  const parsed = ctx.get(parserCtx)(markdown)
  if (!parsed) { return }
  const old = existingContents(view.state.doc)
  if (old) {
    view.dispatch(view.state.tr.replaceWith(old.pos, old.pos + old.node.nodeSize, parsed.content).scrollIntoView())
    notice('The table of contents is up to date.'); view.focus(); return
  }
  insertBlocks(ctx, parsed.content, 'end')
}

export function jumpTo(view, pos) {
  const tr = view.state.tr.setSelection(TextSelection.near(view.state.doc.resolve(pos + 1)))
  view.dispatch(tr)
  const at = view.domAtPos(pos + 1).node, element = at.nodeType === 1 ? at : at.parentElement
  if (element && element.scrollIntoView) { element.scrollIntoView({ block: 'start', behavior: 'smooth' }) }
}
