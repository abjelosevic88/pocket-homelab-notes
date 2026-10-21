export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }

export interface Note {
  id: number
  path: string
  category: string
  title: string
  content?: string
  etag: string
  size: number
  modified: number
  favorite: boolean
  pinned: boolean
  readonly: boolean
  excerpt: string
  words: number
  tasks_open: number
  tasks_done: number
  tags: string[]
  opened_at?: string
  created_at: string
  updated_at: string
}

export interface SearchHit extends Note { snippet: string; rank: number }
export interface Folder { path: string; count: number }
export interface TagCount { tag: string; count: number }

export interface TaskLine { line: number; text: string; done: boolean; due?: string; indent: number; note_id: number; title: string; category: string; modified: number }

export interface Version { id: number; note_id: number; title: string; path: string; content?: string; hash: string; source: string; size: number; created_at: string }
export interface DiffOp { kind: '=' | '+' | '-' | '…'; text: string }

export interface TrashItem { id: number; note_id: number; path: string; title: string; category: string; content?: string; excerpt: string; favorite: boolean; pinned: boolean; reason: 'deleted' | 'gone'; deleted_at: string }

export interface Share { id: number; note_id: number; note_title?: string; token: string; expires_at: string; views: number; created_at: string }

export interface Settings {
  title: string
  setup_complete: boolean
  editor: 'rich' | 'markdown'
  file_suffix: '.md' | '.txt'
  default_folder: string
  title_from_first_line: boolean
  sort_by: 'modified' | 'title' | 'created'
  spellcheck: boolean
  date_format: 'iso' | 'dmy-dot' | 'long'
  daily_folder: string
  daily_format: string
  daily_template: string
  capture_format: string
  templates_folder: string
  attachments_folder: string
  trash_days: number
  versions_keep: number
  sync_minutes: number
  storage_kind: StorageKind
  notify_kind: string
  notify_url: string
  notify_token_set: boolean
  notify_env: boolean
  core: boolean
}

export type StorageKind = 'builtin' | 'folder' | 'webdav' | 'nextcloud' | 'files'

export interface SyncStats { files: number; added: number; changed: number; moved: number; removed: number; unchanged: boolean; at: string; took: string }
export interface SyncStatus { syncing?: boolean; error?: string; failures?: number; last_sync?: string; kind?: string; where?: string; stats?: SyncStats }

export interface StorageInfo {
  kind: StorageKind
  path: string
  url: string
  user: string
  folder: string
  via_core: boolean
  password_set: boolean
  roots: string[] | null
  core: boolean
  status: SyncStatus
  core_nextcloud?: { url: string; user: string }
  core_nextcloud_error?: string
  core_files?: { on: boolean; url?: string; reason?: string }
}

export interface Integration { kind: 'svc' | 'app'; name: string; title: string; reason: string; mode: 'core' | 'direct' | 'off'; url: string; public_url: string; user: string; key_set: boolean; key_label: string; user_label: string }

export interface Dashboard { notes: number; edited_today: number; edited_week: number; open_tasks: number; due_tasks: number; favorites: number; folders: number; storage: string; last_sync: string; sync_error?: string }

export interface APIToken { id: number; name: string; prefix: string; last_used_at?: string; created_at: string }

export type Tools = Record<string, boolean>
