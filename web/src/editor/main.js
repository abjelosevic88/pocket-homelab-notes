// The rich editor in Pocket Homelab's Notes: the note is edited as it reads, and stays
// Markdown on the server. Runs from inside the app, with no network access.
import { Crepe } from '@milkdown/crepe'
import { linkAttr } from '@milkdown/kit/preset/commonmark'
import { TextSelection, Selection } from '@milkdown/kit/prose/state'
import { insert } from '@milkdown/kit/utils'
import { remarkStringifyOptionsCtx, editorViewCtx, parserCtx } from '@milkdown/kit/core'
import { LanguageDescription, StreamLanguage, syntaxHighlighting } from '@codemirror/language'
import { EditorView } from '@codemirror/view'
import { classHighlighter } from '@lezer/highlight'
import '@milkdown/crepe/theme/common/prosemirror.css'
import '@milkdown/crepe/theme/common/reset.css'
import '@milkdown/crepe/theme/common/block-edit.css'
import '@milkdown/crepe/theme/common/code-mirror.css'
import '@milkdown/crepe/theme/common/cursor.css'
import '@milkdown/crepe/theme/common/link-tooltip.css'
import '@milkdown/crepe/theme/common/list-item.css'
import '@milkdown/crepe/theme/common/placeholder.css'
import '@milkdown/crepe/theme/common/toolbar.css'
import '@milkdown/crepe/theme/common/table.css'
import '@milkdown/crepe/theme/common/image-block.css'
import './theme.css'
import { remarkCallouts, callout, calloutInputRule } from './callouts.js'
import { fromNextcloud, toNextcloud } from './markdown.js'
import { post, svg, insertBlocks, notice } from './shared.js'
import { detailsPlugins } from './details.js'
import { mathPlugins, mathPreview, MATH, loadKatex } from './math.js'
import { insertPlugins, findFootnote } from './inserts.js'
import { suggestions } from './suggest.js'
import { emojiRule, loadEmoji, closeEmojiPicker } from './emoji.js'
import { insertTemplate } from './templates.js'
import { findHeading, jumpTo } from './toc.js'
import { withoutEmptyLineTags } from './emptylines.js'
import { buildMenu, runners, paletteItems, slashScrollsAsAList } from './slash.js'
slashScrollsAsAList()
import { mountPalette, runPaletteCommand, activeFormats } from './palette.js'
import { tableActions, tableSelectionTracker, setTableSelectionListener } from './table-actions.js'
import { codeMode } from './code-mode.js'
import { remarkImageDefaults, imageBlockKeepsAlt } from './images.js'
import { wikiLinks } from './wikilinks.js'
import { state as attachments, displayURL, upload } from './attachments.js'
import { contextMenu } from './context-menu.js'
import { pasteSelectedLink, closeLinkForm } from './links.js'
import { setTools, replaceLine, insertInline as putInline, lineAtCursor } from './homelab.js'
import { setRunner, installRunButtons } from './runbook.js'
import { shift, size } from '@floating-ui/dom'
import { turnIntoButton, closeTurnIntoMenu } from './turn-into.js'

