// Editor-owned menus keep commands in ProseMirror's history and target the right-clicked
// block/cell, rather than whichever text happened to have the caret previously.
import { $prose } from '@milkdown/kit/utils'
import { Plugin, TextSelection, NodeSelection } from '@milkdown/kit/prose/state'
import { undo, redo } from '@milkdown/kit/prose/history'
import { CellSelection, selectedRect, addColumnBefore, addColumnAfter, deleteColumn, deleteRow, deleteTable, setCellAttr } from '@milkdown/kit/prose/tables'
import { commandsCtx } from '@milkdown/kit/core'
import { addRowBeforeCommand, addRowAfterCommand } from '@milkdown/kit/preset/gfm'
import { enabled as tools, lineAtCursor } from './homelab.js'
import { post } from './shared.js'

export const contextMenu = $prose((ctx) => {
  let menu = null
  let documentAtOpen = null
  const close = () => { menu?.remove(); menu = null; documentAtOpen = null }
  const outside = (event) => { if (!menu?.contains(event.target)) close() }
  const plugin = new Plugin({
    props: {
      handleDOMEvents: {
        contextmenu(view, event) {
          close()
          if (!view.editable || event.target.closest('input,textarea')) return false
          const hit = view.posAtCoords({ left: event.clientX, top: event.clientY })
          if (!hit) return false
          const cell = event.target.closest('td,th')
          let cellPos = null
          if (cell) {
            const $pos = view.state.doc.resolve(view.posAtDOM(cell, 0))
            for (let depth = $pos.depth; depth > 0; depth--) {
              if (['cell', 'header_cell'].includes($pos.node(depth).type.spec.tableRole)) {
                cellPos = $pos.before(depth); break
              }
            }
            if (cellPos == null) return false
            view.dispatch(view.state.tr.setSelection(CellSelection.create(view.state.doc, cellPos)))
          } else {
            const blockDOM = event.target.closest('hr,.milkdown-code-block,.callout')
            const pos = blockDOM ? view.posAtDOM(blockDOM, 0) : hit.pos
            const $pos = view.state.doc.resolve(pos)
            if (blockDOM) {
              const depth = $pos.depth
              const from = depth > 0 ? $pos.before(depth) : pos
              view.dispatch(view.state.tr.setSelection(NodeSelection.create(view.state.doc, from)))
            } else {
              view.dispatch(view.state.tr.setSelection(TextSelection.near($pos)))
            }
          }
          event.preventDefault()
          documentAtOpen = view.state.doc
          menu = document.createElement('div')
          menu.className = 'note-context-menu'; menu.setAttribute('role', 'menu')
          menu.setAttribute('aria-label', cell ? 'Table actions' : 'Block actions')
          const heading = (text) => { const el = document.createElement('div'); el.className = 'menu-heading'; el.textContent = text; menu.append(el) }
          const item = (label, action, enabled = true, destructive = false) => {
            const button = document.createElement('button'); button.type = 'button'
            button.textContent = label; button.disabled = !enabled; button.setAttribute('role', 'menuitem')
            if (destructive) button.className = 'destructive'
            button.addEventListener('click', () => { close(); view.focus(); action() })
            menu.append(button)
          }
          const command = (label, cmd, enabled = true, destructive = false) => item(label,
            () => cmd(view.state, view.dispatch, view), enabled && cmd(view.state), destructive)
          if (cell) {
            const rect = selectedRect(view.state)
            heading('Rows')
            item('Insert row above', () => ctx.get(commandsCtx).call(addRowBeforeCommand.key), rect.top > 0)
            item('Insert row below', () => ctx.get(commandsCtx).call(addRowAfterCommand.key))
            item('Select row', () => view.dispatch(view.state.tr.setSelection(CellSelection.rowSelection(view.state.doc.resolve(cellPos)))))
            command('Delete row', deleteRow, rect.top > 0 && rect.map.height > 2, true)
            heading('Columns')
            command('Insert column left', addColumnBefore)
            command('Insert column right', addColumnAfter)
            item('Select column', () => view.dispatch(view.state.tr.setSelection(CellSelection.colSelection(view.state.doc.resolve(cellPos)))))
            command('Delete column', deleteColumn, rect.map.width > 1, true)
            heading('Column alignment')
            for (const alignment of ['left', 'center', 'right']) {
              item('Align ' + alignment, () => {
                view.dispatch(view.state.tr.setSelection(CellSelection.colSelection(view.state.doc.resolve(cellPos))))
                setCellAttr('alignment', alignment)(view.state, view.dispatch)
              })
            }
            heading('Table')
            command('Delete table', deleteTable, true, true)
          } else {
            const $pos = view.state.selection.$from
            let depth = $pos.depth
            // A list item is one block, including any nested content it owns.
            for (let d = depth; d > 0; d--) {
              if ($pos.node(d).type.name === 'list_item') { depth = d; break }
            }
            const selectedNode = view.state.selection instanceof NodeSelection ? view.state.selection.node : null
            if (depth > 0 || selectedNode) {
              const from = selectedNode ? view.state.selection.from : $pos.before(depth)
              const node = selectedNode || $pos.node(depth), to = from + node.nodeSize
              heading('Block')
              item('Select block', () => view.dispatch(view.state.tr.setSelection(NodeSelection.create(view.state.doc, from))))
              item('Duplicate block', () => view.dispatch(view.state.tr.insert(to, node).scrollIntoView()))
              if (node.type.name !== 'list_item') {
                for (const [label, pos] of [['Insert paragraph above', from], ['Insert paragraph below', to]]) {
                  const paragraph = view.state.schema.nodes.paragraph.createAndFill()
                  const $at = view.state.doc.resolve(pos)
                  item(label, () => {
                    const tr = view.state.tr.insert(pos, paragraph)
                    tr.setSelection(TextSelection.near(tr.doc.resolve(pos + 1)))
                    view.dispatch(tr.scrollIntoView())
                  }, $at.parent.canReplaceWith($at.index(), $at.index(), paragraph.type))
                }
              }
              item('Delete block', () => view.dispatch(view.state.tr.delete(from, to).scrollIntoView()), true, true)
            }
            // The line, sent to Nextcloud Tasks or Deck by the app, which then puts a link behind it.
            const line = tools.has('task') ? lineAtCursor(view.state) : null
            if (line && line.from >= (depth > 0 ? $pos.before(depth) : 0)) {
              heading('Homelab')
              item('Make this line a task…', () => post('tool', Object.assign({ tool: 'task' }, line)))
            }
          }
          heading('History')
          command('Undo', undo); command('Redo', redo)
          menu.addEventListener('keydown', (event) => {
            const buttons = [...menu.querySelectorAll('button:not(:disabled)')]
            const index = buttons.indexOf(document.activeElement)
            if (event.key === 'Escape' || event.key === 'Tab') { event.preventDefault(); close(); view.focus() }
            else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
              event.preventDefault()
              const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1
                : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length
              buttons[next]?.focus()
            }
          })
          document.body.append(menu)
          const bounds = menu.getBoundingClientRect()
          menu.style.left = Math.max(8, Math.min(event.clientX, innerWidth - bounds.width - 8)) + 'px'
          menu.style.top = Math.max(8, Math.min(event.clientY, innerHeight - bounds.height - 8)) + 'px'
          menu.querySelector('button:not(:disabled)')?.focus({ preventScroll: true })
          return true
        },
      },
    },
    view(view) {
      // CodeMirror node views stop bubbling events; capture before their handler.
      const context = event => {
        if (plugin.props.handleDOMEvents.contextmenu(view, event)) event.stopPropagation()
      }
      view.dom.addEventListener('contextmenu', context, true)
      document.addEventListener('pointerdown', outside, true)
      window.addEventListener('resize', close)
      window.addEventListener('scroll', outside, true)
      return {
        update(view) { if (menu && view.state.doc !== documentAtOpen) close() },
        destroy() { close(); view.dom.removeEventListener('contextmenu', context, true); document.removeEventListener('pointerdown', outside, true); window.removeEventListener('resize', close); window.removeEventListener('scroll', outside, true) },
      }
    },
  })
  return plugin
})
