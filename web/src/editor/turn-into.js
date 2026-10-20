// The selection toolbar's first button: what the selected lines are (text, a heading, a list,
// a quote, code, a callout), and a menu that turns them into something else. The words and
// their formatting stay; only the blocks around them change, in one step of Undo.
import { editorViewCtx } from '@milkdown/kit/core'
import { Selection } from '@milkdown/kit/prose/state'
import { setBlockType, wrapIn, lift } from '@milkdown/kit/prose/commands'
import { wrapInList, liftListItem } from '@milkdown/kit/prose/schema-list'
import { svg } from './shared.js'

const KINDS = [
  { key: 'text', title: 'Text', glyph: 'T' },
  { key: 'h1', title: 'Heading 1', glyph: 'H1' },
  { key: 'h2', title: 'Heading 2', glyph: 'H2' },
  { key: 'h3', title: 'Heading 3', glyph: 'H3' },
  { key: 'bullet', title: 'Bulleted list', glyph: '•' },
  { key: 'ordered', title: 'Numbered list', glyph: '1.' },
  { key: 'task', title: 'Task list', glyph: '☐' },
  { key: 'quote', title: 'Quote', glyph: '❝' },
  { key: 'code', title: 'Code', glyph: '</>' },
  { key: 'callout', title: 'Callout', glyph: 'i' },
]
const WRAPPERS = ['list_item', 'blockquote', 'callout']
const LISTS = { bullet: 'bullet_list', task: 'bullet_list', ordered: 'ordered_list' }

/// What the line the selection starts in is; the innermost block that says so wins.
function kindAt(state) {
  const { $from } = state.selection
  const block = $from.parent
  if (block.type.name === 'heading') return { key: 'h' + block.attrs.level, title: 'Heading ' + block.attrs.level }
  if (block.type.name === 'code_block') return KINDS.find(kind => kind.key === 'code')
  for (let depth = $from.depth - 1; depth > 0; depth--) {
    const node = $from.node(depth), name = node.type.name
    let key = null
    if (name === 'list_item') key = node.attrs.checked != null ? 'task' : $from.node(depth - 1).type.name === 'ordered_list' ? 'ordered' : 'bullet'
    else if (name === 'blockquote') key = 'quote'
    else if (name === 'callout') key = 'callout'
    if (key) return KINDS.find(kind => kind.key === key)
  }
  return KINDS[0]
}

/// The list both ends of the selection sit in, if they share one: a list becomes another
/// kind of list where it stands, nesting and all.
function sharedList(state) {
  const { $from, $to } = state.selection
  for (let depth = $from.depth; depth > 0; depth--) {
    const node = $from.node(depth)
    if (node.type.name !== 'bullet_list' && node.type.name !== 'ordered_list') continue
    return $to.depth >= depth && $to.node(depth) === node ? { node, pos: $from.before(depth) } : null
  }
  return null
}

function turnInto(view, key) {
  const { schema } = view.state, nodes = schema.nodes
  let state = view.state
  const done = []
  const apply = (command) => {
    let next = null
    command(state, tr => { next = tr })
    if (!next || !next.docChanged) return false
    // With what the editor's own plugins add to it (list numbers), so the next step fits.
    const result = state.applyTransaction(next)
    state = result.state; done.push(...result.transactions)
    return true
  }
  const markItems = (checked) => apply((s, dispatch) => {
    const { from, to } = s.selection, tr = s.tr
    s.doc.nodesBetween(from, to, (node, pos) => {
      if (node.type === nodes.list_item && (node.attrs.checked ?? null) !== checked) tr.setNodeAttribute(pos, 'checked', checked)
    })
    dispatch(tr)
    return true
  })

  const list = LISTS[key] && sharedList(state)
  if (list) {
    const type = nodes[LISTS[key]]
    if (list.node.type !== type) apply((s, dispatch) => { dispatch(s.tr.setNodeMarkup(list.pos, type)); return true })
    markItems(key === 'task' ? false : null)
  } else {
    // Out of the lists, quotes and callouts first: what the lines become says all they are.
    for (let round = 0; round < 20; round++) {
      const { $from } = state.selection
      let wrapper = null
      for (let depth = $from.depth; depth > 0 && !wrapper; depth--) if (WRAPPERS.includes($from.node(depth).type.name)) wrapper = $from.node(depth).type
      if (!wrapper || !apply(wrapper === nodes.list_item ? liftListItem(wrapper) : lift)) break
    }
    if (key === 'text') apply(setBlockType(nodes.paragraph))
    else if (/^h\d$/.test(key)) apply(setBlockType(nodes.heading, { level: Number(key[1]) }))
    else if (key === 'code') apply(toCode(nodes.code_block))
    else {
      apply(setBlockType(nodes.paragraph))
      if (LISTS[key]) {
        apply(wrapInList(nodes[LISTS[key]]))
        if (key === 'task') markItems(false)
      } else if (key === 'quote') apply(wrapIn(nodes.blockquote))
      else if (key === 'callout' && nodes.callout) apply(wrapIn(nodes.callout, { kind: 'info' }))
    }
  }
  if (!done.length) return
  const tr = view.state.tr
  for (const step of done.flatMap(t => t.steps)) tr.step(step)
  tr.setSelection(Selection.fromJSON(tr.doc, state.selection.toJSON()))
  view.dispatch(tr.scrollIntoView())
}