// A handful of languages, each loaded from this same file: a note is not an IDE, and every
// language the full list would pull in is more to ship.
const legacy = (load, pick) => () => load().then((m) => StreamLanguage.define(m[pick]))
const languages = [
  LanguageDescription.of({ name: 'JavaScript', alias: ['js', 'jsx', 'mjs'], load: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ jsx: true })) }),
  LanguageDescription.of({ name: 'TypeScript', alias: ['ts', 'tsx'], load: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ typescript: true, jsx: true })) }),
  LanguageDescription.of({ name: 'JSON', alias: ['json5'], load: () => import('@codemirror/lang-json').then((m) => m.json()) }),
  LanguageDescription.of({ name: 'YAML', alias: ['yml'], load: () => import('@codemirror/lang-yaml').then((m) => m.yaml()) }),
  LanguageDescription.of({ name: 'Python', alias: ['py'], load: () => import('@codemirror/lang-python').then((m) => m.python()) }),
  LanguageDescription.of({ name: 'SQL', alias: ['postgres', 'postgresql', 'mysql', 'sqlite'], load: () => import('@codemirror/lang-sql').then((m) => m.sql()) }),
  LanguageDescription.of({ name: 'HTML', alias: ['htm'], load: () => import('@codemirror/lang-html').then((m) => m.html()) }),
  LanguageDescription.of({ name: 'CSS', load: () => import('@codemirror/lang-css').then((m) => m.css()) }),
  LanguageDescription.of({ name: 'XML', alias: ['plist', 'svg'], load: () => import('@codemirror/lang-xml').then((m) => m.xml()) }),
  LanguageDescription.of({ name: 'Markdown', alias: ['md'], load: () => import('@codemirror/lang-markdown').then((m) => m.markdown()) }),
  LanguageDescription.of({ name: 'Go', alias: ['golang'], load: () => import('@codemirror/lang-go').then((m) => m.go()) }),
  LanguageDescription.of({ name: 'Rust', alias: ['rs'], load: () => import('@codemirror/lang-rust').then((m) => m.rust()) }),
  LanguageDescription.of({ name: 'Java', load: () => import('@codemirror/lang-java').then((m) => m.java()) }),
  LanguageDescription.of({ name: 'C++', alias: ['c', 'cpp', 'h', 'hpp', 'objc'], load: () => import('@codemirror/lang-cpp').then((m) => m.cpp()) }),
  LanguageDescription.of({ name: 'PHP', load: () => import('@codemirror/lang-php').then((m) => m.php()) }),
  LanguageDescription.of({ name: 'Swift', load: legacy(() => import('@codemirror/legacy-modes/mode/swift'), 'swift') }),
  LanguageDescription.of({ name: 'Shell', alias: ['sh', 'bash', 'zsh', 'console'], load: legacy(() => import('@codemirror/legacy-modes/mode/shell'), 'shell') }),
  LanguageDescription.of({ name: 'Dockerfile', alias: ['docker'], load: legacy(() => import('@codemirror/legacy-modes/mode/dockerfile'), 'dockerFile') }),
  LanguageDescription.of({ name: 'TOML', alias: ['ini', 'conf'], load: legacy(() => import('@codemirror/legacy-modes/mode/toml'), 'toml') }),
  LanguageDescription.of({ name: 'Diff', alias: ['patch'], load: legacy(() => import('@codemirror/legacy-modes/mode/diff'), 'diff') }),
  LanguageDescription.of({ name: 'Nginx', load: legacy(() => import('@codemirror/legacy-modes/mode/nginx'), 'nginx') }),
  LanguageDescription.of({ name: 'Math', alias: [MATH, 'latex', 'tex', 'katex'], load: legacy(() => import('@codemirror/legacy-modes/mode/stex'), 'stexMath') }),
  LanguageDescription.of({ name: 'Mermaid', alias: ['mermaid'], load: () => Promise.resolve(StreamLanguage.define({ token: (stream) => { stream.skipToEnd(); return null } })) }),
]

// Code is coloured through CSS classes (`tok-keyword` …), so it follows light and dark like the rest.
// Long lines wrap: a sideways scroll bar (always shown on a Mac set to show them) took height from
// the block, which then scrolled up and cut the line in half.
const codeTheme = [
  syntaxHighlighting(classHighlighter),
  EditorView.lineWrapping,
  EditorView.theme({
    '&': { backgroundColor: 'transparent', color: 'inherit', fontSize: '13.5px' },
    '.cm-content': { fontFamily: 'var(--crepe-font-code)', padding: '4px 0' },
    '.cm-scroller': { overflow: 'visible' },
    '.cm-gutters': { backgroundColor: 'transparent', border: 'none', color: 'var(--crepe-color-outline)' },
    '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'transparent' },
    '&.cm-focused': { outline: 'none' },
    '.cm-cursor': { borderLeftColor: 'var(--crepe-color-on-background)' },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'var(--crepe-color-selected) !important' },
  }),
]

