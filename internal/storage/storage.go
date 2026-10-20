// Package storage is where a person's notes live: plain Markdown files in a
// folder tree, on this server or on a WebDAV server such as Nextcloud. The
// database only indexes them; the files stay the truth, so any other app that
// reads the folder (Nextcloud Notes, Obsidian, a text editor) sees the same notes.
package storage

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Errors every backend maps its own failures onto.
var (
	ErrNotFound     = errors.New("storage: not found")
	ErrExists       = errors.New("storage: already exists")
	ErrPrecondition = errors.New("storage: changed meanwhile")
	ErrAuth         = errors.New("storage: sign-in refused")
	ErrLocked       = errors.New("storage: file is locked")
	ErrInvalidPath  = errors.New("storage: invalid path")
)

// Entry is one file or folder, its path relative to the notes root.
type Entry struct {
	Path     string    `json:"path"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// ETag changes whenever the file does. Opaque, compared for equality only.
	ETag string `json:"etag"`
	// FileID is the server's own id for the file (Nextcloud's oc:fileid), or "".
	FileID string `json:"file_id,omitempty"`
	// Favorite is Nextcloud's favourite mark (what Nextcloud Notes shows as a star).
	Favorite bool `json:"favorite,omitempty"`
}

// Backend is one storage location.
type Backend interface {
	// Kind names the backend: builtin | folder | webdav | nextcloud.
	Kind() string
	// Describe says where the notes are, for people ("Nextcloud · Notes").
	Describe() string
	// List returns every file and folder under the root, hidden ones (".x") left out.
	List(ctx context.Context) ([]Entry, error)
	// Stat returns one entry.
	Stat(ctx context.Context, p string) (Entry, error)
	// Read returns a file's bytes and its entry.
	Read(ctx context.Context, p string) ([]byte, Entry, error)
	// Write replaces a file when its ETag is still ifMatch ("" = whatever is
	// there), creating folders on the way. ErrPrecondition when it changed.
	Write(ctx context.Context, p string, data []byte, ifMatch string) (Entry, error)
	// Create writes a new file; ErrExists when the name is taken.
	Create(ctx context.Context, p string, data []byte) (Entry, error)
	// Move renames a file or folder; ErrExists when the target is taken.
	Move(ctx context.Context, from, to string) (Entry, error)
	// Delete removes a file or an empty-or-not folder. A missing one is fine.
	Delete(ctx context.Context, p string) error
	// Mkdir makes a folder and its parents.
	Mkdir(ctx context.Context, p string) error
	// Test checks that the root can be read (and made, when missing).
	Test(ctx context.Context) error
}

// RootTagger is a backend that can tell cheaply whether anything below the
// root changed (Nextcloud propagates ETags upwards).
type RootTagger interface {
	RootETag(ctx context.Context) (string, error)
}

// Dater is a backend that can make a file with a given modification time, so
// notes copied to it keep their "edited" dates.
type Dater interface {
	CreateDated(ctx context.Context, p string, data []byte, modified time.Time) (Entry, error)
}

// Favoriter is a backend that keeps a favourite mark on files (Nextcloud).
type Favoriter interface {
	SetFavorite(ctx context.Context, p string, on bool) error
}

// Clean normalises a relative path: forward slashes, no leading slash, no
// "." or ".." parts. An empty path is the root.
func Clean(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.ContainsRune(p, 0) {
		return "", ErrInvalidPath
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", ErrInvalidPath
		}
		out = append(out, part)
	}
	return strings.Join(out, "/"), nil
}

// Hidden reports whether a path has a part starting with a dot.
func Hidden(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// Resolve reads a link written in a note (relative to the note's folder) and
// returns the path it points at inside the notes root, or ErrInvalidPath when
// it would leave the root.
func Resolve(noteDir, link string) (string, error) {
	link = strings.TrimSpace(link)
	if link == "" || strings.HasPrefix(link, "/") || strings.Contains(link, "://") {
		return "", ErrInvalidPath
	}
	if u, err := url.PathUnescape(link); err == nil {
		link = u
	}
	if i := strings.IndexAny(link, "?#"); i >= 0 {
		link = link[:i]
	}
	joined := path.Join(noteDir, link)
	if joined == ".." || strings.HasPrefix(joined, "../") || joined == "." {
		return "", ErrInvalidPath
	}
	return Clean(joined)
}

// Rel returns the path of target as seen from the folder dir, for a link in a
// note: "../attachments/x.png".
func Rel(dir, target string) string {
	d := splitNonEmpty(dir)
	t := splitNonEmpty(target)
	i := 0
	for i < len(d) && i < len(t)-1 && d[i] == t[i] {
		i++
	}
	var parts []string
	for j := i; j < len(d); j++ {
		parts = append(parts, "..")
	}
	parts = append(parts, t[i:]...)
	return strings.Join(parts, "/")
}

func splitNonEmpty(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Copy copies every file from one backend to another, leaving files that are
// already there with the same bytes alone. A file that is there with other
// bytes is written next to it as "name (copy).ext". It returns how many files
// were copied and how many were kept as copies.
func Copy(ctx context.Context, from, to Backend, progress func(done, total int)) (copied, renamed int, err error) {
	entries, err := from.List(ctx)
	if err != nil {
		return 0, 0, err
	}
	var files []Entry
	for _, e := range entries {
		if e.IsDir {
			if err := to.Mkdir(ctx, e.Path); err != nil {
				return copied, renamed, err
			}
			continue
		}
		files = append(files, e)
	}
	for i, e := range files {
		if progress != nil {
			progress(i, len(files))
		}
		data, _, err := from.Read(ctx, e.Path)
		if err != nil {
			return copied, renamed, err
		}
		create := func(p string) (Entry, error) {
			if d, ok := to.(Dater); ok && !e.Modified.IsZero() {
				return d.CreateDated(ctx, p, data, e.Modified)
			}
			return to.Create(ctx, p, data)
		}
		// A name already taken there: the same file is left alone, a different
		// one is kept beside it as "name (copy).md", "name (copy 2).md"…
		same, kept := false, false
		ext := path.Ext(e.Path)
		for i := 0; i < 50; i++ {
			p := e.Path
			if i == 1 {
				p = strings.TrimSuffix(e.Path, ext) + " (copy)" + ext
			} else if i > 1 {
				p = strings.TrimSuffix(e.Path, ext) + " (copy " + strconv.Itoa(i) + ")" + ext
			}
			if _, err = create(p); !errors.Is(err, ErrExists) {
				kept = i > 0
				break
			}
			if old, _, rerr := to.Read(ctx, p); rerr == nil && string(old) == string(data) {
				same, err = true, nil
				break
			}
		}
		if err != nil {
			return copied, renamed, err
		}
		if same {
			continue
		}
		if kept {
			renamed++
		}
		copied++
	}
	return copied, renamed, nil
}
