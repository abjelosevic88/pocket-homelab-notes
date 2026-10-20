package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/notesmd"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// MaxNoteBytes is the largest file read as a note; bigger ones are listed
// read-only without their text.
const MaxNoteBytes = 4 << 20

// library is one person's notes folder: the backend it lives on and the
// state of keeping the index in step with it. mu serialises the sync and
// every write, so the index never races the folder.
type library struct {
	uid     int64
	mu      sync.Mutex
	key     string
	backend storage.Backend

	stateMu   sync.Mutex
	rootETag  string
	lastSync  time.Time
	lastTry   time.Time
	lastErr   string
	failures  int
	notified  bool
	syncing   bool
	dirs      []string
	lastStats SyncStats
}

// SyncStats says what one sync found.
type SyncStats struct {
	Files     int    `json:"files"`
	Added     int    `json:"added"`
	Changed   int    `json:"changed"`
	Moved     int    `json:"moved"`
	Removed   int    `json:"removed"`
	Unchanged bool   `json:"unchanged"`
	At        string `json:"at"`
	Took      string `json:"took"`
}

// conflictError is a save refused because the note changed meanwhile; Note is
// how it stands now.
type conflictError struct{ Note *store.Note }

func (e *conflictError) Error() string { return "the note changed meanwhile" }

func hashOf(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func isNoteFile(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".txt", ".markdown":
		return !storage.Hidden(p)
	}
	return false
}

// inFolder reports whether p lies in folder (or below it); "" is no folder.
func inFolder(p, folder string) bool {
	return folder != "" && (strings.EqualFold(p, folder) || strings.HasPrefix(strings.ToLower(p), strings.ToLower(folder)+"/"))
}

func splitPath(p string) (category, title string) {
	dir, file := path.Split(p)
	return strings.TrimSuffix(dir, "/"), strings.TrimSuffix(file, path.Ext(file))
}

// storageKey fingerprints the storage settings: a change makes a new backend.
func storageKey(st store.Settings) string {
	return strings.Join([]string{st.StorageKind, st.StoragePath, st.StorageURL, st.StorageUser, hashOf(st.StoragePassword), st.StorageFolder, strconv.FormatBool(st.StorageViaCore)}, "\x00")
}

// lib returns a person's library, building its backend from their settings.
func (s *Server) lib(ctx context.Context, uid int64) (*library, error) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return nil, err
	}
	key := storageKey(st)
	s.libsMu.Lock()
	l := s.libs[uid]
	if l == nil {
		l = &library{uid: uid}
		s.libs[uid] = l
	}
	s.libsMu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.backend == nil || l.key != key {
		b, err := s.backendFor(ctx, uid, st)
		if err != nil {
			l.stateMu.Lock()
			l.lastErr = err.Error()
			l.stateMu.Unlock()
			return nil, err
		}
		l.backend, l.key = b, key
		l.stateMu.Lock()
		l.rootETag, l.lastSync, l.lastErr, l.failures, l.notified = "", time.Time{}, "", 0, false
		l.stateMu.Unlock()
	}
	return l, nil
}

// builtinDir is where a person's notes live when they chose nothing else.
func (s *Server) builtinDir(uid int64) string {
	return filepath.Join(s.cfg.DataDir, "notes", strconv.FormatInt(uid, 10))
}

