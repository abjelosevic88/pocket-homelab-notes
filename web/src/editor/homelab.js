// Tools no general editor has: the homelab around the note. A snapshot of how the server
// stands, a line sent to Nextcloud Tasks or Deck, a link to a file, document, issue or title
// found with the app's own search, a drawing from Pocket Draw shown as its picture, a web
// bookmark with the page's title, a scan or photo
// from the phone's camera, a voice memo. The editor page has no network and knows none of
// the services: every tool asks the app, which answers with what to put into the note.
//
// Which tools are offered depends on what is connected; the app names them when it loads the note.
import { editorViewCtx, parserCtx } from '@milkdown/kit/core'
import { insert } from '@milkdown/kit/utils'
import { post, svg, clearSlash, notice } from './shared.js'

export const enabled = new Set()
export function setTools(list) { enabled.clear(); for (const tool of list || []) { enabled.add(tool) } }

export const icons = {
  snapshot: svg('<rect x="3" y="5" width="18" height="12" rx="2"/><path d="M7 21h10M12 17v4"/><path d="M7 13l3-3 2 2 4-4"/>'),
  task: svg('<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12.5l2.5 2.5L16 9.5"/>'),
  link: svg('<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/><path d="M3 21l3-3"/>'),
  bookmark: svg('<path d="M6 4h12v17l-6-4-6 4z"/>'),
  drawing: svg('<path d="M3 17c3-1 4-5 7-5s2 4 5 4"/><path d="M14.5 4.5a2.12 2.12 0 0 1 3 3L10 15l-3.5 1 1-3.5z"/><path d="M3 21h18"/>'),
  scan: svg('<path d="M4 8V6a2 2 0 0 1 2-2h2M16 4h2a2 2 0 0 1 2 2v2M20 16v2a2 2 0 0 1-2 2h-2M8 20H6a2 2 0 0 1-2-2v-2"/><path d="M7 12h10"/>'),
  camera: svg('<path d="M4 8h3l2-3h6l2 3h3v11H4z"/><circle cx="12" cy="13" r="3.5"/>'),
  voice: svg('<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3M9 21h6"/>'),
  attention: svg('<path d="M12 4l9 15.5H3z"/><path d="M12 10v4M12 17h.01"/>'),
  media: svg('<rect x="3" y="6" width="14" height="11" rx="2"/><path d="M17 9h2a2 2 0 0 1 2 2v9H9"/><path d="M9 9l4 2.5L9 14z"/>'),
  spent: svg('<rect x="3" y="6" width="18" height="12" rx="2"/><path d="M3 10h18M7 15h4"/>'),
  fuel: svg('<path d="M5 4h8v16H5zM13 8h3l3 3v6a2 2 0 0 1-4 0v-4h-2"/><path d="M7 8h4"/>'),
  hosts: svg('<circle cx="12" cy="5" r="2"/><circle cx="5" cy="19" r="2"/><circle cx="19" cy="19" r="2"/><path d="M12 7v5M12 12l-6 5M12 12l6 5"/>'),
  processes: svg('<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>'),
  expense: svg('<circle cx="12" cy="12" r="9"/><path d="M15 8.5a3.5 3.5 0 0 0-6 2.5 3.5 3.5 0 0 0 6 2.5M7.5 11h5M7.5 13h5"/>'),
  habits: svg('<circle cx="12" cy="12" r="9"/><path d="M8 12.5l2.5 2.5L16 9.5"/>'),
}

// The line a tool works on: the block the cursor is in, or, when the `/` menu was typed on an
// empty line, the block above it. Code is not a line. Positions are of the block, so the app's
// answer can replace it; the text is kept to check that the block is still that one.
export function lineAtCursor(state) {
  const blocks = []
  state.doc.descendants((node, pos) => { if (node.isTextblock) { blocks.push({ node, pos }) } })
  const cursor = state.selection.from
  let index = blocks.findIndex(({ node, pos }) => cursor >= pos && cursor <= pos + node.nodeSize)
  if (index < 0) { index = blocks.length - 1 }
  for (; index >= 0; index--) {
    const { node, pos } = blocks[index]
    if (node.type.spec.code) { return null }
    const text = node.textContent.trim()
    if (text) { return { text, from: pos, to: pos + node.nodeSize } }
    // An empty line above another empty line is not "the line above": only the one just above the cursor counts.
    if (index !== blocks.findIndex(({ pos: p }) => p === blocks[index].pos) || cursor < pos) { return null }
  }
  return null
}

// The block found before, found again: at its position if it still reads the same, else by its text.
function findLine(doc, from, text) {
  const at = doc.nodeAt(from)
  if (at && at.isTextblock && at.textContent.trim() === text) { return { from, to: from + at.nodeSize } }
  let found = null
  doc.descendants((node, pos) => {
    if (found) { return false }
    if (node.isTextblock && node.textContent.trim() === text) { found = { from: pos, to: pos + node.nodeSize } }
  })
  return found
}

