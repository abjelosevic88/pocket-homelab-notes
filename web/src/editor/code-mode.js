// Code from the toolbar with only a caret: Milkdown's inline code mark ends at its last
// character (non-inclusive) and its command needs a selection, so typing would lose it after
// one letter. While this mode is on, whatever is typed at the caret is marked as code; tapping
// Code again or moving the caret elsewhere ends it.
import { $prose } from '@milkdown/kit/utils'
import { Plugin, PluginKey } from '@milkdown/kit/prose/state'

export const codeModeKey = new PluginKey('noteCodeMode')

export const codeMode = $prose(() => new Plugin({
  key: codeModeKey,
  state: {
    init: () => false,
    apply(tr, on) {
      const meta = tr.getMeta(codeModeKey)
      if (meta !== undefined) return meta
      // Moving the caret without typing ends the mode.
      if (on && tr.selectionSet && !tr.docChanged) return false
      return on
    },
  },
  appendTransaction(transactions, _old, state) {
    if (!codeModeKey.getState(state)) return null
    const code = state.schema.marks.inlineCode
    if (!code) return null
    // Everything inserted by these transactions, mapped to the final document.
    const ranges = []
    transactions.forEach((tr, index) => {
      if (!tr.docChanged || tr.getMeta(codeModeKey) !== undefined) return
      tr.mapping.maps.forEach((map, i) => map.forEach((_a, _b, start, end) => {
        if (end <= start) return
        let from = start, to = end
        for (let j = i + 1; j < tr.mapping.maps.length; j++) { from = tr.mapping.maps[j].map(from, -1); to = tr.mapping.maps[j].map(to, 1) }
        for (let k = index + 1; k < transactions.length; k++) { from = transactions[k].mapping.map(from, -1); to = transactions[k].mapping.map(to, 1) }
        if (to > from) ranges.push([from, to])
      }))
    })
    if (!ranges.length) return null
    const tr = state.tr
    for (const [from, to] of ranges) tr.addMark(from, to, code.create())
    return tr.setMeta(codeModeKey, true)
  },
}))

export function toggleCodeMode(view) {
  const on = codeModeKey.getState(view.state)
  if (on === undefined) return false
  view.dispatch(view.state.tr.setMeta(codeModeKey, !on))
  return true
}

export const codeModeOn = state => !!codeModeKey.getState(state)
