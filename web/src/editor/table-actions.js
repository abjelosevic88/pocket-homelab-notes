// Table editing for touch screens, which have no right-click. The toolbar switches to these
// while the cursor is in a table; the same commands back the desktop context menu.
import { commandsCtx, editorViewCtx } from '@milkdown/kit/core'
import { $prose } from '@milkdown/kit/utils'
import { Plugin, TextSelection } from '@milkdown/kit/prose/state'
import { CellSelection, selectedRect, isInTable, addColumnBefore, addColumnAfter, deleteColumn, deleteRow, deleteTable, setCellAttr } from '@milkdown/kit/prose/tables'
import { addRowBeforeCommand, addRowAfterCommand } from '@milkdown/kit/preset/gfm'
import { svg } from './shared.js'

// Rows/columns: a grid with a highlighted band and a plus or minus.
const icons = {
  rowAbove: svg('<rect x="3" y="11" width="18" height="10" rx="2"/><path d="M3 16h18M12 2v6M9 5h6"/>'),
  rowBelow: svg('<rect x="3" y="3" width="18" height="10" rx="2"/><path d="M3 8h18M12 16v6M9 19h6"/>'),
  colLeft: svg('<rect x="11" y="3" width="10" height="18" rx="2"/><path d="M16 3v18M2 12h6M5 9v6"/>'),
  colRight: svg('<rect x="3" y="3" width="10" height="18" rx="2"/><path d="M8 3v18M16 12h6M19 9v6"/>'),
  deleteRow: svg('<rect x="3" y="8" width="18" height="8" rx="2"/><path d="M9 12h6"/>'),
  deleteCol: svg('<rect x="8" y="3" width="8" height="18" rx="2"/><path d="M12 9v6"/>'),
  alignLeft: svg('<path d="M4 6h16M4 10h10M4 14h16M4 18h10"/>'),
  alignCenter: svg('<path d="M4 6h16M7 10h10M4 14h16M7 18h10"/>'),
  alignRight: svg('<path d="M4 6h16M10 10h10M4 14h16M10 18h10"/>'),
  deleteTable: svg('<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 3v18M14 14l5 5M19 14l-5 5"/>'),
  leave: svg('<rect x="3" y="3" width="12" height="10" rx="2"/><path d="M9 17v4h12M18 18l3 3-3 3"/>'),
}

function cellPosition(state) {
  const { $from } = state.selection
  for (let depth = $from.depth; depth > 0; depth--) {
    if (['cell', 'header_cell'].includes($from.node(depth).type.spec.tableRole)) return $from.before(depth)
  }
  return null
}

function tableDepth($pos) {
  for (let depth = $pos.depth; depth > 0; depth--) if ($pos.node(depth).type.spec.tableRole === 'table') return depth
  return null
}

/// The actions for the cell under the cursor, or null when the cursor is not in a table.
export function tableActions(ctx) {
  const view = ctx.get(editorViewCtx)
  const { state } = view
  if (!view.editable || !isInTable(state)) return null
  const rect = selectedRect(state)
  const cellPos = cellPosition(state)
  const run = cmd => () => cmd(view.state, view.dispatch, view)
  const align = alignment => () => {
    if (cellPos == null) return
    view.dispatch(view.state.tr.setSelection(CellSelection.colSelection(view.state.doc.resolve(cellPos))))
    setCellAttr('alignment', alignment)(view.state, view.dispatch)
    // Back to a caret in the same cell, so typing continues where it was.
    const $cell = view.state.doc.resolve(cellPos + 1)
    view.dispatch(view.state.tr.setSelection(TextSelection.near($cell)))
  }
  const leave = () => {
    const $from = view.state.selection.$from
    const depth = tableDepth($from)
    if (depth == null) return
    const after = $from.after(depth)
    let tr = view.state.tr
    const next = tr.doc.resolve(after).nodeAfter
    if (!next || !next.isTextblock) tr = tr.insert(after, view.state.schema.nodes.paragraph.create())
    view.dispatch(tr.setSelection(TextSelection.near(tr.doc.resolve(after + 1))).scrollIntoView())
  }
  return [
    { key: 'table-row-below', title: 'Row below', icon: icons.rowBelow, run: () => ctx.get(commandsCtx).call(addRowAfterCommand.key) },
    // The first row is the Markdown header; nothing can go above it.
    { key: 'table-row-above', title: 'Row above', icon: icons.rowAbove, run: () => ctx.get(commandsCtx).call(addRowBeforeCommand.key), enabled: rect.top > 0 },
    { key: 'table-col-right', title: 'Column right', icon: icons.colRight, run: run(addColumnAfter), enabled: addColumnAfter(state) },
    { key: 'table-col-left', title: 'Column left', icon: icons.colLeft, run: run(addColumnBefore), enabled: addColumnBefore(state) },
    { key: 'table-delete-row', title: 'Delete row', icon: icons.deleteRow, run: run(deleteRow), enabled: rect.top > 0 && rect.map.height > 2 && deleteRow(state), destructive: true },
    { key: 'table-delete-col', title: 'Delete column', icon: icons.deleteCol, run: run(deleteColumn), enabled: rect.map.width > 1 && deleteColumn(state), destructive: true },
    { key: 'table-align-left', title: 'Align left', icon: icons.alignLeft, iconOnly: true, run: align('left') },
    { key: 'table-align-center', title: 'Align center', icon: icons.alignCenter, iconOnly: true, run: align('center') },
    { key: 'table-align-right', title: 'Align right', icon: icons.alignRight, iconOnly: true, run: align('right') },
    { key: 'table-delete', title: 'Delete table', icon: icons.deleteTable, run: run(deleteTable), destructive: true },
    { key: 'table-leave', title: 'Below table', icon: icons.leave, run: leave },
  ]
}

// Tells the toolbar each time the selection or document changes.
let selectionListener = null
export const setTableSelectionListener = listener => { selectionListener = listener }
export const tableSelectionTracker = $prose(() => new Plugin({
  view: () => ({
    update(view, previous) {
      // Stored marks change when Bold is toggled with nothing selected; the toolbar shows that too.
      if (view.state.selection.eq(previous.selection) && view.state.doc.eq(previous.doc) && view.state.storedMarks === previous.storedMarks && view.state.plugins.every(p => p.getState(view.state) === p.getState(previous))) return
      selectionListener?.()
    },
  }),
}))
