// Suggestions at the cursor while typing, the way Obsidian and Notion do it. `[[` lists the other
// notes and narrows with every letter; `@` lists the dates (today, now, tomorrow …) and, with a
// letter or more after it, the people in Pocket Contacts whose names match (asked of the app,
// `noteEditorContacts`, after a pause in typing): taking one writes a link to their page. Return, Tab,
// a click or a tap takes the chosen one and replaces what was typed; Escape leaves the text as it
// is. The page has no list of notes of its own and no network, so it asks the app
// (`noteEditorNotes`) each time the list of notes opens.
import { $prose } from '@milkdown/kit/utils'
import { Plugin, TextSelection } from '@milkdown/kit/prose/state'
import { linkSchema } from '@milkdown/kit/preset/commonmark'
import { clearSlash, insertText, request } from './shared.js'
import { stamps } from './inserts.js'
import { enabled } from './homelab.js'

const SHOWN = 8

function askForNotes() {
  return request('notes').then((reply) => (reply && reply.notes) || []).catch(() => [])
}

// "Čišćenje" is found by "cisc".
const plain = (text) => text.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()

export function matching(notes, query) {
  const words = plain(query).split(/\s+/).filter(Boolean)
  if (!words.length) { return notes.slice(0, SHOWN) }
  const scored = []
  notes.forEach((note, index) => {
    const title = plain(note.title), where = plain(note.folder || '')
    if (!words.every((word) => title.includes(word) || where.includes(word))) { return }
    // A title that starts with what was typed first, then one with a word that does; the given order (newest first) within each.
    const rank = title.startsWith(words[0]) ? 0 : title.split(/\s+/).some((part) => part.startsWith(words[0])) ? 1 : title.includes(words[0]) ? 2 : 3
    scored.push({ note, rank, index })
  })
  return scored.sort((a, b) => a.rank - b.rank || a.index - b.index).slice(0, SHOWN).map((entry) => entry.note)
}

// The text a range held becomes the link, followed by a space that is not part of it.
function putLink(view, mark, from, to, title, url) {
  let tr = view.state.tr.replaceWith(from, to, view.state.schema.text(title || url, [mark.create({ href: url })]))
  const after = tr.mapping.map(to)
  tr = tr.insertText(' ', after).removeMark(after, after + 1, mark)
  tr.setSelection(TextSelection.create(tr.doc, after + 1)).setStoredMarks([])
  view.dispatch(tr.scrollIntoView()); view.focus()
}

