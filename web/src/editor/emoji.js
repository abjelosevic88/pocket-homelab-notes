// Emoji by name: a small picker from the `/` menu, and `:rocket:` typed out. The list (GitHub's
// names, made by build.mjs into emoji.js) is a file of its own, read a moment after the editor.
import { $inputRule } from '@milkdown/kit/utils'
import { InputRule } from '@milkdown/kit/prose/inputrules'
import { editorViewCtx } from '@milkdown/kit/core'
import { clearSlash } from './shared.js'

let loading = null, byName = null
export function loadEmoji() {
  if (window.noteEmoji) { return Promise.resolve(window.noteEmoji) }
  if (!loading) {
    loading = import('gemoji').then(({ gemoji }) => {
      const groups = [...new Set(gemoji.map((e) => e.category))]
      const list = gemoji.map((e) => [e.emoji, e.names.join(' '), [...e.tags, e.description].join(' ').toLowerCase(), groups.indexOf(e.category)])
      window.noteEmoji = { groups, list }
      return window.noteEmoji
    })
  }
  return loading
}
const names = () => {
  if (!byName && window.noteEmoji) { byName = new Map(); for (const [emoji, own] of window.noteEmoji.list) { for (const name of own.split(' ')) { byName.set(name, emoji) } } }
  return byName
}

// `:tada:` with its closing colon. A name the list does not know stays as typed (`10:30:` too).
export const emojiRule = $inputRule(() => new InputRule(/:([a-z0-9_+-]{2,}):$/, (state, match, start, end) => {
  if (state.doc.resolve(start).parent.type.spec.code) { return null }
  const emoji = names() && names().get(match[1])
  return emoji ? state.tr.insertText(emoji, start, end) : null
}))

let picker = null
function close() {
  if (!picker) { return }
  document.removeEventListener('pointerdown', picker.outside, true)
  picker.box.remove(); picker = null
}

function open(view) {
  close()
  const box = document.createElement('div'); box.className = 'emoji-picker'; box.setAttribute('role', 'dialog'); box.setAttribute('aria-label', 'Emoji')
  const input = document.createElement('input'); input.type = 'search'; input.placeholder = 'Search emoji'; input.setAttribute('aria-label', 'Search emoji')
  input.autocapitalize = 'off'; input.autocomplete = 'off'; input.spellcheck = false
  const grid = document.createElement('div'); grid.className = 'emoji-grid'; grid.textContent = 'Loading…'
  box.append(input, grid); document.body.append(box)
  const at = view.coordsAtPos(view.state.selection.from)
  const width = Math.min(336, innerWidth - 16), height = Math.min(320, innerHeight - 16)
  box.style.width = width + 'px'; box.style.maxHeight = height + 'px'
  box.style.left = Math.max(8, Math.min(at.left, innerWidth - width - 8)) + 'px'
  box.style.top = (at.bottom + 8 + height > innerHeight ? Math.max(8, at.top - height - 8) : at.bottom + 8) + 'px'
  // Once it has its real height it must end inside the window.
  const fit = () => { box.style.top = Math.max(8, Math.min(parseFloat(box.style.top), innerHeight - box.offsetHeight - 8)) + 'px' }
  const outside = (event) => { if (!box.contains(event.target)) { close() } }
  document.addEventListener('pointerdown', outside, true)
  picker = { box, outside }

  const pick = (emoji) => { close(); view.dispatch(view.state.tr.insertText(emoji).scrollIntoView()); view.focus() }
  const buttons = () => [...grid.querySelectorAll('button')]
  const draw = (data) => {
    const words = input.value.trim().toLowerCase().split(/\s+/).filter(Boolean)
    grid.textContent = ''
    const add = (title, list) => {
      if (!list.length) { return }
      if (title) { const h = document.createElement('div'); h.className = 'emoji-heading'; h.textContent = title; grid.append(h) }
      const row = document.createElement('div'); row.className = 'emoji-row'
      for (const [emoji, own] of list) {
        const b = document.createElement('button'); b.type = 'button'; b.textContent = emoji
        b.title = ':' + own.split(' ')[0] + ':'; b.setAttribute('aria-label', own.split(' ')[0].replace(/_/g, ' '))
        b.addEventListener('click', () => pick(emoji)); row.append(b)
      }
      grid.append(row)
    }
    if (words.length) {
      const found = data.list.filter(([, own, more]) => words.every((w) => own.includes(w) || more.includes(w)))
      // A name that starts with what was typed comes before one that merely holds it.
      found.sort((a, b) => Number(b[1].startsWith(words[0])) - Number(a[1].startsWith(words[0])))
      if (found.length) { add('', found.slice(0, 160)) } else { grid.textContent = 'No emoji by that name.' }
    } else { data.groups.forEach((title, index) => add(title, data.list.filter((entry) => entry[3] === index))) }
  }
  loadEmoji().then((data) => { if (picker && picker.box === box) { draw(data); fit(); input.addEventListener('input', () => draw(data)) } })
    .catch((error) => { grid.textContent = String(error.message || error) })

  box.addEventListener('keydown', (event) => {
    event.stopPropagation()
    const all = buttons(), index = all.indexOf(document.activeElement)
    if (event.key === 'Escape') { event.preventDefault(); close(); view.focus(); return }
    if (event.key === 'Enter' && document.activeElement === input) { event.preventDefault(); if (all[0]) { all[0].click() } return }
    if (document.activeElement === input) { if (event.key === 'ArrowDown' && all[0]) { event.preventDefault(); all[0].focus() } return }
    if (index < 0) { return }
    const columns = Math.max(1, Math.floor(grid.clientWidth / all[0].offsetWidth))
    const step = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: columns, ArrowUp: -columns }[event.key]
    if (!step) { return }
    event.preventDefault()
    const next = index + step
    if (next < 0) { input.focus() } else if (all[next]) { all[next].focus() }
  })
  setTimeout(() => input.focus(), 0)
}

export const emojiItem = (ctx) => { clearSlash(ctx); open(ctx.get(editorViewCtx)) }
export const closeEmojiPicker = close
