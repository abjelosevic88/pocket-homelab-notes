import type { Episode } from './types'

export function ago(iso: string): string {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  const s = Math.max(0, (Date.now() - t) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  if (s < 86400 * 7) return `${Math.floor(s / 86400)} d ago`
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: new Date(iso).getFullYear() === new Date().getFullYear() ? undefined : 'numeric' })
}

/** "2026-03-14T…" → "14 Mar" / "14 Mar 2025"; "" stays "". */
export function fmtDate(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: d.getFullYear() === new Date().getFullYear() ? undefined : 'numeric' })
}

/** 3723 → "1h 2m", 125 → "2 min", 0 → "". */
export function fmtDur(sec: number): string {
  if (!sec || sec <= 0) return ''
  const h = Math.floor(sec / 3600), m = Math.round((sec % 3600) / 60)
  if (h > 0) return m ? `${h}h ${m}m` : `${h}h`
  if (m > 0) return `${m} min`
  return `${Math.round(sec)} s`
}

/** 3723 → "1:02:03", 125 → "2:05". */
export function clock(sec: number): string {
  if (!isFinite(sec) || sec < 0) sec = 0
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60
  const mm = h > 0 ? String(m).padStart(2, '0') : String(m)
  return (h > 0 ? `${h}:` : '') + `${mm}:${String(r).padStart(2, '0')}`
}

/** Hours with one decimal from seconds: 5400 → "1.5 h". */
export function hours(sec: number): string {
  if (!sec) return '0 h'
  const h = sec / 3600
  return h >= 10 ? `${Math.round(h)} h` : `${h.toFixed(1)} h`
}

export function bytes(n: number): string {
  if (!n) return '0 B'
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

/** Stable muted colour for an artwork placeholder, from the title. */
const ARTS = ['#5b4b8a', '#2f5d62', '#8a4b4b', '#3e5a87', '#6b5b3e', '#4a6b45', '#7a4a6e', '#33475b', '#8a6a3e', '#4b4b6b']
export function artColor(title: string): string {
  let h = 0
  for (const c of title) h = (h * 31 + c.charCodeAt(0)) | 0
  return ARTS[Math.abs(h) % ARTS.length]
}

/** Time left in an episode, "12 min left" / "". */
export function left(e: Pick<Episode, 'duration' | 'play_position' | 'completed'>): string {
  if (!e.duration || e.completed) return ''
  const r = e.duration - e.play_position
  return r > 0 && e.play_position > 0 ? `${fmtDur(r)} left` : ''
}

export function progressPct(e: Pick<Episode, 'duration' | 'play_position' | 'completed'>): number {
  if (e.completed) return 100
  if (!e.duration || !e.play_position) return 0
  return Math.min(100, Math.round((e.play_position * 100) / e.duration))
}

/** Strip HTML into plain text for one-line summaries. */
export function plain(html: string, max = 200): string {
  const d = document.createElement('div')
  d.innerHTML = html
  const t = (d.textContent || '').replace(/\s+/g, ' ').trim()
  return t.length > max ? t.slice(0, max - 1) + '…' : t
}

export const DL_LABEL: Record<string, string> = { none: '', queued: 'Queued', downloading: 'Downloading…', done: 'On server', failed: 'Download failed', culled: 'File removed' }

export const SB_CATEGORIES: { id: string; label: string; hint: string }[] = [
  { id: 'sponsor', label: 'Sponsor', hint: 'Paid promotions and ads' },
  { id: 'selfpromo', label: 'Self-promotion', hint: 'Merch, Patreon, own channels' },
  { id: 'interaction', label: 'Interaction reminder', hint: '“Like and subscribe”' },
  { id: 'intro', label: 'Intro', hint: 'Intermission / intro animation' },
  { id: 'outro', label: 'Outro', hint: 'End cards and credits' },
  { id: 'preview', label: 'Preview / recap', hint: 'What is coming up' },
  { id: 'music_offtopic', label: 'Non-music', hint: 'In music videos only' },
  { id: 'filler', label: 'Filler', hint: 'Tangents and jokes (aggressive)' },
]