// People whose names match, from the app; none when Pocket Contacts is not connected or does not answer.
function askForPeople(query) {
  const q = query.replace(/[.'’-]+$/, '').trim()
  if (!q || !enabled.has('contacts')) { return Promise.resolve([]) }
  return request('contacts', { q }).then((reply) => (reply && Array.isArray(reply.contacts) ? reply.contacts : [])).catch(() => [])
}

// A person's picture, or their initials on their colour when there is none (or it does not load).
function avatar(person) {
  const face = document.createElement('span'); face.className = 'note-suggest-avatar'
  face.style.background = /^#[0-9a-f]{3,8}$/i.test(person.color || '') ? person.color : ''
  face.textContent = person.initials || ''
  if (person.photo) {
    const img = document.createElement('img'); img.alt = ''; img.src = person.photo
    img.addEventListener('error', () => img.remove())
    face.append(img)
  }
  return face
}

const dates = [
  ['today', 'Today'], ['tomorrow', 'Tomorrow'], ['yesterday', 'Yesterday'], ['now', 'Now'], ['time', 'Time'], ['date', 'Date, short'],
]

// What opens a list: the text before the cursor it answers to, what it offers, and what taking one does.
const kinds = (ctx) => [
  {
    // `[[name`
    opened: /\[\[([^\[\]\n]{0,80})$/, lead: 0, label: 'Notes', wait: 'Looking…', none: (all) => all.length ? 'No note by that name' : 'No other notes yet',
    load: askForNotes,
    offer: (all, query) => matching(all, query).map((note) => ({ title: note.title || 'Untitled', detail: note.folder, note })),
    take: (view, range, item) => putLink(view, linkSchema.type(ctx), range.from, range.to, item.title, item.note.url),
  },
  {
    // `@tod` or `@Marko Pe`, at the start of a line or after a space: an address like name@host
    // is left alone, and at most two words are taken. Nothing matching closes the list, so text
    // typed after an @ is never in its way.
    opened: /(^|\s)@((?:[\p{L}\p{M}][\p{L}\p{M}\p{N}'’.-]{0,30}(?: [\p{L}\p{M}\p{N}'’.-]{0,30})?)?)$/u, lead: 1, label: 'Dates and people', none: () => null,
    load: () => Promise.resolve(dates),
    search: askForPeople,
    offer: (all, query, people) => all.filter(([key, title]) => query && !query.includes(' ') ? key.startsWith(query.toLowerCase()) || plain(title).startsWith(plain(query)) : !query)
      .map(([key, title]) => ({ title, detail: stamps[key](), key }))
      .concat((query ? people : []).slice(0, SHOWN).map((person) => ({ title: person.name, detail: person.detail, person }))),
    take: (view, range, item) => {
      if (item.person) { putLink(view, linkSchema.type(ctx), range.from, range.to, item.person.name, item.person.url); return }
      view.dispatch(view.state.tr.insertText(stamps[item.key]() + ' ', range.from, range.to).scrollIntoView()); view.focus()
    },
  },
]

export const suggestions = $prose((ctx) => {
  const all = kinds(ctx)
  let box = null, kind = null, loaded = [], shown = [], chosen = 0, range = null, query = '', dismissedAt = null, asked = false, editorView = null
  // What the kind's own search found (people), for which words, and the pause before asking.
  let people = [], peopleFor = '', searchTimer = null

  const close = () => {
    if (box) { box.remove(); box = null }
    range = null; kind = null; asked = false; loaded = []; people = []; peopleFor = ''; clearTimeout(searchTimer)
  }

  // Ask again after a pause in typing; an answer to an older question is dropped. Meanwhile the
  // people found before stay, those that still match what is typed.
  function search(view) {
    if (!kind || !kind.search) { return }
    const want = query.replace(/[.'’-]+$/, '').trim()
    if (want === peopleFor) { return }
    clearTimeout(searchTimer)
    const words = plain(want).split(/\s+/).filter(Boolean)
    people = want ? people.filter((p) => words.every((word) => plain(p.name + ' ' + (p.detail || '')).includes(word))) : []
    if (!want) { peopleFor = ''; return }
    const from = kind
    searchTimer = setTimeout(() => {
      from.search(want).then((list) => {
        if (kind !== from || !range || query.replace(/[.'’-]+$/, '').trim() !== want) { return }
        people = list; peopleFor = want
        try { draw(view) } catch (e) { close() }
      })
    }, 180)
  }
  const pick = (view, item) => { const at = range, from = kind; close(); if (at && item) { from.take(view, at, item) } }

  function draw(view) {
    shown = kind.offer(loaded, query, people); chosen = Math.min(chosen, Math.max(0, shown.length - 1))
    const message = shown.length ? null : !asked ? kind.wait : kind.none(loaded)
    if (!shown.length && !message) { if (box) { box.remove(); box = null } return }
    if (!box) {
      box = document.createElement('div'); box.className = 'note-suggest'; box.setAttribute('role', 'listbox')
      // The editor keeps the keyboard: a press on the list must not take the focus away.
      box.addEventListener('mousedown', (event) => event.preventDefault())
      document.body.append(box)
    }
    box.setAttribute('aria-label', kind.label); box.textContent = ''
    if (message) { const empty = document.createElement('div'); empty.className = 'note-suggest-empty'; empty.textContent = message; box.append(empty) }
    shown.forEach((item, index) => {
      const row = document.createElement('div'); row.className = 'note-suggest-row' + (index === chosen ? ' chosen' : '') + (item.person ? ' person' : '')
      row.setAttribute('role', 'option'); row.setAttribute('aria-selected', String(index === chosen))
      const title = document.createElement('strong'); title.textContent = item.title
      const more = item.detail ? document.createElement('small') : null
      if (more) { more.textContent = item.detail }
      if (item.person) {
        const words = document.createElement('span'); words.className = 'note-suggest-words'
        words.append(title); if (more) { words.append(more) }
        row.append(avatar(item.person), words)
      } else { row.append(title); if (more) { row.append(more) } }
      row.addEventListener('click', (event) => { event.preventDefault(); pick(view, item) })
      box.append(row)
    })
    // Under what was typed, or above it when the window (or the iPhone's keyboard) leaves no room below.
    const at = view.coordsAtPos(range.from), port = window.visualViewport
    const height = port ? port.height : innerHeight, width = Math.min(340, innerWidth - 16)
    box.style.width = width + 'px'
    box.style.left = Math.max(8, Math.min(at.left, innerWidth - width - 8)) + 'px'
    const tall = box.offsetHeight
    box.style.top = (at.bottom + 6 + tall > height && at.top - 6 - tall > 0 ? at.top - 6 - tall : at.bottom + 6) + 'px'
    const row = box.querySelector('.chosen'); if (row && row.scrollIntoView) { row.scrollIntoView({ block: 'nearest' }) }
  }

  // Return, Tab, the arrows and Escape belong to the list while it is open. The editor's own keys
  // (Return splits the line) are asked first by ProseMirror, so these are taken before it sees them.
  const keys = (event) => {
    const view = editorView
    if (!box || !range || !view || !view.dom.contains(event.target) || event.isComposing) { return }
    let taken = true
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      if (shown.length) { chosen = (chosen + (event.key === 'ArrowDown' ? 1 : -1) + shown.length) % shown.length; draw(view) } else { taken = false }
    } else if (event.key === 'Enter' || event.key === 'Tab') {
      if (shown.length) { pick(view, shown[chosen]) } else { taken = false }
    } else if (event.key === 'Escape') { dismissedAt = range.from; close() } else { taken = false }
    if (taken) { event.preventDefault(); event.stopPropagation() }
  }

  return new Plugin({
    view: (created) => {
      editorView = created; document.addEventListener('keydown', keys, true)
      return {
        update(view) {
          const { selection } = view.state, $from = selection.$from
          const inText = selection.empty && view.editable && $from.parent.isTextblock && !$from.parent.type.spec.code
          const before = inText ? $from.parent.textBetween(0, $from.parentOffset, undefined, '￼') : ''
          let found = null, match = null
          for (const each of all) { match = before.match(each.opened); if (match) { found = each; break } }
          if (!found) { dismissedAt = null; if (range) { close() } return }
          const typed = match[match.length - 1], from = $from.pos - match[0].length + (found.lead ? match[1].length : 0)
          // Escape was pressed for this one: it stays text until another is typed.
          if (dismissedAt === from) { if (range) { close() } return }
          const opening = !range || kind !== found
          if (opening) { close() }
          kind = found; range = { from, to: $from.pos }
          if (query !== typed) { chosen = 0 }
          query = typed
          if (opening) { found.load().then((list) => { if (kind === found && range) { loaded = list; asked = true; draw(view) } }) }
          search(view)
          draw(view)
        },
        destroy() { close(); document.removeEventListener('keydown', keys, true); editorView = null },
      }
    },
    props: {
      // Typing went somewhere else (another field, another window): the list goes.
      handleDOMEvents: { blur: (view) => { setTimeout(() => { if (range && !view.hasFocus()) { close() } }, 200); return false } },
    },
  })
})

// `/` → Link to a note: the two brackets, and the list opens as if they had been typed.
export const noteLinkItem = (ctx) => { clearSlash(ctx); insertText(ctx, '[[') }