// Several lines become one code block, a line each, rather than a block per line.
const toCode = (type) => (state, dispatch) => {
  const { $from, $to } = state.selection
  const range = $from.blockRange($to)
  if (!range || range.endIndex - range.startIndex < 2) return setBlockType(type)(state, dispatch)
  const lines = []
  for (let i = range.startIndex; i < range.endIndex; i++) lines.push(range.parent.child(i).textContent)
  const text = lines.join('\n')
  const tr = state.tr.replaceWith(range.start, range.end, type.create(null, text ? state.schema.text(text) : null))
  tr.setSelection(Selection.near(tr.doc.resolve(range.start + 1)))
  dispatch(tr)
  return true
}

// The menu under the button. The editor keeps the focus and the selection while it is open;
// the arrow keys, Return and Escape work it from the keyboard.
let menu = null
function closeMenu() {
  if (!menu) return
  menu.element.remove()
  document.removeEventListener('keydown', menu.keys, true)
  document.removeEventListener('pointerdown', menu.outside, true)
  menu = null
}

function openMenu(ctx) {
  const view = ctx.get(editorViewCtx)
  const button = document.querySelector('.milkdown-toolbar [data-toolbar-item="turn-into"]')
  if (!button || !view.editable) return
  const current = kindAt(view.state).key
  const element = document.createElement('div')
  element.className = 'turn-into-menu'
  element.setAttribute('role', 'menu')
  const choose = (key) => { closeMenu(); turnInto(view, key); view.focus() }
  const items = KINDS.map(kind => {
    const item = document.createElement('button')
    item.type = 'button'; item.setAttribute('role', 'menuitemradio'); item.dataset.kind = kind.key
    item.setAttribute('aria-checked', String(kind.key === current))
    item.innerHTML = '<span class="turn-into-glyph"></span><span class="turn-into-title"></span>' + (kind.key === current ? svg('<path d="M5 12.5l4.5 4.5L19 7.5"/>') : '')
    item.querySelector('.turn-into-glyph').textContent = kind.glyph
    item.querySelector('.turn-into-title').textContent = kind.title
    item.addEventListener('pointerdown', event => event.preventDefault())
    item.addEventListener('pointerenter', () => highlight(items.indexOf(item)))
    item.addEventListener('click', event => { event.preventDefault(); choose(kind.key) })
    element.append(item)
    return item
  })
  let index = Math.max(0, KINDS.findIndex(kind => kind.key === current))
  const highlight = (at) => { index = at; items.forEach((item, i) => item.classList.toggle('highlighted', i === at)) }
  highlight(index)
  document.body.append(element)
  const anchor = button.getBoundingClientRect(), size = element.getBoundingClientRect()
  const below = anchor.bottom + 6 + size.height <= window.innerHeight - 8
  element.style.left = Math.max(8, Math.min(anchor.left, window.innerWidth - size.width - 8)) + 'px'
  element.style.top = Math.max(8, below ? anchor.bottom + 6 : anchor.top - 6 - size.height) + 'px'

  const keys = (event) => {
    const moves = { ArrowDown: 1, ArrowUp: -1 }
    if (event.key in moves) highlight((index + moves[event.key] + items.length) % items.length)
    else if (event.key === 'Enter') choose(KINDS[index].key)
    else if (event.key === 'Escape') closeMenu()
    else { closeMenu(); return }
    event.preventDefault(); event.stopPropagation()
  }
  const outside = (event) => { if (!element.contains(event.target) && !button.contains(event.target)) closeMenu() }
  document.addEventListener('keydown', keys, true)
  document.addEventListener('pointerdown', outside, true)
  menu = { element, keys, outside, selection: view.state.selection }
}

/// Crepe's `buildToolbar`: the button goes first, as what the text is comes before how it looks.
export function turnIntoButton(builder) {
  builder.addGroup('turn-into', 'Turn into').addItem('turn-into', {
    icon: svg('<path d="M7 10l5 5 5-5"/>'),
    label: 'Turn into',
    // Asked each time the toolbar redraws, which is each time the selection moves: the button
    // names what is selected, and a menu left open for another selection goes away.
    active: (ctx) => {
      const view = ctx.get(editorViewCtx)
      if (menu && !view.state.selection.eq(menu.selection)) closeMenu()
      const title = kindAt(view.state).title
      queueMicrotask(() => {
        const button = document.querySelector('.milkdown-toolbar [data-toolbar-item="turn-into"]')
        if (button) button.dataset.kind = title
      })
      return false
    },
    onRun: (ctx) => { if (menu) closeMenu(); else openMenu(ctx) },
  })
  const groups = builder.build()
  groups.unshift(groups.pop())
}

export { closeMenu as closeTurnIntoMenu }
