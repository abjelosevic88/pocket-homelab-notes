// A structure to start from. Four come with the editor; the owner's own are the notes in a
// folder called Templates, which the app lists and hands over through `insertTemplate`.
// `{{date}}`, `{{time}}`, `{{now}}`, `{{iso}}` and `{{weekday}}` are filled in at that moment.
import { parserCtx } from '@milkdown/kit/core'
import { clearSlash, insertBlocks, post } from './shared.js'
import { stamps, weekday } from './inserts.js'
import { fromNextcloud } from './markdown.js'

export const builtIn = {
  meeting: ['# Meeting, {{date}}', '', '**With:** ', '', '## Agenda', '', '- ', '', '## Notes', '', '', '', '## Decisions', '', '- ', '', '## To do', '', '- [ ] ', ''].join('\n'),
  daily: ['# {{date}}', '', '## Today', '', '- [ ] ', '', '## Notes', '', '', '', '## Tomorrow', '', '- ', ''].join('\n'),
  checklist: ['# Checklist', '', '- [ ] ', '- [ ] ', '- [ ] ', ''].join('\n'),
  incident: ['# Incident, {{now}}', '', ':::warning', '**Status:** investigating', ':::', '', '## What happened', '', '', '', '## Affected', '', '- ', '',
    '## Timeline', '', '| Time | What |', '| --- | --- |', '| {{time}} | Noticed |', '', '## Cause', '', '', '', '## Fix', '', '', '', '## So it does not happen again', '', '- [ ] ', ''].join('\n'),
  // Steps with the commands to run; a shell block gets "Run on server" on a Mac with SSH set up.
  runbook: ['## Runbook: …', '', '**When:** …', '**Where:** the server, over SSH', '', '1. Look', '', '   ```sh', '   systemctl status …', '   ```', '',
    '2. Fix', '', '   ```sh', '   sudo systemctl restart …', '   ```', '', '3. Check that it worked', '', '   ```sh', '   journalctl -u … -n 20 --no-pager', '   ```', '',
    '**Last run:** {{now}}', ''].join('\n'),
  ports: ['**Open ports** · run on the server, then "Paste the output" below', '', '```sh', "sudo ss -ltnp | awk 'NR>1 {print $4, $6}' | sort -t: -k2 -n", "docker ps --format 'table {{.Names}}\\t{{.Ports}}'", '```', ''].join('\n'),
  compose: ['```yaml', 'services:', '  app:', '    image: image:tag', '    container_name: app', '    restart: unless-stopped', '    ports:', '      - "8080:80"', '    volumes:', '      - ./data:/data',
    '    environment:', '      - TZ=Etc/UTC', '```', ''].join('\n'),
}

export const fill = (markdown) => markdown.replace(/\{\{\s*(date|today|time|now|iso|weekday)\s*\}\}/gi, (_, key) => {
  switch (key.toLowerCase()) {
    case 'time': return stamps.time(); case 'now': return stamps.now(); case 'iso': return stamps.date(); case 'weekday': return weekday()
    default: return stamps.today()
  }
})

// A template's empty boxes (`- [ ] `) become real ones when it is parsed (emptylines.js).
function insert(ctx, markdown) {
  const parsed = ctx.get(parserCtx)(markdown)
  if (!parsed || !parsed.content.size) { return false }
  insertBlocks(ctx, parsed.content, 'start'); return true
}

export const templateItem = (key) => (ctx) => { clearSlash(ctx); insert(ctx, fill(builtIn[key])) }
export const ownTemplatesItem = (ctx) => { clearSlash(ctx); post('templates') }
export const insertTemplate = (ctx, markdown) => insert(ctx, fromNextcloud(fill(markdown)))
