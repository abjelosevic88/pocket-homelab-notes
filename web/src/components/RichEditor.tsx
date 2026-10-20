import { forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState } from 'react'

// The rich editor is the Milkdown editor the Pocket Homelab app ships (src/editor), running in a
// frame of this page as it runs in a web view there. It tells the page what happens (an edit, a
// link, a tool from its `/` menu) and the page answers by calling into it.

export interface EditorMessage { name: string; [k: string]: unknown }

interface NoteEditorAPI {
  load(markdown: string, options: Record<string, unknown>): Promise<boolean>
  setKeyboard(visible: boolean, hardware?: boolean): void
  focus(atEnd?: boolean): void
  toggleInsertMenu(): boolean
  flush(doc: string): string | null
  insertMarkdown(markdown: string, noteID?: number): boolean
  insertOwnTemplate(markdown: string): boolean
  insertInline(markdown: string): boolean
  replaceLineText(from: number, text: string, markdown: string): boolean
  currentLine(): { text: string; from: number; to: number } | null
  notice(text: string): void
}

export interface RichEditorHandle {
  /** What was typed and not yet reported (null when nothing). */
  flush(): string | null
  insertMarkdown(md: string): boolean
  insertInline(md: string): boolean
  replaceLine(from: number | undefined, text: string, md: string): boolean
  insertTemplate(md: string): boolean
  currentLine(): { text: string; from: number; to: number } | null
  notice(text: string): void
  focus(): void
  /** The rendered note, for printing and HTML export. */
  frame(): HTMLIFrameElement | null
  scrollToHeading(text: string, index: number): void
}

interface Props {
  /** Which document is open; a change loads the new one. */
  docKey: string
  /** Bumped by the page to load `content` again (a reload from the server). */
  revision: number
  content: string
  noteID?: number
  share?: string
  readonly?: boolean
  checkable?: boolean
  focus?: boolean
  tools?: string[]
  theme: 'light' | 'dark'
  spellcheck?: boolean
  placeholder?: string
  onChange?: (markdown: string) => void
  onMessage?: (m: EditorMessage) => void
  onRequest?: (m: EditorMessage) => Promise<unknown>
  onFailed?: (message: string) => void
}

type HostWindow = Window & { __noteHost?: (m: EditorMessage) => void; __noteHostRequest?: (m: EditorMessage) => Promise<unknown> }

export const RichEditor = forwardRef<RichEditorHandle, Props>(function RichEditor(props, ref) {
  const frame = useRef<HTMLIFrameElement>(null)
  const [ready, setReady] = useState(false)
  const latest = useRef(props)
  latest.current = props
  const loaded = useRef<{ doc: string; revision: number } | null>(null)

  const api = (): NoteEditorAPI | null => {
    const w = frame.current?.contentWindow as (Window & { noteEditor?: NoteEditorAPI }) | null
    return w?.noteEditor ?? null
  }

  // The page answers the frame: one editor on the page at a time.
  useLayoutEffect(() => {
    const host = window as HostWindow
    host.__noteHost = (m: EditorMessage) => {
      const p = latest.current
      switch (m.name) {
        case 'ready': setReady(true); break
        case 'changed': if (m.document === p.docKey) p.onChange?.(String(m.markdown ?? '')); break
        case 'failed': p.onFailed?.(String(m.message ?? 'The editor could not open this note')); break
        case 'error': console.warn('editor:', m.message); break
        case 'log': break
        default: p.onMessage?.(m)
      }
    }
    host.__noteHostRequest = (m: EditorMessage) => latest.current.onRequest?.(m) ?? Promise.resolve(null)
    return () => { delete host.__noteHost; delete host.__noteHostRequest }
  }, [])

  const load = () => {
    const ed = api()
    if (!ed) return
    const p = latest.current
    loaded.current = { doc: p.docKey, revision: p.revision }
    const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim()
    ed.load(p.content, {
      document: p.docKey, noteID: p.noteID ?? null, share: p.share ?? null, desktop: window.innerWidth > 760, compact: false,
      tools: p.tools ?? [], readonly: !!p.readonly, checkable: !!p.checkable, focus: !!p.focus, accent, theme: p.theme,
      spellcheck: p.spellcheck !== false, placeholder: p.placeholder,
    }).then(() => {
      // A real keyboard: the editor's toolbar follows its focus.
      ed.setKeyboard(false, true)
    })
  }

  useEffect(() => {
    if (!ready) return
    const l = loaded.current
    if (!l || l.doc !== props.docKey || l.revision !== props.revision) load()
  }, [ready, props.docKey, props.revision]) // eslint-disable-line react-hooks/exhaustive-deps

  // Theme changes reach the frame without reloading the note.
  useEffect(() => {
    const doc = frame.current?.contentDocument
    if (doc) doc.documentElement.dataset.theme = props.theme
  }, [props.theme, ready])

  useImperativeHandle(ref, () => ({
    flush: () => api()?.flush(latest.current.docKey) ?? null,
    insertMarkdown: (md: string) => api()?.insertMarkdown(md, latest.current.noteID) ?? false,
    insertInline: (md: string) => api()?.insertInline(md) ?? false,
    replaceLine: (from: number | undefined, text: string, md: string) => {
      const ed = api()
      if (!ed) return false
      if (from != null && ed.replaceLineText(from, text, md)) return true
      return ed.insertInline(md)
    },
    insertTemplate: (md: string) => api()?.insertOwnTemplate(md) ?? false,
    currentLine: () => api()?.currentLine() ?? null,
    notice: (text: string) => api()?.notice(text),
    focus: () => api()?.focus(false),
    frame: () => frame.current,
    scrollToHeading: (text: string, index: number) => {
      const doc = frame.current?.contentDocument
      if (!doc) return
      const all = Array.from(doc.querySelectorAll('.ProseMirror h1, .ProseMirror h2, .ProseMirror h3, .ProseMirror h4, .ProseMirror h5, .ProseMirror h6'))
      const match = all.filter(h => (h.textContent || '').trim() === text.trim())
      const el = match[Math.min(index, match.length - 1)] || all[index]
      el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    },
  }))

  return <iframe ref={frame} className="rich-frame" src="/editor.html" title="Note editor" onLoad={() => { if (api()) setReady(true) }} />
})
