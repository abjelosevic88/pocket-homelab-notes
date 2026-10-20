// Formulas: `$…$` inside a sentence, a `$$` block on lines of its own. KaTeX draws them; it is a
// file of its own, read only for a note that has a formula.
//
// A dollar sign is also money and a shell variable. Where the two dollars of `$HOME and $PATH`
// or `$5 to $10` would make a formula, the text stays text: the opening dollar must touch what
// follows, the closing one what came before, and no digit may follow it (the rule Pandoc and
// GitHub use). Nothing is escaped on the way out either, so prose keeps its dollars as typed.
import { math as mathSyntax } from 'micromark-extension-math'
import { mathFromMarkdown, mathToMarkdown } from 'mdast-util-math'
import { $node, $remark, $view, $inputRule } from '@milkdown/kit/utils'
import { InputRule, textblockTypeInputRule } from '@milkdown/kit/prose/inputrules'
import { codeBlockSchema } from '@milkdown/kit/preset/commonmark'
import { editorViewCtx } from '@milkdown/kit/core'
import { TextSelection } from '@milkdown/kit/prose/state'
import { clearSlash } from './shared.js'

export const MATH = 'math'

function sentenceMath(tree, source) {
  const walk = (parent) => {
    if (!Array.isArray(parent.children)) { return }
    let changed = false
    parent.children = parent.children.map((node) => {
      if (node.type === 'math') { return { type: 'code', lang: MATH, meta: null, value: node.value } }
      if (node.type !== 'inlineMath' || !node.position || !source) { return node }
      const raw = source.slice(node.position.start.offset, node.position.end.offset)
      if (raw.startsWith('$$')) { return node }
      const after = source[node.position.end.offset] || ''
      if (node.value && !/\s/.test(raw[1]) && !/\s/.test(raw[raw.length - 2]) && !/[0-9]/.test(after)) { return node }
      changed = true
      return { type: 'text', value: raw }
    })
    if (changed) {
      // Text next to text is one text again, so it is written back exactly as it came.
      parent.children = parent.children.reduce((all, node) => {
        const last = all[all.length - 1]
        if (last && last.type === 'text' && node.type === 'text') { last.value += node.value } else { all.push(node) }
        return all
      }, [])
    }
    parent.children.forEach(walk)
  }
  walk(tree)
}

function mathMarkdown() {
  const data = this.data()
  ;(data.micromarkExtensions ??= []).push(mathSyntax())
  ;(data.fromMarkdownExtensions ??= []).push(mathFromMarkdown())
  // The handlers only: the extension's escaping rules would put a backslash before every dollar in prose.
  ;(data.toMarkdownExtensions ??= []).push({ handlers: mathToMarkdown().handlers })
  return (tree, file) => sentenceMath(tree, file && file.value != null ? String(file.value) : '')
}

export const remarkMath = $remark('remarkNoteMath', () => mathMarkdown)

// KaTeX and its fonts, a part of the bundle of its own.
let loading = null
export function loadKatex() {
  if (window.katex) { return Promise.resolve(window.katex) }
  if (!loading) {
    loading = Promise.all([import('katex/dist/katex.mjs'), import('katex/dist/katex.min.css')]).then(([m]) => { window.katex = m.default || m; return window.katex })
  }
  return loading
}

export function drawFormula(into, tex, display) {
  into.textContent = display ? tex : '$' + tex + '$'
  return loadKatex().then((katex) => {
    try { katex.render(tex, into, { displayMode: !!display, throwOnError: false, output: 'html', strict: 'ignore', trust: false }) }
    catch (error) { into.textContent = tex; into.title = String(error.message || error) }
  }).catch((error) => { into.title = String(error.message || error) })
}

export const mathInline = $node('math_inline', () => ({
  group: 'inline', inline: true, atom: true, selectable: true,
  attrs: { value: { default: '' } },
  parseDOM: [{ tag: 'span[data-type="math_inline"]', getAttrs: (dom) => ({ value: dom.getAttribute('data-value') || '' }) }],
  toDOM: (node) => ['span', { 'data-type': 'math_inline', 'data-value': node.attrs.value }, '$' + node.attrs.value + '$'],
  parseMarkdown: { match: (node) => node.type === 'inlineMath', runner: (state, node, type) => { state.addNode(type, { value: node.value }) } },
  toMarkdown: { match: (node) => node.type.name === 'math_inline', runner: (state, node) => { state.addNode('inlineMath', undefined, node.attrs.value) } },
}))

