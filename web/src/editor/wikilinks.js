// `[[Another note]]` as Obsidian and other notes apps write it: shown as a link, and followed
// like one (⌘-click on a computer, a tap on a touch screen). The text stays as typed; the page
// finds the note by its title.
import { $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import { post } from './shared.js'

const WIKI = /\[\[([^\[\]\n|#]+)(?:#[^\[\]\n|]*)?(?:\|([^\[\]\n]*))?\]\]/g

function decorate(doc) {
  const found = []
  doc.descendants((node, pos, parent) => {
    if (parent && parent.type.spec.code) { return false }
    if (!node.isText || !node.text || node.marks.some((m) => m.type.name === 'inlineCode')) { return }
    WIKI.lastIndex = 0
    let m
    while ((m = WIKI.exec(node.text))) {
      found.push(Decoration.inline(pos + m.index, pos + m.index + m[0].length, { class: 'wikilink', 'data-target': m[1].trim(), title: m[1].trim() + ' (⌘-click opens it)' }))
    }
  })
  return DecorationSet.create(doc, found)
}

export const wikiLinks = $prose(() => new Plugin({
  state: {
    init: (_, state) => decorate(state.doc),
    apply: (tr, old) => (tr.docChanged ? decorate(tr.doc) : old),
  },
  props: {
    decorations(state) { return this.getState(state) },
    handleDOMEvents: {
      click: (view, event) => {
        const el = event.target && event.target.closest ? event.target.closest('.wikilink') : null
        if (!el) { return false }
        const touch = event.pointerType === 'touch' || window.matchMedia('(hover: none)').matches
        if (!(event.metaKey || event.ctrlKey || touch || !view.editable)) { return false }
        event.preventDefault()
        post('link', { url: '', raw: '[[' + el.dataset.target + ']]' })
        return true
      },
    },
  },
}))