// allowedFolder reports whether p is inside one of STORAGE_ROOTS.
func (s *Server) allowedFolder(p string) bool {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return false
	}
	for _, root := range s.cfg.StorageRoots {
		root = filepath.Clean(root)
		if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// backendFor builds the backend a person's settings describe.
func (s *Server) backendFor(ctx context.Context, uid int64, st store.Settings) (storage.Backend, error) {
	ua := "PocketNotes/" + Version
	switch st.StorageKind {
	case "folder":
		if !s.allowedFolder(st.StoragePath) {
			return nil, fmt.Errorf("the folder %q is not one this server offers (STORAGE_ROOTS)", st.StoragePath)
		}
		if other := s.store.StoragePathOwner(ctx, filepath.Clean(st.StoragePath)); other != 0 && other != uid {
			return nil, errors.New("another account keeps its notes in that folder")
		}
		return storage.NewLocal(st.StoragePath, "folder", "Folder · "+st.StoragePath), nil
	case "webdav":
		user, pass := st.StorageUser, st.StoragePassword
		b, err := storage.NewWebDAV(st.StorageURL, func(context.Context) (string, string, error) { return user, pass, nil }, "WebDAV · "+st.StorageURL)
		if err != nil {
			return nil, err
		}
		b.UserAgent = ua
		return b, nil
	case "nextcloud":
		server, user, folder := st.StorageURL, st.StorageUser, st.StorageFolder
		var creds storage.Credentials
		if st.StorageViaCore {
			email := s.userEmail(ctx, uid)
			cred, err := s.core.ServiceFor(ctx, email, "nextcloud")
			if err != nil {
				return nil, fmt.Errorf("Pocket Core has no Nextcloud account for you: %w", err)
			}
			server, user = cred.URL, cred.Fields["USERNAME"]
			if user == "" {
				return nil, errors.New("your Nextcloud account in Pocket Core has no username")
			}
			creds = func(ctx context.Context) (string, string, error) {
				c, err := s.core.ServiceFor(ctx, email, "nextcloud")
				if err != nil {
					return "", "", err
				}
				pw := c.Fields["APP_PASSWORD"]
				if pw == "" {
					pw = c.Key
				}
				return c.Fields["USERNAME"], pw, nil
			}
		} else {
			pass := st.StoragePassword
			creds = func(context.Context) (string, string, error) { return user, pass, nil }
		}
		if server == "" || user == "" {
			return nil, errors.New("Nextcloud needs its address and your username")
		}
		b, err := storage.NewNextcloud(server, user, folder, creds)
		if err != nil {
			return nil, err
		}
		b.UserAgent = ua
		return b, nil
	default:
		return storage.NewLocal(s.builtinDir(uid), "builtin", "Pocket Notes"), nil
	}
}

func (s *Server) userEmail(ctx context.Context, uid int64) string {
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		return u.Email
	}
	return ""
}

// ---- indexing ----

// fill sets everything the index derives from a note's text and path.
func fill(n *store.Note, content string) (tags, links []string) {
	n.Category, n.Title = splitPath(n.Path)
	n.Content = content
	n.Hash = hashOf(content)
	n.Size = int64(len(content))
	info := notesmd.Analyze(content)
	n.Excerpt, n.Words = info.Excerpt, info.Words
	n.TasksOpen, n.TasksDone = 0, 0
	for _, t := range info.Tasks {
		if t.Done {
			n.TasksDone++
		} else {
			n.TasksOpen++
		}
	}
	n.Tags = info.Tags
	if n.Tags == nil {
		n.Tags = []string{}
	}
	return info.Tags, info.Links
}

func modifiedOf(e storage.Entry) int64 {
	if e.Modified.IsZero() {
		return time.Now().Unix()
	}
	return e.Modified.Unix()
}

func fileIDNum(id string) int64 {
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}

func (s *Server) saveMeta(ctx context.Context, n *store.Note, tags, links []string) {
	if err := s.store.SetNoteMeta(ctx, n.UserID, n.ID, tags, links); err != nil {
		s.log.Warn("save tags", "note", n.ID, "err", err)
	}
}

func (s *Server) addVersion(ctx context.Context, n *store.Note, source string) {
	window := 10 * time.Minute
	if source == "sync" {
		window = 0
	}
	v := &store.Version{NoteID: n.ID, UserID: n.UserID, Title: n.Title, Path: n.Path, Content: n.Content, Hash: n.Hash, Source: source}
	if err := s.store.AddVersion(ctx, v, window); err != nil {
		s.log.Warn("save version", "note", n.ID, "err", err)
	}
}

// ---- sync ----

func (l *library) status() map[string]any {
	l.stateMu.Lock()
	defer l.stateMu.Unlock()
	out := map[string]any{"syncing": l.syncing, "error": l.lastErr, "failures": l.failures, "stats": l.lastStats}
	if !l.lastSync.IsZero() {
		out["last_sync"] = l.lastSync.UTC().Format(time.RFC3339)
	}
	if l.backend != nil {
		out["kind"], out["where"] = l.backend.Kind(), l.backend.Describe()
	}
	return out
}

