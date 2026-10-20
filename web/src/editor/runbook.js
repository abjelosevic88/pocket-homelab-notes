// Commands in a note, run on the server. A shell code block gets a "Run on server" button
// beside Crepe's Copy when the app says it can (a Mac with an SSH destination set up): the app
// copies the command and opens a terminal on the server; what came back is put into the note
// with "Paste the output". The block's language and text come from the document, not the DOM,
// and the buttons are put back whenever the block redraws its toolbar.
import { editorViewCtx } from '@milkdown/kit/core'
import { post, svg } from './shared.js'

const SHELL = new Set(['bash', 'sh', 'shell', 'zsh', 'console', 'terminal'])
let canRun = false
let currentView = () => null
export function setRunner(enabled, view) { canRun = !!enabled; currentView = view || (() => null); schedule() }

// The code block node behind a code block's DOM, wherever the block sits (a list item too).
function blockOf(dom) {
  const view = currentView(); if (!view) { return null }
  let found = null
  view.state.doc.descendants((node, pos) => {
    if (found) { return false }
    if (node.type.name !== 'code_block') { return true }
    const at = view.nodeDOM(pos)
    if (at === dom || (at && at.contains && at.contains(dom))) { found = { node, pos, view } }
    return false
  })
  return found
}

const isShell = (node) => SHELL.has(String(node.attrs.language || '').trim().toLowerCase())
const icon = svg('<path d="M4 17l6-5-6-5"/><path d="M12 19h8"/>')

function refresh() {
  const root = document.getElementById('editor'); if (!root) { return }
  for (const block of root.querySelectorAll('.milkdown-code-block')) {
    const group = block.querySelector('.tools-button-group'); if (!group) { continue }
    const have = group.querySelector('.code-run')
    const found = canRun ? blockOf(block) : null
    const wanted = !!(found && found.view.editable && isShell(found.node) && found.node.textContent.trim())
    if (wanted && !have) {
      const button = document.createElement('button'); button.type = 'button'; button.className = 'code-run'
      button.title = 'Copy the command and open a terminal on the server'
      button.innerHTML = icon + '<span>Run on server</span>'
      group.append(button)
      post('log', { message: 'Run on server offered for a shell block' })
    } else if (!wanted && have) { have.remove() }
  }
}

let pending = 0
function schedule() { if (pending) { return } pending = requestAnimationFrame(() => { pending = 0; refresh() }) }

export function watchCodeBlocks() {
  const root = document.getElementById('editor'); if (!root) { return }
  new MutationObserver(schedule).observe(root, { childList: true, subtree: true, characterData: true })
}

function run(button) {
  const block = button.closest('.milkdown-code-block'); const found = block ? blockOf(block) : null
  if (!found) { return }
  post('code', { action: 'run', language: found.node.attrs.language || '', code: found.node.textContent })
}

// The click is taken before ProseMirror sees it, so the block is not selected instead.
const runButton = (event) => (event.target && event.target.closest ? event.target.closest('.code-run') : null)
export function installRunButtons() {
  for (const type of ['pointerdown', 'mousedown', 'touchstart']) {
    document.addEventListener(type, (event) => { if (runButton(event)) { event.preventDefault(); event.stopPropagation() } }, true)
  }
  document.addEventListener('click', (event) => {
    const button = runButton(event)
    if (button) { event.preventDefault(); event.stopPropagation(); run(button) }
  }, true)
  watchCodeBlocks()
}
