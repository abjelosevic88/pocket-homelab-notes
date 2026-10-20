/** Unix seconds or ISO → "just now", "5 min ago", "3 h ago", "Yesterday", "Mon", "14 Mar", "14 Mar 2025". */
export function ago(t: string | number): string {
  if (!t) return ''
  const ms = typeof t === 'number' ? t * 1000 : new Date(t).getTime()
  if (isNaN(ms)) return ''
  const s = Math.max(0, (Date.now() - ms) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  const d = new Date(ms), now = new Date()
  const days = Math.round((new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() - new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()) / 86400000)
  if (days === 0) return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  if (days === 1) return 'Yesterday'
  if (days < 7) return d.toLocaleDateString(undefined, { weekday: 'long' })
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric' })
}

/** Full date and time for a tooltip. */
export function stamp(t: string | number): string {
  if (!t) return ''
  const d = typeof t === 'number' ? new Date(t * 1000) : new Date(t)
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

export function bytes(n: number): string {
  if (!n) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1048576).toFixed(1)} MB`
  return `${(n / 1073741824).toFixed(2)} GB`
}

export function initials(name: string): string {
  const parts = name.trim().split(/[\s@._-]+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

const PALETTE = ['#2563eb', '#7c3aed', '#0891b2', '#059669', '#d97706', '#dc2626', '#db2777', '#4b5563', '#b45309', '#0d9488']
export function colorFor(id: number): string { return PALETTE[Math.abs(id) % PALETTE.length] }

/** A stable colour for a tag or folder name. */
export function tint(name: string): string {
  let h = 0
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) | 0
  return PALETTE[Math.abs(h) % PALETTE.length]
}

export function today(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function isoDay(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/** The last segment of a folder path. */
export function leaf(path: string): string {
  const i = path.lastIndexOf('/')
  return i >= 0 ? path.slice(i + 1) : path
}

/** Snippets mark the words found with [[[ ]]]; split them for highlighting. */
export function snippetParts(s: string): { text: string; hit: boolean }[] {
  const out: { text: string; hit: boolean }[] = []
  const re = /\[\[\[(.*?)\]\]\]/g
  let last = 0, m: RegExpExecArray | null
  while ((m = re.exec(s))) {
    if (m.index > last) out.push({ text: s.slice(last, m.index), hit: false })
    out.push({ text: m[1], hit: true })
    last = m.index + m[0].length
  }
  if (last < s.length) out.push({ text: s.slice(last), hit: false })
  return out
}

/** The headings of a note, for the outline. */
export function headings(md: string): { level: number; text: string; line: number }[] {
  const out: { level: number; text: string; line: number }[] = []
  let fence = false
  md.split('\n').forEach((l, i) => {
    if (/^\s*(```|~~~)/.test(l)) { fence = !fence; return }
    if (fence) return
    const m = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(l)
    if (m) out.push({ level: m[1].length, text: m[2].replace(/[*_`]/g, ''), line: i })
  })
  return out
}

export function wordCount(md: string): number {
  return (md.replace(/```[\s\S]*?```/g, ' ').match(/[\p{L}\p{N}][\p{L}\p{N}'’-]*/gu) || []).length
}