// fresh syncs when the last look is older than maxAge; errors are logged and
// the index is served as it is.
func (s *Server) fresh(ctx context.Context, uid int64, maxAge time.Duration) {
	l, err := s.lib(ctx, uid)
	if err != nil {
		return
	}
	l.stateMu.Lock()
	recent := time.Since(l.lastTry) < maxAge
	l.stateMu.Unlock()
	if recent {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := s.syncUser(cctx, uid, false); err != nil {
		s.log.Debug("sync before answering", "user", uid, "err", err)
	}
}

// syncUser brings a person's index in step with their folder: new files are
// added, changed ones re-read (each change kept as a version), moved ones
// followed (by Nextcloud's file id, else by identical content) and vanished
// ones moved to the trash with their last text.
func (s *Server) syncUser(ctx context.Context, uid int64, force bool) (SyncStats, error) {
	l, err := s.lib(ctx, uid)
	if err != nil {
		return SyncStats{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stateMu.Lock()
	l.syncing, l.lastTry = true, time.Now()
	l.stateMu.Unlock()
	start := time.Now()
	stats, err := s.syncLocked(ctx, l, force)
	stats.At, stats.Took = time.Now().UTC().Format(time.RFC3339), time.Since(start).Round(time.Millisecond).String()
	l.stateMu.Lock()
	l.syncing = false
	if err != nil {
		l.lastErr = err.Error()
		l.failures++
	} else {
		l.lastErr, l.failures, l.lastSync, l.lastStats = "", 0, time.Now(), stats
	}
	failures, notified := l.failures, l.notified
	if err == nil {
		l.notified = false
	} else if failures >= 5 && !notified {
		l.notified = true
	}
	l.stateMu.Unlock()
	if err != nil && failures >= 5 && !notified {
		s.notifyUser(uid, "Pocket Notes can't reach your notes", "Syncing your notes folder has failed "+strconv.Itoa(failures)+" times in a row: "+err.Error())
	}
	if err == nil && (stats.Added+stats.Changed+stats.Moved+stats.Removed) > 0 {
		s.log.Info("notes synced", "user", uid, "files", stats.Files, "added", stats.Added, "changed", stats.Changed, "moved", stats.Moved, "removed", stats.Removed, "took", stats.Took)
	}
	return stats, err
}

func (s *Server) syncLocked(ctx context.Context, l *library, force bool) (SyncStats, error) {
	var stats SyncStats
	b := l.backend
	var rootTag string
	if rt, ok := b.(storage.RootTagger); ok {
		tag, err := rt.RootETag(ctx)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return stats, err
		}
		rootTag = tag
		l.stateMu.Lock()
		same := !force && tag != "" && tag == l.rootETag
		l.stateMu.Unlock()
		if same {
			stats.Unchanged = true
			return stats, nil
		}
	}
	entries, err := b.List(ctx)
	if err != nil {
		return stats, err
	}
	rows, err := s.store.SyncRows(ctx, l.uid)
	if err != nil {
		return stats, err
	}
	// A .md or .txt file copied into a note (the attachments folder) is a file
	// of that note, not a note of its own.
	st, _ := s.store.GetSettings(ctx, l.uid)
	attachments := notesmd.SafeFolder(st.AttachmentsFolder)
	var files []storage.Entry
	var dirs []string
	for _, e := range entries {
		if e.IsDir {
			dirs = append(dirs, e.Path)
		} else if isNoteFile(e.Path) && !inFolder(e.Path, attachments) {
			files = append(files, e)
		}
	}
	stats.Files = len(files)
	if len(files) == 0 && len(rows) > 3 {
		return stats, fmt.Errorf("the notes folder (%s) answered with no notes at all while %d are known; nothing was removed — check the storage settings", b.Describe(), len(rows))
	}
	first := len(rows) == 0
	seen := map[string]bool{}
	var fresh []storage.Entry
	_, canFav := b.(storage.Favoriter)
	for _, e := range files {
		r := rows[e.Path]
		if r == nil {
			fresh = append(fresh, e)
			continue
		}
		seen[e.Path] = true
		if canFav && e.Favorite != r.Favorite {
			_ = s.store.SetFlag(ctx, l.uid, r.ID, "favorite", e.Favorite)
		}
		if r.RemoteETag != "" && r.RemoteETag == e.ETag {
			continue
		}
		if e.Size > MaxNoteBytes {
			_ = s.store.SetRemote(ctx, l.uid, r.ID, e.ETag, e.FileID, modifiedOf(e))
			continue
		}
		data, ent, err := b.Read(ctx, e.Path)
		if errors.Is(err, storage.ErrNotFound) {
			delete(seen, e.Path) // gone between the listing and now
			continue
		}
		if err != nil {
			return stats, err
		}
		if ent.ETag == "" {
			ent.ETag = e.ETag
		}
		if hashOf(string(data)) == r.Hash {
			_ = s.store.SetRemote(ctx, l.uid, r.ID, firstNonEmpty(e.ETag, ent.ETag), e.FileID, 0)
			continue
		}
		n, err := s.store.GetNote(ctx, l.uid, r.ID)
		if err != nil {
			return stats, err
		}
		tags, links := fill(n, string(data))
		n.RemoteETag, n.Modified = firstNonEmpty(e.ETag, ent.ETag), modifiedOf(e)
		if e.FileID != "" {
			n.FileID = e.FileID
		}
		if err := s.store.UpdateNote(ctx, n); err != nil {
			return stats, err
		}
		s.saveMeta(ctx, n, tags, links)
		s.addVersion(ctx, n, "sync")
		stats.Changed++
	}
	gone := map[string]*store.SyncRow{}
	for p, r := range rows {
		if !seen[p] {
			gone[p] = r
		}
	}
	for _, e := range fresh {
		var data []byte
		var ent storage.Entry
		readonly := e.Size > MaxNoteBytes
		if !readonly {
			data, ent, err = b.Read(ctx, e.Path)
			if errors.Is(err, storage.ErrNotFound) {
				continue
			}
			if err != nil {
				return stats, err
			}
		}
		content := string(data)
		h := hashOf(content)
		// A file that moved: same Nextcloud id, or (no ids) the only vanished note with the same text.
		var moved *store.SyncRow
		if e.FileID != "" {
			for p, r := range gone {
				if r.FileID == e.FileID {
					moved = r
					delete(gone, p)
					break
				}
			}
		}
		if moved == nil && e.FileID == "" && !readonly {
			var cands []string
			for p, r := range gone {
				if r.Hash == h && r.FileID == "" {
					cands = append(cands, p)
				}
			}
			if len(cands) == 1 {
				moved = gone[cands[0]]
				delete(gone, cands[0])
			}
		}
		if moved != nil {
			n, err := s.store.GetNote(ctx, l.uid, moved.ID)
			if err != nil {
				return stats, err
			}
			n.Path = e.Path
			var tags, links []string
			if readonly {
				n.Category, n.Title = splitPath(e.Path)
			} else {
				tags, links = fill(n, content)
			}
			n.RemoteETag, n.Modified = firstNonEmpty(e.ETag, ent.ETag), modifiedOf(e)
			if e.FileID != "" {
				n.FileID = e.FileID
			}
			if err := s.store.UpdateNote(ctx, n); err != nil {
				return stats, err
			}
			if !readonly {
				s.saveMeta(ctx, n, tags, links)
			}
			if !readonly && h != moved.Hash {
				s.addVersion(ctx, n, "sync")
			}
			stats.Moved++
			continue
		}
		n := &store.Note{UserID: l.uid, Path: e.Path, Favorite: e.Favorite, Readonly: readonly}
		tags, links := fill(n, content)
		if readonly {
			n.Content, n.Hash, n.Size = "", "", e.Size
			n.Excerpt = "This file is too large to open as a note."
			tags, links = nil, nil
		}
		n.RemoteETag, n.FileID, n.Modified = firstNonEmpty(e.ETag, ent.ETag), e.FileID, modifiedOf(e)
		n.CreatedAt = time.Unix(n.Modified, 0).UTC().Format(time.RFC3339Nano)
		if err := s.store.InsertNote(ctx, n, fileIDNum(e.FileID)); err != nil {
			if errors.Is(err, store.ErrConflict) {
				continue // two files differing only in case: the first one wins
			}
			return stats, err
		}
		s.saveMeta(ctx, n, tags, links)
		if !readonly {
			src := "sync"
			if first {
				src = "import"
			}
			s.addVersion(ctx, n, src)
		}
		stats.Added++
	}
	for _, r := range gone {
		n, err := s.store.GetNote(ctx, l.uid, r.ID)
		if err != nil {
			continue
		}
		// One indexed before attachments were left out is still there: no trash.
		if !inFolder(n.Path, attachments) {
			_ = s.store.AddTrash(ctx, &store.TrashItem{UserID: l.uid, NoteID: n.ID, Path: n.Path, Title: n.Title, Category: n.Category, Content: n.Content, Favorite: n.Favorite, Pinned: n.Pinned, Reason: "gone"})
		}
		if err := s.store.DeleteNote(ctx, l.uid, n.ID); err != nil {
			return stats, err
		}
		stats.Removed++
	}
	l.stateMu.Lock()
	l.rootETag, l.dirs = rootTag, dirs
	l.stateMu.Unlock()
	return stats, nil
}

func (s *Server) notifyUser(uid int64, title, body string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		st, _ := s.store.GetSettings(ctx, uid)
		kind, u, tok := st.NotifyKind, st.NotifyURL, st.NotifyToken
		if u == "" && s.cfg.NotifyURL != "" && s.isOwner(ctx, uid) {
			kind, u = s.cfg.NotifyKind, s.cfg.NotifyURL
		}
		if u == "" {
			return
		}
		if err := notify.Send(ctx, kind, u, tok, title, body, s.cfg.BaseURL); err != nil {
			s.log.Warn("notify", "err", err)
		}
	}()
}

// syncLoop keeps every person's index in step: on start, then each person
// at their own interval.
func (s *Server) syncLoop(ctx context.Context) {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	run := func() {
		for _, uid := range s.store.UserIDs(ctx) {
			if ctx.Err() != nil {
				return
			}
			st, err := s.store.GetSettings(ctx, uid)
			if err != nil {
				continue
			}
			l, err := s.lib(ctx, uid)
			if err != nil {
				continue
			}
			l.stateMu.Lock()
			due := time.Since(l.lastTry) >= time.Duration(st.SyncMinutes)*time.Minute
			// Back off while it keeps failing: 1, 2, 4 … up to 30 minutes.
			if l.failures > 0 {
				back := time.Duration(1<<min(l.failures, 5)) * time.Minute
				due = time.Since(l.lastTry) >= min(back, 30*time.Minute)
			}
			l.stateMu.Unlock()
			if !due {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			if _, err := s.syncUser(cctx, uid, false); err != nil {
				s.log.Warn("sync", "user", uid, "err", err)
			}
			cancel()
		}
	}
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			run()
		}
	}
}