// A small field under the formula: Return keeps it, Escape leaves it as it was, empty removes it.
let field = null
function closeField() { if (field) { field.remove(); field = null } }
function editFormula(view, getPos, node, anchor) {
  closeField()
  const box = document.createElement('div'); box.className = 'formula-field'
  const input = document.createElement('input'); input.type = 'text'; input.value = node.attrs.value
  input.placeholder = 'LaTeX, for example  \\frac{a}{b}'; input.setAttribute('aria-label', 'Formula'); input.spellcheck = false
  input.autocapitalize = 'off'; input.autocomplete = 'off'
  const preview = document.createElement('div'); preview.className = 'formula-preview'
  box.append(input, preview); document.body.append(box); field = box
  const rect = anchor.getBoundingClientRect()
  box.style.left = Math.max(8, Math.min(rect.left, innerWidth - 328)) + 'px'
  box.style.top = Math.min(rect.bottom + 6, innerHeight - 96) + 'px'
  const draw = () => { if (input.value.trim()) { drawFormula(preview, input.value.trim(), false) } else { preview.textContent = '' } }
  draw(); input.addEventListener('input', draw)
  let done = false
  const finish = (keep) => {
    if (done) { return } done = true; closeField()
    const pos = getPos(); if (pos == null) { return }
    const value = input.value.trim()
    if (keep && value !== node.attrs.value) {
      const tr = value ? view.state.tr.setNodeMarkup(pos, undefined, { value }) : view.state.tr.delete(pos, pos + 1)
      view.dispatch(tr)
    } else if (!node.attrs.value) { view.dispatch(view.state.tr.delete(pos, pos + 1)) }
    view.focus()
  }
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') { event.preventDefault(); finish(true) } else if (event.key === 'Escape') { event.preventDefault(); finish(false) }
    event.stopPropagation()
  })
  input.addEventListener('blur', () => finish(true))
  setTimeout(() => { input.focus(); input.select() }, 0)
}

export const mathInlineView = $view(mathInline, () => (initial, view, getPos) => {
  let node = initial
  const dom = document.createElement('span'); dom.className = 'math-inline'; dom.dataset.type = 'math_inline'
  const draw = () => { if (node.attrs.value) { drawFormula(dom, node.attrs.value, false) } else { dom.textContent = 'formula' ; dom.classList.add('empty') } }
  draw()
  dom.addEventListener('click', () => { if (view.editable) { editFormula(view, getPos, node, dom) } })
  // A formula put in by the `/` menu opens its field at once.
  if (pendingEdit && !node.attrs.value) { pendingEdit = false; setTimeout(() => editFormula(view, getPos, node, dom), 30) }
  return {
    dom,
    update(next) { if (next.type !== node.type) { return false } node = next; dom.classList.toggle('empty', !node.attrs.value); draw(); return true },
    ignoreMutation: () => true,
  }
})
let pendingEdit = false

// A `$$` block is a code block whose language is "math": the code is the LaTeX, the preview the formula.
export const mathBlock = codeBlockSchema.extendSchema((prev) => (ctx) => {
  const base = prev(ctx)
  return {
    ...base,
    toMarkdown: {
      match: base.toMarkdown.match,
      runner: (state, node) => {
        if ((node.attrs.language || '').toLowerCase() === MATH) { state.addNode('math', undefined, node.textContent) } else { base.toMarkdown.runner(state, node) }
      },
    },
  }
})

// Typing `$$` and a space starts a block; `$$x$$` in a sentence makes a formula of x. One dollar
// on each side is left alone while typing: that is how prices and shell variables are written.
export const mathBlockRule = $inputRule((ctx) => textblockTypeInputRule(/^\$\$\s$/, codeBlockSchema.type(ctx), () => ({ language: MATH })))
export const mathInlineRule = $inputRule((ctx) => new InputRule(/\$\$([^$\s][^$]*?)\$\$$/, (state, match, start, end) => {
  if (state.doc.resolve(start).parent.type.spec.code) { return null }
  return state.tr.replaceRangeWith(start, end, mathInline.type(ctx).create({ value: match[1].trim() }))
}))

export const mathPlugins = [remarkMath, mathInline, mathInlineView, mathBlock, mathBlockRule, mathInlineRule]

export function mathPreview(content, apply) {
  const box = document.createElement('div'); box.className = 'formula-block'
  drawFormula(box, content.trim(), true).then(() => apply(box))
  return undefined
}

export const mathBlockItem = (ctx) => {
  clearSlash(ctx)
  const view = ctx.get(editorViewCtx), { state } = view
  const node = codeBlockSchema.type(ctx).create({ language: MATH })
  const $from = state.selection.$from
  const empty = $from.parent.content.size === 0
  const at = empty ? $from.before() : $from.after()
  const tr = empty ? state.tr.replaceWith(at, $from.after(), node) : state.tr.insert(at, node)
  tr.setSelection(TextSelection.create(tr.doc, at + 1))
  view.dispatch(tr.scrollIntoView()); view.focus()
}

export const mathInlineItem = (ctx) => {
  clearSlash(ctx)
  const view = ctx.get(editorViewCtx)
  pendingEdit = true
  view.dispatch(view.state.tr.replaceSelectionWith(mathInline.type(ctx).create({ value: '' })).scrollIntoView())
}
