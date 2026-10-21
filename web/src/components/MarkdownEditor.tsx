import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react'
import { EditorState, EditorSelection, Compartment, type Extension } from '@codemirror/state'
import { EditorView, keymap, drawSelection, highlightActiveLine, placeholder as cmPlaceholder, dropCursor } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { syntaxHighlighting, syntaxTree, HighlightStyle, indentOnInput, bracketMatching } from '@codemirror/language'
import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
import { autocompletion, closeBrackets, type Completion, type CompletionContext, type CompletionResult } from '@codemirror/autocomplete'
import type { Person } from '../lib/types'
import { tags as t } from '@lezer/highlight'

// The note as Markdown, every character of it: the plain editor for people who want to see and
// type the syntax, for anything the rich editor does not know, and for .txt notes.

export interface MarkdownEditorHandle {
  insert(text: string, block?: boolean): void
  wrap(before: string, after?: string, placeholder?: string): void
  linePrefix(prefix: string): void
  currentLine(): { text: string; from: number; to: number } | null
  replaceLine(from: number, text: string, md: string): boolean
  focus(): void
  scrollToLine(line: number): void
}

interface Props {
  docKey: string
  revision: number
  content: string
  readonly?: boolean
  spellcheck?: boolean
  onChange: (md: string) => void
  onPasteFiles?: (files: File[]) => Promise<string[]>
  onOpenLink?: (url: string) => void
  /** People from Pocket Contacts for `@` and a name ([] when it is off). */
  onPeople?: (q: string) => Promise<Person[]>
}

// `@` and a name at the start of a line or after a space (an address like name@host is left
// alone): at most two words, so a sentence typed after an @ soon stops asking.
const mentionRe = /(?:^|\s)@([\p{L}\p{M}][\p{L}\p{M}\p{N}'’.-]{0,30}(?: [\p{L}\p{M}\p{N}'’.-]{0,30})?)$/u

/** A person's picture, or their initials on their colour, in front of the name in the list. */
function personIcon(c: Completion): Node | null {
  const p = (c as Completion & { person?: Person }).person
  if (!p) return null
  const el = document.createElement('span')
  el.className = 'cm-person-avatar'
  el.style.background = p.color || ''
  el.textContent = p.initials || ''
  if (p.photo) {
    const img = document.createElement('img')
    img.src = p.photo; img.alt = ''
    img.onerror = () => img.remove()
    el.append(img)
  }
  return el
}

const style = HighlightStyle.define([
  { tag: t.heading1, fontSize: '1.45em', fontWeight: '700' },
  { tag: t.heading2, fontSize: '1.25em', fontWeight: '700' },
  { tag: t.heading3, fontSize: '1.1em', fontWeight: '700' },
  { tag: [t.heading4, t.heading5, t.heading6], fontWeight: '700' },
  { tag: t.strong, fontWeight: '700' },
  { tag: t.emphasis, fontStyle: 'italic' },
  { tag: t.strikethrough, textDecoration: 'line-through' },
  { tag: t.link, color: 'var(--accent-hover)' },
  { tag: t.url, color: 'var(--muted)' },
  { tag: t.monospace, fontFamily: 'var(--mono)', color: 'var(--code)' },
  { tag: t.quote, color: 'var(--muted)', fontStyle: 'italic' },
  { tag: [t.processingInstruction, t.meta, t.punctuation, t.contentSeparator], color: 'var(--muted)' },
])

const theme = EditorView.theme({
  '&': { height: '100%', fontSize: '15px', backgroundColor: 'transparent', color: 'var(--text)' },
  '.cm-scroller': { fontFamily: 'var(--mono-prose)', lineHeight: '1.65', padding: '18px 0 80px' },
  '.cm-content': { maxWidth: '820px', margin: '0 auto', padding: '0 28px', caretColor: 'var(--accent)' },
  '.cm-activeLine': { backgroundColor: 'var(--surface-2)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection': { backgroundColor: 'var(--selection) !important' },
  '.cm-cursor': { borderLeftColor: 'var(--accent)' },
  '.cm-placeholder': { color: 'var(--muted)' },
  '.cm-tooltip.cm-tooltip-autocomplete': { border: '1px solid var(--border)', borderRadius: '10px', background: 'var(--surface)', boxShadow: '0 12px 36px #0003', overflow: 'hidden' },
  '.cm-tooltip.cm-tooltip-autocomplete > ul': { fontFamily: 'var(--font)', maxHeight: '300px', padding: '4px' },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li': { display: 'flex', alignItems: 'center', gap: '8px', padding: '5px 8px', borderRadius: '6px' },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]': { background: 'var(--accent)', color: '#fff' },
  '.cm-completionDetail': { marginLeft: 'auto', paddingLeft: '10px', fontStyle: 'normal', opacity: '.75', fontSize: '12px' },
  '.cm-person-avatar': { position: 'relative', width: '22px', height: '22px', borderRadius: '50%', display: 'inline-grid', placeItems: 'center', fontSize: '10px', fontWeight: '700', color: '#fff', flexShrink: '0', overflow: 'hidden', background: 'var(--accent)' },
  '.cm-person-avatar img': { position: 'absolute', inset: '0', width: '100%', height: '100%', objectFit: 'cover' },
})

const taskRe = /^(\s*(?:[-*+]|\d+[.)])\s+\[)([ xX])(\])/

