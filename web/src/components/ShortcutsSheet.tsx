import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['t'], 'Today'], [['a'], 'All notes'], [['x'], 'Tasks'], [['c'], 'Calendar'], [[','], 'Settings'], [['⌘', '⇧', 'D'], 'Today, from anywhere']] },
  { title: 'Find', keys: [[['⌘', 'K'], 'Find a note or run a command'], [['/'], 'The same, when nothing is typed']] },
  { title: 'Write', keys: [[['n'], 'New note'], [['Alt', 'N'], 'New note, from anywhere'], [['⌘', 'S'], 'Save now'], [['/'], 'In a note: blocks and homelab tools'], [['[', '['], 'In a note: link another note'], [['@'], 'In a note: a person from Pocket Contacts']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog or menu'], [['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map((k, i) => <kbd key={k + i}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
      <p className="muted small mt">New notes go to the folder you are looking at, or the one set in Settings → Writing. On a Mac, ⌘ is Ctrl elsewhere.</p>
    </Modal>
  )
}