// Mermaid is a part of the bundle of its own, read only for a note with a diagram.
let mermaidLoading = null
function loadMermaid() {
  if (window.mermaid) { return Promise.resolve() }
  if (!mermaidLoading) { mermaidLoading = import('mermaid').then((m) => { window.mermaid = m.default || m }) }
  return mermaidLoading
}

let diagramCount = 0
function renderPreview(language, content, apply) {
  const kind = (language || '').toLowerCase()
  if (kind === MATH) { return content.trim() ? mathPreview(content, apply) : null }
  if (kind !== 'mermaid') { return null }
  if (!content.trim()) { return null }
  loadMermaid().then(() => {
    const dark = document.documentElement.dataset.theme === 'dark' || (!document.documentElement.dataset.theme && window.matchMedia('(prefers-color-scheme: dark)').matches)
    window.mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: dark ? 'dark' : 'default', fontFamily: '-apple-system, system-ui, sans-serif' })
    const id = 'editor-diagram-' + (++diagramCount)
    return window.mermaid.render(id, content).then((out) => {
      const box = document.createElement('div'); box.className = 'diagram'; box.innerHTML = out.svg
      // Full size, in the app's own viewer: zoom and pan there, the note stays as it is.
      const open = document.createElement('button'); open.type = 'button'; open.className = 'diagram-open'
      open.title = 'Open the diagram full size'; open.setAttribute('aria-label', 'Open the diagram full size')
      open.innerHTML = svg('<path d="M4 9V4h5"/><path d="M20 15v5h-5"/><path d="M4 4l6 6"/><path d="M20 20l-6-6"/>')
      // The click is handled at the document (below): the code block re-inserts this markup, which drops listeners.
      box.append(open); apply(box)
    }).catch((error) => {
      const stray = document.getElementById('d' + id); if (stray) { stray.remove() }
      const box = document.createElement('div'); box.className = 'diagram failed'
      box.textContent = 'This diagram cannot be drawn yet: ' + String(error && error.message ? error.message : error).split('\n')[0]
      apply(box)
    })
  }).catch((error) => { const box = document.createElement('div'); box.className = 'diagram failed'; box.textContent = String(error.message || error); apply(box) })
  return undefined   // drawn when it is ready
}

let removePalette = null, togglePalette = null
let crepe = null, generation = 0, lastSent = null, baseline = null, documentID = null

