// External links share the editor's existing link popover. Pasting a URL over prose
// adds a mark in one undoable transaction instead of replacing the selected words.
import { editorViewCtx } from '@milkdown/kit/core'
import { linkTooltipAPI } from '@milkdown/kit/component/link-tooltip'
import { $prose } from '@milkdown/kit/utils'
import { Plugin, TextSelection } from '@milkdown/kit/prose/state'
import { post } from './shared.js'

export function openExternalLink(ctx) {
  const view = ctx.get(editorViewCtx)
  if (!view.editable) return false
  const { from, to, empty } = view.state.selection
  // Selected words become the link's text, so only the address is asked for.
  if (!empty) { ctx.get(linkTooltipAPI.key).addLink(from, to); return true }
  return showLinkForm(view)
}

/// An address someone typed, made absolute: `example.com` means https.
export function normalizeLink(input) {
  let href = input.trim()
  if (!href || /\s/.test(href)) return null
  if (!/^[a-z][a-z0-9+.-]*:/i.test(href)) href = (/^[^@/]+@[^@/]+\.[^@/]+$/.test(href) ? 'mailto:' : 'https://') + href
  try {
    const url = new URL(href)
    if (!['https:', 'http:', 'mailto:'].includes(url.protocol)) return null
    if (url.protocol !== 'mailto:' && !url.hostname.includes('.') && url.hostname !== 'localhost') return null
  } catch { return null }
  return href
}

let form = null, outside = null
export const closeLinkForm = () => {
  // The floating note controls step aside while the form is up, as for the insert menu.
  if (form) { post('picker', { open: false }); document.body.classList.remove('picker-open'); document.dispatchEvent(new Event('note-picker-closed')) }
  form?.remove(); form = null
  if (outside) { document.removeEventListener('pointerdown', outside, true); outside = null }
}

/// Nothing selected: ask for the text and the address, then insert them as one link.
export function showLinkForm(view) {
  closeLinkForm()
  const { state } = view, { $from } = state.selection
  const link = state.schema.marks.link
  if (!link || $from.parent.type.spec.code || !$from.parent.inlineContent) return false
  const at = state.selection.getBookmark()
  form = document.createElement('form'); form.className = 'note-link-form'
  // The browser would refuse `example.com`; normalizeLink accepts it and adds https.
  form.noValidate = true
  form.setAttribute('role', 'dialog'); form.setAttribute('aria-label', 'Insert link')
  const field = (label, attrs) => {
    const input = document.createElement('input'); Object.assign(input, attrs)
    input.setAttribute('aria-label', label); input.autocomplete = 'off'
    form.append(input); return input
  }
  const text = field('Link text', { type: 'text', placeholder: 'Text', name: 'text' })
  const url = field('Link address', { type: 'url', placeholder: 'Paste link…', name: 'url', inputMode: 'url', spellcheck: false })
  url.setAttribute('autocapitalize', 'off'); url.setAttribute('autocorrect', 'off')
  const actions = document.createElement('div'); actions.className = 'note-link-actions'
  const cancel = document.createElement('button'); cancel.type = 'button'; cancel.textContent = 'Cancel'
  const insert = document.createElement('button'); insert.type = 'submit'; insert.textContent = 'Insert'; insert.className = 'primary'
  actions.append(cancel, insert); form.append(actions)
  const refresh = () => { insert.disabled = !normalizeLink(url.value) }
  url.addEventListener('input', refresh); refresh()
  const back = () => { closeLinkForm(); view.focus() }
  cancel.addEventListener('click', back)
  form.addEventListener('keydown', e => { if (e.key === 'Escape') { e.preventDefault(); back() } })
  // Enter in Text moves on to the address rather than inserting without one.
  text.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); url.focus() } })
  form.addEventListener('submit', e => {
    e.preventDefault()
    const href = normalizeLink(url.value)
    if (!href) return
    const label = text.value.trim() || url.value.trim()
    const tr = view.state.tr
    try { tr.setSelection(at.resolve(tr.doc)) } catch {}
    const mark = link.create({ href })
    tr.replaceSelectionWith(view.state.schema.text(label, [mark]), false)
    // Typing on after the link is ordinary text.
    tr.removeStoredMark(link)
    view.dispatch(tr.scrollIntoView())
    back()
  })
  document.body.append(form)
  post('picker', { open: true }); document.body.classList.add('picker-open')
  outside = e => { if (form && !form.contains(e.target)) closeLinkForm() }
  document.addEventListener('pointerdown', outside, true)
  if (!document.body.classList.contains('phone')) {
    const coords = view.coordsAtPos(state.selection.from)
    const width = form.offsetWidth
    form.style.left = Math.max(8, Math.min(coords.left, innerWidth - width - 8)) + 'px'
    form.style.top = Math.min(coords.bottom + 8, innerHeight - form.offsetHeight - 8) + 'px'
  }
  text.focus()
  return true
}

export function pasteLinkOverSelection(view, text) {
  const { state } = view, { selection } = state
  if (!view.editable || !(selection instanceof TextSelection) || selection.empty) return false
  const href = text.trim()
  if (!href || /\s/.test(href)) return false
  try {
    const url = new URL(href)
    if (!['https:', 'http:', 'mailto:'].includes(url.protocol)) return false
    if (url.protocol === 'mailto:' && !url.pathname) return false
  } catch { return false }
  let code = false
  state.doc.nodesBetween(selection.from, selection.to, node => {
    if (node.type.spec.code || node.marks.some(mark => mark.type.spec.code)) code = true
  })
  const link = state.schema.marks.link
  if (code || !link) return false
  view.dispatch(state.tr.addMark(selection.from, selection.to, link.create({ href })))
  return true
}

export const pasteSelectedLink = $prose(() => new Plugin({
  props: {
    // DOM paste runs before Milkdown's Markdown clipboard handler, which otherwise
    // consumes the event and replaces the selection before mark-based handling.
    handleDOMEvents: {
      paste(view, event) {
        if (event.clipboardData?.files.length) return false
        if (!pasteLinkOverSelection(view, event.clipboardData?.getData('text/plain') || '')) return false
        event.preventDefault()
        return true
      },
    },
  },
}))
