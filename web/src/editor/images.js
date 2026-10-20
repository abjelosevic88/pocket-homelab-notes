// Milkdown's image nodes insist on a title that is a string, and Markdown's `![alt](src)` has
// none: the node is refused and the picture silently leaves the note at the next save. Every
// image gets an empty title (and alt) before the editor sees it; an empty title is not written
// back, so the Markdown stays as it was.
import { $remark } from '@milkdown/kit/utils'
import { visit } from 'unist-util-visit'
import { imageBlockSchema } from '@milkdown/kit/component/image-block'

export const remarkImageDefaults = $remark('remarkImageDefaults', () => () => (tree) => {
  // `image-block` is what a paragraph holding only a picture has become by now.
  visit(tree, (node) => node.type === 'image' || node.type === 'image-block', (node) => {
    if (typeof node.title !== 'string') { node.title = '' }
    if (typeof node.alt !== 'string') { node.alt = '' }
  })
})

// The image block keeps its size in the picture's alt text (`![0.50](src)`), which throws away
// the words that were there. Here the alt text stays what it was; a size is not written into
// the note at all (Nextcloud's editor has no sizes either).
export const imageBlockKeepsAlt = imageBlockSchema.extendSchema((prev) => (ctx) => {
  const base = prev(ctx)
  return Object.assign({}, base, {
    attrs: Object.assign({}, base.attrs, { alt: { default: '', validate: 'string' } }),
    parseMarkdown: {
      match: base.parseMarkdown.match,
      runner: (state, node, type) => {
        state.addNode(type, { src: node.url || '', caption: typeof node.title === 'string' ? node.title : '', alt: typeof node.alt === 'string' ? node.alt : '', ratio: 1 })
      },
    },
    toMarkdown: {
      match: base.toMarkdown.match,
      runner: (state, node) => {
        state.openNode('paragraph')
        state.addNode('image', undefined, undefined, { title: node.attrs.caption, url: node.attrs.src, alt: node.attrs.alt })
        state.closeNode()
      },
    },
  })
})