async function load(markdown, options) {
  options = options || {}
  const run = ++generation
  if (options.accent) { document.documentElement.style.setProperty('--accent', options.accent) }
  if (options.theme) { document.documentElement.dataset.theme = options.theme }
  attachments.share = options.share || null
  document.body.spellcheck = options.spellcheck !== false
  document.body.classList.toggle('compact', !!options.compact)
  document.body.classList.toggle('reader', !!options.reader)
  document.body.classList.toggle('desktop', !!options.desktop)
  document.body.classList.toggle('phone', !options.desktop && !options.compact)
  closeEmojiPicker(); closeTurnIntoMenu()
  removePalette?.(); removePalette = null; togglePalette = null; closeLinkForm()
  // A still picture of the note on screen stays on top until the next one is drawn, so
  // changing notes never flashes an empty page.
  const root = document.getElementById('editor')
  const curtain = root.querySelector(':scope > .editor-host') ? root.querySelector(':scope > .editor-host').cloneNode(true) : null
  if (crepe) { const old = crepe; crepe = null; try { await old.destroy() } catch (e) {} }
  if (run !== generation) { return false }
  root.innerHTML = ''
  if (curtain) {
    curtain.className = 'editor-curtain'; curtain.setAttribute('aria-hidden', 'true'); curtain.inert = true
    curtain.querySelectorAll('[contenteditable]').forEach((el) => el.removeAttribute('contenteditable'))
    root.append(curtain)
  }
  const host = document.createElement('div'); host.className = 'editor-host'; root.prepend(host)
  const lift = () => { if (curtain) { curtain.remove() } }
  lastSent = null; baseline = null; documentID = options.document || null
  attachments.noteID = options.noteID == null ? null : options.noteID
  setTools(options.tools)
  setRunner(options.run, () => (crepe ? crepe.editor.action((ctx) => ctx.get(editorViewCtx)) : null))
  const doc = documentID
  const editor = new Crepe({
    root: host,
    defaultValue: fromNextcloud(markdown || ''),
    features: {
      [Crepe.Feature.Latex]: false, [Crepe.Feature.TopBar]: false, [Crepe.Feature.AI]: false,
    },
    featureConfigs: {
      [Crepe.Feature.BlockEdit]: { buildMenu, slashMenu: { middleware: [
        shift({ padding: 10 }),
        size({ padding: 10, apply({ availableHeight, elements }) {
          elements.floating.style.maxHeight = Math.max(80, Math.min(400, availableHeight)) + 'px'
        } }),
      ] } },
      // Pictures live with the note on the server: the app uploads them and serves them to the page.
      [Crepe.Feature.ImageBlock]: {
        onUpload: upload, inlineOnUpload: upload, blockOnUpload: upload, proxyDomURL: displayURL,
        blockUploadPlaceholderText: 'or paste a link', inlineUploadPlaceholderText: 'or paste a link',
      },
      [Crepe.Feature.Toolbar]: { buildToolbar: turnIntoButton },
      [Crepe.Feature.Placeholder]: { text: options.placeholder || 'Write, or type / for blocks', mode: 'doc' },
      [Crepe.Feature.CodeMirror]: { languages, theme: codeTheme, renderPreview, previewOnlyByDefault: true, previewLabel: 'Preview', searchPlaceholder: 'Language' },
    },
  })
  // Markdown spelled the common way, so an edit changes as little of the file as it can.
  // The link mark blanks any address whose scheme it does not know (only http, https, mailto,
  // tel and ftp reach the `href`), so the app's own links — a podcast note's clock — would be
  // dead. The real address rides along in `data-href`, which the click handler reads.
  editor.editor.config((ctx) => ctx.set(linkAttr.key, (mark) => ({ 'data-href': mark.attrs.href || '' })))
  editor.editor.config((ctx) => ctx.update(remarkStringifyOptionsCtx, (prev) => Object.assign({}, prev, { bullet: '-', bulletOther: '*', rule: '-', emphasis: '*', strong: '*', fences: true, listItemIndent: 'one' })))
  editor.editor.use(remarkImageDefaults).use(imageBlockKeepsAlt).use(remarkCallouts).use(callout).use(calloutInputRule).use(contextMenu).use(pasteSelectedLink).use(tableSelectionTracker).use(codeMode)
  editor.editor.use(detailsPlugins).use(mathPlugins).use(insertPlugins).use(emojiRule).use(suggestions).use(wikiLinks)
  await withoutEmptyLineTags(editor.editor)
  editor.setReadonly(!!options.readonly)
  try {
    await editor.create()
  } catch (error) {
    // A note the editor cannot read stays untouched: the app falls back to plain Markdown.
    lift()
    post('failed', { message: String(error && error.message ? error.message : error) })
    return false
  }
  if (run !== generation) { try { await editor.destroy() } catch (e) {} return false }
  crepe = editor
  lift()
  // What the editor makes of the note before anything is typed. Only a difference from it
  // goes back to the app: opening a note never rewrites it.
  baseline = toNextcloud(editor.getMarkdown())
  if (options.debug) { post('log', { message: 'baseline: ' + JSON.stringify(baseline) }) }
  editor.on((listener) => listener.markdownUpdated((ctx, next) => {
    if (crepe !== editor) { return }
    const text = toNextcloud(next)
    if (text === lastSent || (lastSent === null && text === baseline)) { return }
    lastSent = text
    post('changed', { markdown: text, document: doc })
  }))
  // Reading a note you may edit: the text stays put, but a task's box can still be ticked.
  if (options.readonly && options.checkable) editor.editor.action(ctx => allowTaskToggle(ctx.get(editorViewCtx)))
  if (!options.readonly) {
    const palette = mountPalette({
      items: paletteItems,
      table: () => editor.editor.action(ctx => tableActions(ctx)),
      active: () => editor.editor.action(ctx => activeFormats(ctx)),
      run: key => editor.editor.action(ctx => runPaletteCommand(ctx, key, runners)),
      capture: () => editor.editor.action(ctx => ctx.get(editorViewCtx).state.selection.getBookmark()),
      restore: bookmark => editor.editor.action(ctx => {
        const view = ctx.get(editorViewCtx)
        if (bookmark) { try { view.dispatch(view.state.tr.setSelection(bookmark.resolve(view.state.doc))) } catch {} }
      }),
    })
    setTableSelectionListener(palette.refresh)
    removePalette = () => { setTableSelectionListener(null); palette.remove() }
    togglePalette = palette.toggle
  }
  post('loaded')
  // `:tada:` needs the list of names; it is small, and read once the note is on screen.
  setTimeout(() => loadEmoji().catch(() => {}), 1200)
  if (options.focus) { focus(true) }
  return true
}

