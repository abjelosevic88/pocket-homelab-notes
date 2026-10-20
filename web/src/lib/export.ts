// A note as one web page that needs nothing else (pictures inlined), and the same page printed
// to PDF: what "Share as HTML / PDF" in the editor's `/` menu makes.

async function styles(doc: Document): Promise<string> {
  let css = ''
  for (const node of Array.from(doc.querySelectorAll('style, link[rel="stylesheet"]'))) {
    if (node.tagName === 'STYLE') css += node.textContent + '\n'
    else {
      try { css += await (await fetch((node as HTMLLinkElement).href)).text() + '\n' } catch {}
    }
  }
  return css
}

async function inlineImages(root: HTMLElement) {
  for (const img of Array.from(root.querySelectorAll('img'))) {
    try {
      const res = await fetch(img.src, { credentials: 'same-origin' })
      const blob = await res.blob()
      img.src = await new Promise<string>((ok, bad) => { const r = new FileReader(); r.onload = () => ok(String(r.result)); r.onerror = bad; r.readAsDataURL(blob) })
    } catch {}
  }
}

const escape = (s: string) => s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]!))

async function page(frame: HTMLIFrameElement, title: string, inline: boolean): Promise<string> {
  const doc = frame.contentDocument
  const pm = doc?.querySelector('.ProseMirror') as HTMLElement | null
  if (!doc || !pm) throw new Error('Open the note in the rich editor first')
  const clone = pm.cloneNode(true) as HTMLElement
  clone.removeAttribute('contenteditable')
  clone.querySelectorAll('[contenteditable]').forEach(e => e.removeAttribute('contenteditable'))
  clone.querySelectorAll('.milkdown-block-handle, .tools, .diagram-open, .ProseMirror-widget, .note-command-bar').forEach(e => e.remove())
  if (inline) await inlineImages(clone)
  const css = await styles(doc)
  const theme = doc.documentElement.dataset.theme || 'light'
  return `<!doctype html><html data-theme="${theme}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>${escape(title)}</title>
<style>${css}
html, body { height: auto; overflow: visible; background: ${theme === 'dark' ? '#1c1c1e' : '#fff'}; }
.milkdown .ProseMirror { min-height: 0 !important; max-width: 820px; margin: 0 auto; padding: 32px 24px 48px !important; }
@media print { .milkdown .ProseMirror { padding: 0 !important; } html, body { background: #fff; } }
</style></head><body><div class="milkdown"><div class="ProseMirror">${clone.innerHTML}</div></div></body></html>`
}

export async function exportHTML(frame: HTMLIFrameElement, title: string) {
  const html = await page(frame, title, true)
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([html], { type: 'text/html' }))
  a.download = `${title.replace(/[\\/:*?"<>|]+/g, ' ').trim() || 'note'}.html`
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 5000)
}

export async function printNote(frame: HTMLIFrameElement, title: string) {
  const html = await page(frame, title, false)
  const w = window.open('', '_blank')
  if (!w) throw new Error('The browser blocked the print window')
  w.document.open(); w.document.write(html.replace('data-theme="dark"', 'data-theme="light"')); w.document.close()
  w.onload = () => { w.focus(); w.print() }
  setTimeout(() => { try { w.focus(); w.print() } catch {} }, 1200)
}
