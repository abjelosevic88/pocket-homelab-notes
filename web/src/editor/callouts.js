// Nextcloud's callouts, `::: info` … `:::`, as an editable block.
//
// The syntax is a container directive with a space after the colons. Only the container is
// switched on: the same extension would also read `:name` inside a sentence as a directive,
// and ordinary text must never be taken for markup.
import { directive } from 'micromark-extension-directive'
import { directiveFromMarkdown, directiveToMarkdown } from 'mdast-util-directive'
import { $node, $remark, $inputRule } from '@milkdown/kit/utils'
import { InputRule } from '@milkdown/kit/prose/inputrules'

function containerDirectives() {
  const data = this.data()
  const full = directive()
  const container = full.flow[58].filter((construct) => construct.concrete)
  const to = directiveToMarkdown()
  ;(data.micromarkExtensions ??= []).push({ flow: { 58: container } })
  ;(data.fromMarkdownExtensions ??= []).push(directiveFromMarkdown())
  // The handlers only; the extension's escaping rules would put backslashes before colons in prose.
  ;(data.toMarkdownExtensions ??= []).push({ handlers: { containerDirective: to.handlers.containerDirective } })
}

export const remarkCallouts = $remark('remarkCallouts', () => containerDirectives)

export const KINDS = ['info', 'success', 'warning', 'error', 'question', 'tip']

export const callout = $node('callout', () => ({
  content: 'block+',
  group: 'block',
  defining: true,
  attrs: { kind: { default: 'info' } },
  parseDOM: [{ tag: 'div[data-callout]', getAttrs: (dom) => ({ kind: dom.getAttribute('data-callout') || 'info' }) }],
  toDOM: (node) => ['div', { 'data-callout': node.attrs.kind, class: 'callout ' + node.attrs.kind }, 0],
  parseMarkdown: {
    match: (node) => node.type === 'containerDirective',
    runner: (state, node, type) => {
      state.openNode(type, { kind: String(node.name || 'info').toLowerCase() })
      // An empty callout still needs a line to type on.
      if (node.children && node.children.length) { state.next(node.children) } else { state.addNode(state.schema.nodes.paragraph) }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'callout',
    runner: (state, node) => {
      state.openNode('containerDirective', undefined, { name: node.attrs.kind })
      state.next(node.content)
      state.closeNode()
    },
  },
}))

// Typing `:::info` and a space starts one.
export const calloutInputRule = $inputRule((ctx) => new InputRule(/^:::\s?([A-Za-z]+)\s$/, (state, match, start, end) => {
  const type = callout.type(ctx)
  const paragraph = state.schema.nodes.paragraph.create()
  return state.tr.replaceRangeWith(start, end, type.create({ kind: match[1].toLowerCase() }, paragraph))
}))

// Outside code fences: `::: info` ⇄ `:::info`, the one difference between the two spellings.
export function mapOutsideFences(markdown, change) {
  let fence = null
  return markdown.split('\n').map((line) => {
    const mark = line.match(/^\s*(```+|~~~+)/)
    if (mark) {
      if (!fence) { fence = mark[1][0] } else if (mark[1][0] === fence) { fence = null }
      return line
    }
    return fence ? line : change(line)
  }).join('\n')
}

export const calloutFromNextcloud = (line) => {
  const m = line.match(/^(\s*):::[ \t]+([A-Za-z][\w-]*)[ \t]*(.*)$/)
  if (!m) { return line }
  return m[1] + ':::' + m[2] + (m[3] ? '[' + m[3].replace(/\]/g, '\\]') + ']' : '')
}

export const calloutToNextcloud = (line) => {
  const m = line.match(/^(\s*):::([A-Za-z][\w-]*)(?:\[(.*)\])?\s*$/)
  return m ? m[1] + '::: ' + m[2] + (m[3] ? ' ' + m[3] : '') : line
}