// The Mac's insert-menu shortcut: opens (or closes) the palette at the cursor, which stays put.
function toggleInsertMenu() {
  if (!togglePalette) return false
  document.querySelector('#editor .ProseMirror')?.focus()
  togglePalette()
  return true
}

// `atEnd`: a note opened to be written in (a new one, today's) has the cursor after its last word.
function focus(atEnd) {
  if (atEnd && crepe) {
    crepe.editor.action((ctx) => {
      const view = ctx.get(editorViewCtx)
      view.dispatch(view.state.tr.setSelection(Selection.atEnd(view.state.doc)).scrollIntoView())
    })
  }
  const view = document.querySelector('#editor .ProseMirror')
  if (view) { view.focus() }
}

// A link is followed with ⌘-click on the Mac (a plain click puts the cursor in it) and with a
// tap on a touch screen, where there is no other way. It opens in the browser or, for another
// note or a file of this one, in the app. Links within the note (its table of contents, a
// footnote's number) always jump on a plain click.
// The diagram's corner button. The code block's node view takes the press before the page
// sees a click (on a touch screen no click comes at all), so the button acts on the pointer
// going up, ahead of the editor, and the press never reaches the editor. A synthetic
// `.click()` (scripted screenshots) still works through the click handler below.
let diagramOpenedAt = 0
function openDiagram(button) {
  const now = Date.now()
  if (now - diagramOpenedAt < 500) { return }
  diagramOpenedAt = now
  const drawing = button.closest('.diagram')?.querySelector('svg')
  if (drawing) { post('diagram', { svg: drawing.outerHTML }) }
}
const diagramButton = (event) => (event.target && event.target.closest ? event.target.closest('.diagram-open') : null)
for (const type of ['pointerdown', 'mousedown', 'touchstart']) {
  document.addEventListener(type, (event) => { if (diagramButton(event)) { event.preventDefault(); event.stopPropagation() } }, true)
}
document.addEventListener('pointerup', (event) => {
  const button = diagramButton(event)
  if (button) { event.preventDefault(); event.stopPropagation(); openDiagram(button) }
}, true)

document.addEventListener('click', (event) => {
  const target = event.target && event.target.closest ? event.target : null
  if (!target) { return }
  const openButton = target.closest('.diagram-open')
  if (openButton) { event.preventDefault(); event.stopPropagation(); openDiagram(openButton); return }
  const view = crepe ? crepe.editor.action((ctx) => ctx.get(editorViewCtx)) : null
  const mark = target.closest('sup[data-type="footnote_reference"]')
  if (mark && view) {
    const pos = findFootnote(view.state.doc, mark.dataset.label)
    if (pos != null) { event.preventDefault(); jumpTo(view, pos + 1) }
    return
  }
  const link = target.closest('a[href]')
  if (!link) { return }
  const raw = link.dataset.href || link.getAttribute('href') || ''
  if (raw.startsWith('#')) {
    event.preventDefault()
    const heading = view ? findHeading(view.state.doc, raw) : null
    if (heading) { jumpTo(view, heading.pos) }
    return
  }
  // WebKit does not say what kind of pointer made a click; a screen that cannot hover is a touch screen.
  const touch = event.pointerType === 'touch' || window.matchMedia('(hover: none)').matches
  // A podcast note's clock is a play button, not text to edit: a plain click is enough.
  const plays = raw.startsWith('pockethomelab://')
  if (!(plays || event.metaKey || touch || !link.closest('.ProseMirror') || (view && !view.editable))) { return }
  // `link.href` resolves against the page; the app's own scheme is sent as written.
  event.preventDefault(); post('link', { url: plays ? raw : link.href, raw })
}, true)