// The app's answer for a line sent away: the line's words replaced by the markdown (the words
// again, with a link behind them). Only the contents change, so a list item stays a list item.
export function replaceLine(ctx, from, text, markdown) {
  const view = ctx.get(editorViewCtx)
  const line = findLine(view.state.doc, from, text)
  if (!line) { return false }
  const parsed = ctx.get(parserCtx)(markdown)
  const paragraph = parsed && parsed.firstChild
  if (!paragraph) { return false }
  view.dispatch(view.state.tr.replaceWith(line.from + 1, line.to - 1, paragraph.content).scrollIntoView())
  return true
}

// Words with a link, put in where the cursor is, as typing would put them.
export function insertInline(ctx, markdown) {
  insert(markdown, true)(ctx)
  ctx.get(editorViewCtx).focus()
}

const ask = (tool, body) => post('tool', Object.assign({ tool }, body || {}))

const taskItem = (ctx) => {
  clearSlash(ctx)
  const line = lineAtCursor(ctx.get(editorViewCtx).state)
  if (!line) { notice('Write the line first, then ask again on the line below it.'); return }
  ask('task', line)
}
const simple = (tool) => (ctx) => { clearSlash(ctx); ask(tool) }
// A tool that works on the line above the `/`.
const lineItem = (tool) => (ctx) => {
  clearSlash(ctx)
  const line = lineAtCursor(ctx.get(editorViewCtx).state)
  if (!line) { notice('Write the line first, then ask again on the line below it.'); return }
  ask(tool, line)
}

// What each tool is called and does, in the `/` menu.
export const entries = [
  ['snapshot', { title: 'Homelab snapshot', label: 'Homelab snapshot status server now incident', description: 'How the server, storage, monitors and downloads stand right now, as a table', icon: icons.snapshot, onRun: simple('snapshot') }],
  ['task', { title: 'Make this line a task', label: 'Make this line a task todo deck card send', description: 'Sends the line above to Nextcloud Tasks or Deck and leaves a link behind', icon: icons.task, onRun: taskItem }],
  ['link', { title: 'Link to something', label: 'Link to something person people contact file document book episode issue pull request movie series pocket contacts documents forgejo jellyfin nextcloud', description: 'A person, file, document, issue or title, found with the app’s search', icon: icons.link, onRun: simple('link') }],
  ['draw', { title: 'Drawing', label: 'Drawing sketch diagram draw picture flowchart mermaid excalidraw drawio pocket draw', description: 'A sketch or diagram from Pocket Draw, shown in the note', icon: icons.drawing, onRun: simple('draw') }],
  ['bookmark', { title: 'Web bookmark', label: 'Web bookmark url address website page', description: 'An address becomes a link with the page’s title', icon: icons.bookmark, onRun: simple('bookmark') }],
  ['scan', { title: 'Scan a document', label: 'Scan a document camera pages pdf paper', description: 'The camera scans pages into a PDF stored with the note', icon: icons.scan, onRun: simple('scan') }],
  ['camera', { title: 'Take a photo', label: 'Take a photo camera picture', description: 'A picture taken now, stored with the note', icon: icons.camera, onRun: simple('camera') }],
  ['voice', { title: 'Voice memo', label: 'Voice memo record audio dictate transcribe', description: 'A recording stored with the note, with the words written out', icon: icons.voice, onRun: simple('voice') }],
  ['attention', { title: 'What needs attention', label: 'What needs attention downtime outages monitors disks backups broke this week', description: 'Monitors down and their outages, disks with warnings, backups that need a look', icon: icons.attention, onRun: simple('attention') }],
  ['media', { title: 'Watched and listened this week', label: 'Watched listened this week films series podcasts audiobooks journal', description: 'From Jellyfin, PinePods, Audiobookshelf and the listening library', icon: icons.media, onRun: simple('media') }],
  ['spent', { title: 'Spending this month', label: 'Spending this month spent money firefly categories transactions', description: 'By category, and the largest transactions, from Firefly', icon: icons.spent, onRun: simple('spent') }],
  ['fuel', { title: 'Vehicle log', label: 'Vehicle log fuel fill-ups car service repairs lubelogger', description: 'What each car cost this month and this year, and its last fill-ups and services', icon: icons.fuel, onRun: simple('fuel') }],
  ['hosts', { title: 'Hosts on the network', label: 'Hosts on the network ip addresses clients adguard devices', description: 'AdGuard’s busiest clients, with their names', icon: icons.hosts, onRun: simple('hosts') }],
  ['processes', { title: 'Server processes', label: 'Server processes top cpu memory glances', description: 'What the server is busy with, by CPU and by memory', icon: icons.processes, onRun: simple('processes') }],
  ['habits', { title: 'Days of habits', label: 'Days of habits habit tracker journal streak sleep mood pocket habits', description: 'Habits done and missed, the trackers and the journal, for the days you pick, from Pocket Habits', icon: icons.habits, onRun: simple('habits') }],
  ['expense', { title: 'Make this line a transaction', label: 'Make this line a transaction expense firefly money spent', description: 'The line above, as template + amount + what for (e.g. groceries 24.50 bread), goes to the Firefly assistant; a link comes back', icon: icons.expense, onRun: lineItem('expense') }],
]

export function addHomelabGroup(builder) {
  const offered = entries.filter(([key]) => enabled.has(key))
  if (!offered.length) { return }
  const group = builder.addGroup('homelab', 'Homelab')
  for (const [key, item] of offered) { group.addItem('homelab-' + key, item) }
}
