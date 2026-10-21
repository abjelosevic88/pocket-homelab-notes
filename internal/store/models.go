package store

// User is an account that can sign in. The first user is the admin; admins
// manage other users. Every user has their own subscriptions.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Note is one note: a Markdown file in the person's notes folder.
type Note struct {
	ID         int64    `json:"id"`
	UserID     int64    `json:"-"`
	Path       string   `json:"path"`
	Category   string   `json:"category"`
	Title      string   `json:"title"`
	Content    string   `json:"content,omitempty"`
	Hash       string   `json:"etag"`
	RemoteETag string   `json:"-"`
	FileID     string   `json:"-"`
	Size       int64    `json:"size"`
	Modified   int64    `json:"modified"` // unix seconds
	Favorite   bool     `json:"favorite"`
	Pinned     bool     `json:"pinned"`
	Readonly   bool     `json:"readonly"`
	Excerpt    string   `json:"excerpt"`
	Words      int      `json:"words"`
	TasksOpen  int      `json:"tasks_open"`
	TasksDone  int      `json:"tasks_done"`
	Tags       []string `json:"tags"`
	OpenedAt   string   `json:"opened_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
}

// NoteFilter narrows ListNotes.
type NoteFilter struct {
	Category  *string // exact folder ("" = the root); nil = every folder
	Recursive bool    // with Category: its subfolders too
	Tag       string
	Favorite  bool
	Pinned    bool
	HasTasks  bool
	Sort      string // modified | title | created | opened
	Limit     int
	Since     int64 // modified after (unix)
	Content   bool  // include content
}

// Folder is a category with how many notes are in it (not counting subfolders).
type Folder struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// TagCount is a tag with how many notes carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// SearchHit is a note found by its words.
type SearchHit struct {
	Note
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
}

// Version is one saved state of a note.
type Version struct {
	ID        int64  `json:"id"`
	NoteID    int64  `json:"note_id"`
	UserID    int64  `json:"-"`
	Title     string `json:"title"`
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
	Hash      string `json:"hash"`
	Source    string `json:"source"` // web | api | nextcloud-api | sync | restore | import | capture
	Size      int    `json:"size"`
	CreatedAt string `json:"created_at"`
}

// TrashItem is a deleted note, kept for a while.
type TrashItem struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	NoteID    int64  `json:"note_id"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Category  string `json:"category"`
	Content   string `json:"content,omitempty"`
	Excerpt   string `json:"excerpt"`
	Favorite  bool   `json:"favorite"`
	Pinned    bool   `json:"pinned"`
	Reason    string `json:"reason"` // deleted | gone (removed from the folder outside this app)
	DeletedAt string `json:"deleted_at"`
}

// Share is a public read-only link to a note.
type Share struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	NoteID    int64  `json:"note_id"`
	NoteTitle string `json:"note_title,omitempty"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Views     int    `json:"views"`
	CreatedAt string `json:"created_at"`
}

// Settings are the box's title plus one person's preferences.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	// Writing
	Editor             string `json:"editor"`      // rich | markdown
	FileSuffix         string `json:"file_suffix"` // .md | .txt
	DefaultFolder      string `json:"default_folder"`
	TitleFromFirstLine bool   `json:"title_from_first_line"` // rename the file after the first line, as Nextcloud Notes does
	SortBy             string `json:"sort_by"`               // modified | title | created
	SpellCheck         bool   `json:"spellcheck"`
	DateFormat         string `json:"date_format"` // what @today writes: iso | dmy-dot | long

	// Daily notes and capture
	DailyFolder   string `json:"daily_folder"`
	DailyFormat   string `json:"daily_format"` // Go layout of the daily note's title
	DailyTemplate string `json:"daily_template"`
	CaptureFormat string `json:"capture_format"` // a captured line: {time} {text}

	// Folders
	TemplatesFolder   string `json:"templates_folder"`
	AttachmentsFolder string `json:"attachments_folder"` // relative to the notes root; "" = beside the note (.attachments.<id>)

	// History and trash
	TrashDays    int `json:"trash_days"`
	VersionsKeep int `json:"versions_keep"`

	// Storage
	StorageKind     string `json:"storage_kind"` // builtin | folder | webdav | nextcloud | files
	StoragePath     string `json:"storage_path"` // folder: an absolute path under STORAGE_ROOTS
	StorageURL      string `json:"storage_url"`  // webdav: the folder; nextcloud: the server
	StorageUser     string `json:"storage_user"`
	StoragePassword string `json:"-"`
	StorageFolder   string `json:"storage_folder"`   // nextcloud, files: the folder inside the person's files
	StorageViaCore  bool   `json:"storage_via_core"` // nextcloud: the account Pocket Core holds for this person
	SyncMinutes     int    `json:"sync_minutes"`

	// Notifications (sync trouble)
	NotifyKind  string `json:"notify_kind"`
	NotifyURL   string `json:"notify_url"`
	NotifyToken string `json:"notify_token"`
}

// Dashboard is the compact status used by widgets.
type Dashboard struct {
	Notes       int    `json:"notes"`
	EditedToday int    `json:"edited_today"`
	EditedWeek  int    `json:"edited_week"`
	OpenTasks   int    `json:"open_tasks"`
	DueTasks    int    `json:"due_tasks"`
	Favorites   int    `json:"favorites"`
	Folders     int    `json:"folders"`
	Storage     string `json:"storage"`
	LastSync    string `json:"last_sync"`
	SyncError   string `json:"sync_error,omitempty"`
}