// What was typed in the last moments, before the editor's own notice went out. Asked for
// right before a save or a change of note.
function flush(doc) {
  if (!crepe || doc !== documentID) { return null }
  const text = toNextcloud(crepe.getMarkdown())
  if (text === lastSent || (lastSent === null && text === baseline)) { return null }
  lastSent = text
  return text
}

// For the app's scripted checks: an edit made the way typing makes one.
const debugInsert = (markdown) => {
  try { if (!crepe) { post('log', { message: 'debugInsert: no editor' }); return }
    crepe.editor.action(insert(markdown)); post('log', { message: 'debugInsert: done, now ' + crepe.getMarkdown().length + ' characters' })
  } catch (error) { post('log', { message: 'debugInsert failed: ' + error }) }
}

// For the app's scripted checks: a `/` menu entry by its key, run on a fresh line at the end of the note.
const debugRun = (key) => act((ctx) => {
  const view = ctx.get(editorViewCtx), end = view.state.doc.content.size
  const tr = view.state.tr.insert(end, view.state.schema.nodes.paragraph.create())
  view.dispatch(tr.setSelection(TextSelection.create(tr.doc, end + 1)))
  if (runners[key]) { runners[key](ctx); post('log', { message: 'debugRun: ' + key }) } else { post('log', { message: 'debugRun: no entry ' + key }) }
})

// For the app's scripted checks: text typed at the end of the note, with the cursor left behind it
// as a keyboard leaves it, and then a key (`Enter`) pressed if one is named.
const debugKeys = (text, key) => act((ctx) => {
  const view = ctx.get(editorViewCtx), end = view.state.doc.content.size
  let tr = view.state.tr.insert(end, view.state.schema.nodes.paragraph.create())
  tr = tr.setSelection(TextSelection.create(tr.doc, end + 1)).insertText(text)
  view.dispatch(tr)
  if (key) { setTimeout(() => view.dom.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })), 1500) }
  post('log', { message: 'debugKeys: ' + text + (key ? ' + ' + key : '') })
})

// For the app's scripted checks: the first place the words stand is selected, as a mouse would
// select them, and a moment later the selection toolbar's place is reported against the bar's.
const debugSelect = (text) => act((ctx) => {
  const view = ctx.get(editorViewCtx)
  let found = null
  view.state.doc.descendants((node, pos) => {
    if (found || !node.isText) { return !found }
    const at = node.text.indexOf(text)
    if (at >= 0) { found = pos + at }
  })
  if (found == null) { post('log', { message: 'debugSelect: not found ' + text }); return }
  view.focus()
  view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, found, found + text.length)).scrollIntoView())
  setTimeout(() => {
    const bar = document.querySelector('.note-command-bar'), toolbar = document.querySelector('.milkdown-toolbar')
    const b = bar ? bar.getBoundingClientRect() : null, t = toolbar ? toolbar.getBoundingClientRect() : null
    const sel = view.coordsAtPos(found)
    post('log', { message: 'debugSelect: bar bottom ' + (b ? Math.round(b.bottom) : 'none') + ', toolbar ' + (t ? Math.round(t.top) + '–' + Math.round(t.bottom) + (toolbar.dataset.show === 'false' ? ' hidden' : '') : 'none') + ', text ' + Math.round(sel.top) + '–' + Math.round(sel.bottom) })
  }, 800)
})

// A file the app stored with the note (picked from Nextcloud, or from this device through the
// app's own picker) goes in where the cursor is.
const insertMarkdown = (markdown, noteID) => {
  if (!crepe) { return false }
  if (noteID != null) { attachments.noteID = noteID }
  crepe.editor.action((ctx) => {
    const parsed = ctx.get(parserCtx)(markdown)
    // What was put in stays selected, so the next file follows it instead of overtaking it.
    if (parsed) { insertBlocks(ctx, parsed.content, 'node') }
  })
  return true
}