// ---- writing ----

// NoteInput is what a client may set on a note.
type NoteInput struct {
	Title    *string `json:"title"`
	Category *string `json:"category"`
	Content  *string `json:"content"`
	Favorite *bool   `json:"favorite"`
	Pinned   *bool   `json:"pinned"`
	Modified *int64  `json:"modified"`
}

// uniquePath finds a free file name for title in category: "Title.md",
// "Title (2).md" … skipping the note's own path.
func (s *Server) uniquePath(ctx context.Context, uid int64, category, title, suffix, self string) string {
	for i := 1; i < 1000; i++ {
		name := title
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", title, i)
		}
		p := name + suffix
		if category != "" {
			p = category + "/" + p
		}
		if strings.EqualFold(p, self) || !s.store.PathTaken(ctx, uid, p) {
			return p
		}
	}
	return path.Join(category, title+" "+store.RandomToken(3)+suffix)
}

// createNote writes a new note file and indexes it.
func (s *Server) createNote(ctx context.Context, uid int64, in NoteInput, source string) (*store.Note, error) {
	l, err := s.lib(ctx, uid)
	if err != nil {
		return nil, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	content := ""
	if in.Content != nil {
		content = *in.Content
	}
	title := ""
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	if title == "" {
		title = notesmd.TitleFromContent(content)
	}
	if title == "" {
		title = "New note"
	}
	title = notesmd.SafeName(title)
	category := st.DefaultFolder
	if in.Category != nil {
		category = *in.Category
	}
	category = notesmd.SafeFolder(category)
	l.mu.Lock()
	defer l.mu.Unlock()
	var p string
	var ent storage.Entry
	for attempt := 0; attempt < 20; attempt++ {
		p = s.uniquePath(ctx, uid, category, title, st.FileSuffix, "")
		if attempt > 0 {
			p = s.uniquePath(ctx, uid, category, fmt.Sprintf("%s (%d)", title, attempt+1), st.FileSuffix, "")
		}
		ent, err = l.backend.Create(ctx, p, []byte(content))
		if !errors.Is(err, storage.ErrExists) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	n := &store.Note{UserID: uid, Path: p, RemoteETag: ent.ETag, FileID: ent.FileID, Modified: modifiedOf(ent)}
	if in.Modified != nil && *in.Modified > 0 {
		n.Modified = *in.Modified
	}
	tags, links := fill(n, content)
	if in.Favorite != nil {
		n.Favorite = *in.Favorite
	}
	if in.Pinned != nil {
		n.Pinned = *in.Pinned
	}
	if err := s.store.InsertNote(ctx, n, fileIDNum(ent.FileID)); err != nil {
		return nil, err
	}
	s.saveMeta(ctx, n, tags, links)
	s.addVersion(ctx, n, source)
	if n.Favorite {
		if f, ok := l.backend.(storage.Favoriter); ok {
			_ = f.SetFavorite(ctx, p, true)
		}
	}
	s.publish(uid, "notes.created", strconv.FormatInt(n.ID, 10), map[string]any{"id": n.ID, "title": n.Title, "category": n.Category, "source": source})
	return n, nil
}

// updateNote changes a note. ifMatch, when given, must be the note's current
// ETag (its hash). A file changed outside since it was last read is not
// overwritten: the save answers conflictError with the note as it is now.
func (s *Server) updateNote(ctx context.Context, uid, id int64, in NoteInput, ifMatch, source string) (*store.Note, error) {
	l, err := s.lib(ctx, uid)
	if err != nil {
		return nil, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	if ifMatch != "" && strings.Trim(ifMatch, `"`) != n.Hash {
		return nil, &conflictError{Note: n}
	}
	b := l.backend
	contentChanged := in.Content != nil && *in.Content != n.Content
	if contentChanged && n.Readonly {
		return nil, errors.New("this note is read-only")
	}
	// The title follows the first line, as in Nextcloud Notes, when the
	// person wants that and this save did not name a title itself.
	if contentChanged && in.Title == nil && st.TitleFromFirstLine && (source == "web") {
		if t := notesmd.TitleFromContent(*in.Content); t != "" && notesmd.SafeName(t) != n.Title {
			in.Title = &t
		}
	}
	var tags, links []string
	if contentChanged {
		data := []byte(*in.Content)
		ent, err := b.Write(ctx, n.Path, data, n.RemoteETag)
		if errors.Is(err, storage.ErrPrecondition) || errors.Is(err, storage.ErrNotFound) {
			cur, curEnt, rerr := b.Read(ctx, n.Path)
			switch {
			case rerr == nil && hashOf(string(cur)) == n.Hash:
				// Only the storage's tag moved (a touch, a sync client): ours is still the latest text.
				ent, err = b.Write(ctx, n.Path, data, curEnt.ETag)
			case rerr == nil:
				// Someone else changed it: index their text and let the person decide.
				t2, l2 := fill(n, string(cur))
				n.RemoteETag, n.Modified = curEnt.ETag, modifiedOf(curEnt)
				if uerr := s.store.UpdateNote(ctx, n); uerr == nil {
					s.saveMeta(ctx, n, t2, l2)
					s.addVersion(ctx, n, "sync")
				}
				return nil, &conflictError{Note: n}
			case errors.Is(rerr, storage.ErrNotFound):
				// The file went away meanwhile; what is being written brings it back.
				ent, err = b.Create(ctx, n.Path, data)
			default:
				err = rerr
			}
		}
		if err != nil {
			return nil, err
		}
		tags, links = fill(n, *in.Content)
		n.RemoteETag, n.Modified = ent.ETag, modifiedOf(ent)
		if ent.FileID != "" {
			n.FileID = ent.FileID
		}
	}
	newTitle, newCat := n.Title, n.Category
	if in.Title != nil {
		if t := strings.TrimSpace(*in.Title); t != "" {
			newTitle = notesmd.SafeName(t)
		}
	}
	if in.Category != nil {
		newCat = notesmd.SafeFolder(*in.Category)
	}
	if newTitle != n.Title || newCat != n.Category {
		suffix := path.Ext(n.Path)
		target := s.uniquePath(ctx, uid, newCat, newTitle, suffix, n.Path)
		if target != n.Path {
			ent, err := b.Move(ctx, n.Path, target)
			for i := 2; errors.Is(err, storage.ErrExists) && i < 20; i++ {
				target = s.uniquePath(ctx, uid, newCat, fmt.Sprintf("%s (%d)", newTitle, i), suffix, n.Path)
				ent, err = b.Move(ctx, n.Path, target)
			}
			if err != nil {
				return nil, err
			}
			n.Path = target
			n.Category, n.Title = splitPath(target)
			n.RemoteETag = ent.ETag
			if ent.FileID != "" {
				n.FileID = ent.FileID
			}
		}
	}
	if in.Favorite != nil && *in.Favorite != n.Favorite {
		n.Favorite = *in.Favorite
		if f, ok := b.(storage.Favoriter); ok {
			if err := f.SetFavorite(ctx, n.Path, n.Favorite); err != nil {
				s.log.Warn("favourite in storage", "note", n.ID, "err", err)
			}
		}
	}
	if in.Pinned != nil {
		n.Pinned = *in.Pinned
	}
	if in.Modified != nil && *in.Modified > 0 {
		n.Modified = *in.Modified
	}
	if err := s.store.UpdateNote(ctx, n); err != nil {
		return nil, err
	}
	if contentChanged {
		s.saveMeta(ctx, n, tags, links)
		s.addVersion(ctx, n, source)
	} else {
		n.Tags = notesmd.Analyze(n.Content).Tags
		if n.Tags == nil {
			n.Tags = []string{}
		}
	}
	s.publish(uid, "notes.updated", strconv.FormatInt(n.ID, 10), map[string]any{"id": n.ID, "title": n.Title, "category": n.Category, "source": source})
	return n, nil
}

// deleteNote removes a note's file and keeps its text in the trash.
func (s *Server) deleteNote(ctx context.Context, uid, id int64, source string) error {
	l, err := s.lib(ctx, uid)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := s.store.GetNote(ctx, uid, id)
	if err != nil {
		return err
	}
	if err := l.backend.Delete(ctx, n.Path); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	_ = s.store.AddTrash(ctx, &store.TrashItem{UserID: uid, NoteID: n.ID, Path: n.Path, Title: n.Title, Category: n.Category, Content: n.Content, Favorite: n.Favorite, Pinned: n.Pinned, Reason: "deleted"})
	if err := s.store.DeleteNote(ctx, uid, id); err != nil {
		return err
	}
	s.publish(uid, "notes.deleted", strconv.FormatInt(id, 10), map[string]any{"id": id, "title": n.Title, "category": n.Category, "source": source})
	return nil
}

// restoreTrash writes a deleted note back where it was (beside it when the
// name is taken) and indexes it again, under its old id when that is free.
func (s *Server) restoreTrash(ctx context.Context, uid, trashID int64) (*store.Note, error) {
	t, err := s.store.GetTrash(ctx, uid, trashID)
	if err != nil {
		return nil, err
	}
	l, err := s.lib(ctx, uid)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	suffix := path.Ext(t.Path)
	if suffix == "" {
		suffix = ".md"
	}
	var p string
	var ent storage.Entry
	for i := 1; i < 20; i++ {
		title := t.Title
		if i > 1 {
			title = fmt.Sprintf("%s (%d)", t.Title, i)
		}
		p = s.uniquePath(ctx, uid, t.Category, title, suffix, "")
		ent, err = l.backend.Create(ctx, p, []byte(t.Content))
		if !errors.Is(err, storage.ErrExists) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	n := &store.Note{UserID: uid, Path: p, RemoteETag: ent.ETag, FileID: ent.FileID, Modified: modifiedOf(ent), Favorite: t.Favorite, Pinned: t.Pinned}
	tags, links := fill(n, t.Content)
	prefer := fileIDNum(ent.FileID)
	if prefer == 0 {
		prefer = t.NoteID
	}
	if err := s.store.InsertNote(ctx, n, prefer); err != nil {
		return nil, err
	}
	s.saveMeta(ctx, n, tags, links)
	s.addVersion(ctx, n, "restore")
	_ = s.store.DeleteTrash(ctx, uid, trashID, false)
	return n, nil
}

// ---- folders ----

// folders lists the person's folders: the ones with notes and the empty ones
// the last sync saw.
func (s *Server) folders(ctx context.Context, uid int64) ([]store.Folder, error) {
	list, err := s.store.Folders(ctx, uid)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, f := range list {
		have[f.Path] = true
	}
	// Parents of folders with notes are folders too.
	for _, f := range list {
		for p := parentOf(f.Path); p != ""; p = parentOf(p) {
			if !have[p] {
				have[p] = true
				list = append(list, store.Folder{Path: p})
			}
		}
	}
	if l, err := s.lib(ctx, uid); err == nil {
		l.stateMu.Lock()
		for _, d := range l.dirs {
			if !have[d] && !storage.Hidden(d) {
				have[d] = true
				list = append(list, store.Folder{Path: d})
			}
		}
		l.stateMu.Unlock()
	}
	return list, nil
}

func parentOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// renameFolder moves a folder (with everything in it) and rewrites the index.
func (s *Server) renameFolder(ctx context.Context, uid int64, from, to string) error {
	from, to = notesmd.SafeFolder(from), notesmd.SafeFolder(to)
	if from == "" || to == "" {
		return errors.New("a folder name is needed")
	}
	if from == to {
		return nil
	}
	if strings.HasPrefix(to+"/", from+"/") {
		return errors.New("a folder cannot move into itself")
	}
	l, err := s.lib(ctx, uid)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.backend.Move(ctx, from, to); err != nil {
		return err
	}
	cat := from
	notes, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Category: &cat, Recursive: true, Content: true})
	if err != nil {
		return err
	}
	for i := range notes {
		n := &notes[i]
		n.Path = to + strings.TrimPrefix(n.Path, from)
		n.Category, n.Title = splitPath(n.Path)
		if err := s.store.UpdateNote(ctx, n); err != nil {
			return err
		}
	}
	l.stateMu.Lock()
	for i, d := range l.dirs {
		if d == from || strings.HasPrefix(d, from+"/") {
			l.dirs[i] = to + strings.TrimPrefix(d, from)
		}
	}
	l.rootETag = ""
	l.stateMu.Unlock()
	return nil
}

// deleteFolder moves a folder's notes to the trash and removes the folder.
func (s *Server) deleteFolder(ctx context.Context, uid int64, folder string) error {
	folder = notesmd.SafeFolder(folder)
	if folder == "" {
		return errors.New("the notes folder itself cannot be deleted")
	}
	l, err := s.lib(ctx, uid)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cat := folder
	notes, err := s.store.ListNotes(ctx, uid, store.NoteFilter{Category: &cat, Recursive: true, Content: true})
	if err != nil {
		return err
	}
	if err := l.backend.Delete(ctx, folder); err != nil {
		return err
	}
	for _, n := range notes {
		_ = s.store.AddTrash(ctx, &store.TrashItem{UserID: uid, NoteID: n.ID, Path: n.Path, Title: n.Title, Category: n.Category, Content: n.Content, Favorite: n.Favorite, Pinned: n.Pinned, Reason: "deleted"})
		_ = s.store.DeleteNote(ctx, uid, n.ID)
	}
	l.stateMu.Lock()
	kept := l.dirs[:0]
	for _, d := range l.dirs {
		if d != folder && !strings.HasPrefix(d, folder+"/") {
			kept = append(kept, d)
		}
	}
	l.dirs, l.rootETag = kept, ""
	l.stateMu.Unlock()
	return nil
}

// makeFolder creates an empty folder.
func (s *Server) makeFolder(ctx context.Context, uid int64, folder string) (string, error) {
	folder = notesmd.SafeFolder(folder)
	if folder == "" {
		return "", errors.New("a folder name is needed")
	}
	l, err := s.lib(ctx, uid)
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.backend.Mkdir(ctx, folder); err != nil {
		return "", err
	}
	l.stateMu.Lock()
	for p := folder; p != ""; p = parentOf(p) {
		found := false
		for _, d := range l.dirs {
			if d == p {
				found = true
				break
			}
		}
		if !found {
			l.dirs = append(l.dirs, p)
		}
	}
	l.stateMu.Unlock()
	return folder, nil
}