export const MarkdownEditor = forwardRef<MarkdownEditorHandle, Props>(function MarkdownEditor(props, ref) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const latest = useRef(props)
  latest.current = props
  const editable = useRef(new Compartment())
  const spell = useRef(new Compartment())
  const shown = useRef<{ doc: string; revision: number } | null>(null)
  const exts = useRef<Extension[]>([])

  // `@mar` lists the people whose names match; taking one writes a link to their page.
  async function people(cx: CompletionContext): Promise<CompletionResult | null> {
    const find = latest.current.onPeople
    if (!find) return null
    const line = cx.state.doc.lineAt(cx.pos)
    const m = mentionRe.exec(line.text.slice(Math.max(0, cx.pos - line.from - 80), cx.pos - line.from))
    if (!m) return null
    const node = syntaxTree(cx.state).resolveInner(cx.pos, -1)
    for (let n: typeof node | null = node; n; n = n.parent) if (/Code|URL|LinkMark/.test(n.name)) return null
    const q = m[1].replace(/[.'’-]+$/, '').trim()
    if (!q) return null
    // A pause in typing first: no question per letter.
    await new Promise(r => setTimeout(r, 150))
    if (cx.aborted) return null
    let found: Person[] = []
    try { found = await find(q) } catch { return null }
    if (cx.aborted || !found.length) return null
    const from = cx.pos - m[1].length - 1
    return {
      from, filter: false,
      options: found.map(p => ({
        label: p.name, detail: p.detail, type: 'person', person: p,
        apply: (view: EditorView, _c: Completion, at: number, to: number) => {
          const text = p.markdown + ' '
          view.dispatch({ changes: { from: at, to, insert: text }, selection: EditorSelection.cursor(at + text.length), scrollIntoView: true })
        },
      }) as Completion),
    }
  }

  useEffect(() => {
    exts.current = [
          autocompletion({ override: [people], icons: false, addToOptions: [{ render: personIcon, position: 20 }] }),
          history(), drawSelection(), dropCursor(), highlightActiveLine(), indentOnInput(), bracketMatching(), closeBrackets(), highlightSelectionMatches(),
          markdown({ base: markdownLanguage }), syntaxHighlighting(style), EditorView.lineWrapping, theme,
          cmPlaceholder('Write in Markdown…'),
          keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab,
            { key: 'Mod-b', run: () => { wrapSel('**'); return true } },
            { key: 'Mod-i', run: () => { wrapSel('*'); return true } },
            { key: 'Mod-k', run: () => { wrapSel('[', '](https://)'); return true } },
          ]),
          editable.current.of(EditorView.editable.of(!props.readonly)),
          spell.current.of(EditorView.contentAttributes.of({ spellcheck: props.spellcheck === false ? 'false' : 'true' })),
          EditorView.updateListener.of(u => { if (u.docChanged) latest.current.onChange(u.state.doc.toString()) }),
          EditorView.domEventHandlers({
            // A click on a task's box ticks it, as in the rich editor.
            mousedown: (e, v) => {
              const pos = v.posAtCoords({ x: e.clientX, y: e.clientY })
              if (pos == null) return false
              const line = v.state.doc.lineAt(pos)
              const m = taskRe.exec(line.text)
              if (!m) return false
              const box = line.from + m[1].length
              if (pos < box - 1 || pos > box + 1) return false
              e.preventDefault()
              v.dispatch({ changes: { from: box, to: box + 1, insert: m[2] === ' ' ? 'x' : ' ' } })
              return true
            },
            // ⌘/Ctrl-click opens a link.
            click: (e, v) => {
              if (!(e.metaKey || e.ctrlKey)) return false
              const pos = v.posAtCoords({ x: e.clientX, y: e.clientY })
              if (pos == null) return false
              const line = v.state.doc.lineAt(pos)
              const off = pos - line.from
              const re = /\[[^\]]*\]\(([^)\s]+)\)|(https?:\/\/[^\s)>]+)/g
              let m: RegExpExecArray | null
              while ((m = re.exec(line.text))) {
                if (off >= m.index && off <= m.index + m[0].length) { latest.current.onOpenLink?.(m[1] || m[2]); e.preventDefault(); return true }
              }
              return false
            },
            paste: (e) => handleFiles(e.clipboardData?.files, e),
            drop: (e) => handleFiles(e.dataTransfer?.files, e),
          }),
        ]
    const v = new EditorView({ parent: host.current!, state: EditorState.create({ doc: props.content, extensions: exts.current }) })
    view.current = v
    shown.current = { doc: props.docKey, revision: props.revision }
    return () => { v.destroy(); view.current = null }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function handleFiles(list: FileList | undefined | null, e: Event) {
    const files = Array.from(list || [])
    if (!files.length || !latest.current.onPasteFiles) return false
    e.preventDefault()
    latest.current.onPasteFiles(files).then(mds => insert(mds.join('\n\n'), true))
    return true
  }

  // Another note, or the same one reloaded from the server.
  useEffect(() => {
    const v = view.current
    if (!v) return
    const s = shown.current
    if (s && s.doc === props.docKey && s.revision === props.revision) return
    shown.current = { doc: props.docKey, revision: props.revision }
    // A fresh state: the undo history belongs to the note it was made in.
    v.setState(EditorState.create({ doc: props.content, extensions: exts.current }))
  }, [props.docKey, props.revision]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    view.current?.dispatch({ effects: [editable.current.reconfigure(EditorView.editable.of(!props.readonly)), spell.current.reconfigure(EditorView.contentAttributes.of({ spellcheck: props.spellcheck === false ? 'false' : 'true' }))] })
  }, [props.readonly, props.spellcheck])

  function insert(text: string, block = false) {
    const v = view.current
    if (!v || !text) return
    const sel = v.state.selection.main
    let ins = text
    if (block) {
      const line = v.state.doc.lineAt(sel.from)
      const before = line.text.trim() ? '\n\n' : ''
      ins = before + text + '\n'
    }
    v.dispatch({ changes: { from: sel.from, to: sel.to, insert: ins }, selection: EditorSelection.cursor(sel.from + ins.length), scrollIntoView: true })
    v.focus()
  }

  function wrapSel(before: string, after = before, ph = 'text') {
    const v = view.current
    if (!v) return
    const sel = v.state.selection.main
    const inner = v.state.sliceDoc(sel.from, sel.to) || ph
    v.dispatch({ changes: { from: sel.from, to: sel.to, insert: before + inner + after }, selection: EditorSelection.range(sel.from + before.length, sel.from + before.length + inner.length) })
    v.focus()
  }

  useImperativeHandle(ref, () => ({
    insert,
    wrap: wrapSel,
    linePrefix: (prefix: string) => {
      const v = view.current
      if (!v) return
      const sel = v.state.selection.main
      const changes = []
      for (let pos = sel.from; pos <= sel.to;) {
        const line = v.state.doc.lineAt(pos)
        const stripped = line.text.replace(/^(#{1,6}\s|[-*+]\s\[[ xX]\]\s|[-*+]\s|\d+\.\s|>\s)/, '')
        const has = line.text.startsWith(prefix)
        changes.push({ from: line.from, to: line.to, insert: has ? stripped : prefix + stripped })
        pos = line.to + 1
      }
      v.dispatch({ changes })
      v.focus()
    },
    currentLine: () => {
      const v = view.current
      if (!v) return null
      let line = v.state.doc.lineAt(v.state.selection.main.head)
      while (!line.text.trim() && line.number > 1) line = v.state.doc.line(line.number - 1)
      return line.text.trim() ? { text: line.text.trim(), from: line.from, to: line.to } : null
    },
    replaceLine: (from: number, text: string, md: string) => {
      const v = view.current
      if (!v) return false
      const line = v.state.doc.lineAt(Math.min(from, v.state.doc.length))
      const at = line.text.indexOf(text)
      if (at < 0) return false
      v.dispatch({ changes: { from: line.from + at, to: line.from + at + text.length, insert: md } })
      return true
    },
    focus: () => view.current?.focus(),
    scrollToLine: (n: number) => {
      const v = view.current
      if (!v) return
      const line = v.state.doc.line(Math.min(n + 1, v.state.doc.lines))
      v.dispatch({ selection: EditorSelection.cursor(line.from), effects: EditorView.scrollIntoView(line.from, { y: 'start' }) })
      v.focus()
    },
  }))

  return <div className="md-editor" ref={host} />
})