// What the app's own picker chose: one of the owner's templates.
const act = (work) => { if (!crepe) { return false } crepe.editor.action(work); return true }
const insertOwnTemplate = (markdown) => act((ctx) => insertTemplate(ctx, markdown))
// The homelab tools' answers: a link where the cursor is; a line replaced by itself with a link.
const insertInline = (markdown) => act((ctx) => putInline(ctx, markdown))
const replaceLineText = (from, text, markdown) => { let ok = false; act((ctx) => { ok = replaceLine(ctx, from, text, markdown) }); return ok }
// The line the toolbar's "Make this line a task" means: the one the cursor is in.
const currentLine = () => { let line = null; act((ctx) => { line = lineAtCursor(ctx.get(editorViewCtx).state) }); return line }

// Milkdown's task box ignores taps while the editor is read-only. Toggle the list item's
// `checked` directly; the change reaches the app like any other edit.
function allowTaskToggle(view) {
  view.dom.addEventListener('pointerdown', event => {
    const label = event.target.closest?.('.milkdown-list-item-block .label-wrapper')
    if (!label) return
    const block = label.closest('.milkdown-list-item-block')
    let pos
    try { pos = view.posAtDOM(block, 0) } catch { return }
    const $pos = view.state.doc.resolve(pos)
    for (let depth = $pos.depth; depth >= 0; depth--) {
      const node = depth ? $pos.node(depth) : view.state.doc.nodeAt(pos)
      if (node?.type.name !== 'list_item') continue
      if (node.attrs.checked == null) return
      event.preventDefault(); event.stopPropagation()
      const at = depth ? $pos.before(depth) : pos
      view.dispatch(view.state.tr.setNodeAttribute(at, 'checked', !node.attrs.checked))
      return
    }
  }, true)
}

// The phone's toolbar and insert menu show only while the keyboard is up; the app reports it.
// With a hardware keyboard there is no on-screen one: the toolbar then follows the editor's focus.
const keyboard = { soft: false, hardware: false }
const typingHere = () => { const el = document.activeElement; return !!el && (el.isContentEditable || el.tagName === 'INPUT') }
// An open insert menu stays until it is closed; it has its own Close.
const wanted = () => keyboard.soft || (keyboard.hardware && typingHere()) || document.body.classList.contains('picker-open')
// Focus moving from the note to the insert menu's field can hide and re-show the keyboard;
// hiding waits a moment so that hand-off neither flickers the toolbar nor closes the menu.
let hideTimer = null
function applyKeyboard() {
  const shown = wanted()
  clearTimeout(hideTimer)
  if (shown) { document.body.classList.remove('keyboard-hidden'); return }
  const hide = () => {
    if (wanted()) return
    const was = !document.body.classList.contains('keyboard-hidden')
    document.body.classList.add('keyboard-hidden')
  }
  // Nothing to hand off on first load: start hidden straight away.
  if (document.body.classList.contains('keyboard-hidden') || !document.querySelector('.note-command-bar')) hide()
  else hideTimer = setTimeout(hide, 350)
}
function setKeyboard(visible, hardware = false) { keyboard.soft = !!visible; keyboard.hardware = !!hardware; applyKeyboard() }
// Focus moving between the note and the insert menu's field passes through the body; settle first.
for (const type of ['focusin', 'focusout']) document.addEventListener(type, () => { if (keyboard.hardware) setTimeout(applyKeyboard, 0) })
document.addEventListener('note-picker-closed', () => applyKeyboard())
window.noteEditor = { load, setKeyboard, focus, toggleInsertMenu, flush, debugInsert, debugRun, debugKeys, debugSelect, insertMarkdown, insertOwnTemplate, insertInline, replaceLineText, currentLine, notice }
installRunButtons()
window.addEventListener('error', (event) => post('error', { message: String(event.message) }))
post('ready')
