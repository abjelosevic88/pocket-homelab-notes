// What a template may say in {{…}}: the list `{{` offers while writing a template (both
// editors), with an example of what each gives. The server fills them in (templates.go).

export interface Placeholder { text: string; detail: string }

const pad = (n: number) => String(n).padStart(2, '0')
const shift = (days: number) => { const d = new Date(); d.setDate(d.getDate() + days); return d }

/** {{date:FORMAT}} as the server writes it: YYYY YY MMMM MMM MM M DD D dddd ddd HH mm ss, [text] as is. */
export function formatDate(d: Date, format: string): string {
  const name = (opts: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat('en-GB', opts).format(d)
  const tokens: [string, () => string][] = [
    ['YYYY', () => String(d.getFullYear())], ['MMMM', () => name({ month: 'long' })], ['dddd', () => name({ weekday: 'long' })],
    ['MMM', () => name({ month: 'short' })], ['ddd', () => name({ weekday: 'short' })], ['YY', () => pad(d.getFullYear() % 100)],
    ['MM', () => pad(d.getMonth() + 1)], ['DD', () => pad(d.getDate())], ['HH', () => pad(d.getHours())], ['mm', () => pad(d.getMinutes())],
    ['ss', () => pad(d.getSeconds())], ['M', () => String(d.getMonth() + 1)], ['D', () => String(d.getDate())],
  ]
  let out = ''
  for (let i = 0; i < format.length;) {
    if (format[i] === '[') { const j = format.indexOf(']', i); if (j > i) { out += format.slice(i + 1, j); i = j + 1; continue } }
    const t = tokens.find(([tok]) => format.startsWith(tok, i))
    if (t) { out += t[1](); i += t[0].length } else { out += format[i]; i++ }
  }
  return out
}

function isoWeek(d: Date): number {
  const t = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()))
  const day = t.getUTCDay() || 7
  t.setUTCDate(t.getUTCDate() + 4 - day)
  return Math.ceil(((t.getTime() - Date.UTC(t.getUTCFullYear(), 0, 1)) / 86400000 + 1) / 7)
}

/** Every placeholder, with what it gives now. */
export function placeholders(): Placeholder[] {
  const now = new Date()
  const f = (fmt: string, d = now) => formatDate(d, fmt)
  return [
    { text: '{{date}}', detail: 'Today, in your format (Settings → Writing)' },
    { text: '{{date:YYMMDD}}', detail: f('YYMMDD') },
    { text: '{{date:YYYY-MM-DD}}', detail: f('YYYY-MM-DD') },
    { text: '{{date:DD.MM.YYYY}}', detail: f('DD.MM.YYYY') },
    { text: '{{date:ddd D MMM}}', detail: f('ddd D MMM') },
    { text: '{{tomorrow}}', detail: 'Tomorrow, in your format' },
    { text: '{{yesterday}}', detail: 'Yesterday, in your format' },
    { text: '{{time}}', detail: f('HH:mm') },
    { text: '{{now}}', detail: 'Date and time' },
    { text: '{{weekday}}', detail: f('dddd') },
    { text: '{{week}}', detail: `Week number (${isoWeek(now)})` },
    { text: '{{month}}', detail: f('MMMM') },
    { text: '{{year}}', detail: f('YYYY') },
    { text: '{{title}}', detail: 'The new note’s title' },
    { text: '{{name}}', detail: 'The template’s name' },
    { text: '{{iso}}', detail: f('YYYY-MM-DD') },
  ]
}

const DATE_KEYS = ['date', 'today', 'tomorrow', 'yesterday', 'now', 'time', 'iso']

/** The placeholders for what was typed after `{{`; a date with a format of one's own is offered too. */
export function matchPlaceholders(typed: string): Placeholder[] {
  const q = typed.trim().toLowerCase()
  const all = placeholders()
  if (!q) return all
  const key = (p: Placeholder) => p.text.slice(2, -2).toLowerCase()
  const found = [...all.filter(p => key(p).startsWith(q)), ...all.filter(p => !key(p).startsWith(q) && key(p).includes(q))]
  const own = /^([a-z]+)\s*:\s*(.+)$/i.exec(typed.trim())
  if (own && DATE_KEYS.includes(own[1].toLowerCase()) && !found.some(p => key(p) === q)) {
    const day = own[1].toLowerCase() === 'tomorrow' ? shift(1) : own[1].toLowerCase() === 'yesterday' ? shift(-1) : new Date()
    found.unshift({ text: `{{${own[1].toLowerCase()}:${own[2]}}}`, detail: formatDate(day, own[2]) })
  }
  return found
}

/** `{{` and what follows it, right before the cursor. */
export const placeholderOpened = /\{\{([^{}\n]{0,40})$/
