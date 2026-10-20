// The places where Nextcloud's spelling of Markdown and the editor's differ, outside code
// fences only: callouts (`::: info` ⇄ `:::info`), the empty lines a folding section needs, and
// the `<br />` Milkdown would write for an empty line, which this editor leaves out.
import { mapOutsideFences, calloutFromNextcloud, calloutToNextcloud } from './callouts.js'
import { detailsFromNextcloud } from './details.js'
import { tidyEmptyLines } from './emptylines.js'

export const fromNextcloud = (markdown) => mapOutsideFences(markdown, (line) => detailsFromNextcloud(calloutFromNextcloud(line)))
export const toNextcloud = (markdown) => tidyEmptyLines(mapOutsideFences(markdown, calloutToNextcloud))
