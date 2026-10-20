package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Local keeps notes in a folder on this server: the built-in place under
// DATA_DIR, or a folder the box's owner made available (a NAS mount, a
// Syncthing folder).
type Local struct {
	Root  string
	kind  string
	label string
}

// NewLocal returns a backend rooted at dir. kind is "builtin" or "folder".
func NewLocal(dir, kind, label string) *Local {
	return &Local{Root: filepath.Clean(dir), kind: kind, label: label}
}

func (l *Local) Kind() string     { return l.kind }
func (l *Local) Describe() string { return l.label }

func (l *Local) abs(p string) (string, error) {
	c, err := Clean(p)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.Root, filepath.FromSlash(c)), nil
}

func localETag(fi fs.FileInfo) string {
	return fmt.Sprintf("%x-%x", fi.ModTime().UnixNano(), fi.Size())
}

func localEntry(rel string, fi fs.FileInfo) Entry {
	e := Entry{Path: rel, IsDir: fi.IsDir(), Modified: fi.ModTime().UTC()}
	if !fi.IsDir() {
		e.Size = fi.Size()
		e.ETag = localETag(fi)
	}
	return e
}

func (l *Local) Test(ctx context.Context) error {
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(l.Root, ".pocket-notes-test-*")
	if err != nil {
		return fmt.Errorf("the folder cannot be written: %w", err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func (l *Local) List(ctx context.Context) ([]Entry, error) {
	if _, err := os.Stat(l.Root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, err
	}
	var out []Entry
	err := filepath.WalkDir(l.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == l.Root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return nil // vanished meanwhile
		}
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(l.Root, p)
		out = append(out, localEntry(filepath.ToSlash(rel), fi))
		return nil
	})
	return out, err
}

func (l *Local) Stat(ctx context.Context, p string) (Entry, error) {
	a, err := l.abs(p)
	if err != nil {
		return Entry{}, err
	}
	fi, err := os.Stat(a)
	if err != nil {
		return Entry{}, mapOS(err)
	}
	c, _ := Clean(p)
	return localEntry(c, fi), nil
}

func (l *Local) Read(ctx context.Context, p string) ([]byte, Entry, error) {
	a, err := l.abs(p)
	if err != nil {
		return nil, Entry{}, err
	}
	data, err := os.ReadFile(a)
	if err != nil {
		return nil, Entry{}, mapOS(err)
	}
	e, err := l.Stat(ctx, p)
	return data, e, err
}

// writeAtomic writes through a temporary file and a rename, so a reader never
// sees half a note.
func writeAtomic(a string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(a), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a), ".pn-tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Chmod(tmp, 0o644)
	if err := os.Rename(tmp, a); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (l *Local) Write(ctx context.Context, p string, data []byte, ifMatch string) (Entry, error) {
	a, err := l.abs(p)
	if err != nil {
		return Entry{}, err
	}
	if ifMatch != "" {
		fi, err := os.Stat(a)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return Entry{}, ErrPrecondition
			}
			return Entry{}, err
		}
		if localETag(fi) != ifMatch {
			return Entry{}, ErrPrecondition
		}
	}
	if err := writeAtomic(a, data); err != nil {
		return Entry{}, err
	}
	return l.Stat(ctx, p)
}

func (l *Local) Create(ctx context.Context, p string, data []byte) (Entry, error) {
	a, err := l.abs(p)
	if err != nil {
		return Entry{}, err
	}
	if _, err := os.Lstat(a); err == nil {
		return Entry{}, ErrExists
	}
	if err := os.MkdirAll(filepath.Dir(a), 0o755); err != nil {
		return Entry{}, err
	}
	// O_EXCL claims the name; the bytes then go in through a rename.
	f, err := os.OpenFile(a, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Entry{}, ErrExists
		}
		return Entry{}, err
	}
	f.Close()
	if err := writeAtomic(a, data); err != nil {
		os.Remove(a)
		return Entry{}, err
	}
	return l.Stat(ctx, p)
}

// CreateDated is Create with the file's modification time set to modified.
func (l *Local) CreateDated(ctx context.Context, p string, data []byte, modified time.Time) (Entry, error) {
	if _, err := l.Create(ctx, p, data); err != nil {
		return Entry{}, err
	}
	if a, err := l.abs(p); err == nil {
		_ = os.Chtimes(a, modified, modified)
	}
	return l.Stat(ctx, p)
}

func (l *Local) Move(ctx context.Context, from, to string) (Entry, error) {
	af, err := l.abs(from)
	if err != nil {
		return Entry{}, err
	}
	at, err := l.abs(to)
	if err != nil {
		return Entry{}, err
	}
	if af == at {
		return l.Stat(ctx, to)
	}
	// A rename that only changes letter case is the same file on some filesystems.
	if _, err := os.Lstat(at); err == nil && !strings.EqualFold(af, at) {
		return Entry{}, ErrExists
	}
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(af, at); err != nil {
		return Entry{}, mapOS(err)
	}
	return l.Stat(ctx, to)
}

func (l *Local) Delete(ctx context.Context, p string) error {
	a, err := l.abs(p)
	if err != nil {
		return err
	}
	if a == l.Root {
		return ErrInvalidPath
	}
	if err := os.RemoveAll(a); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) Mkdir(ctx context.Context, p string) error {
	a, err := l.abs(p)
	if err != nil {
		return err
	}
	return os.MkdirAll(a, 0o755)
}

func mapOS(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	}
	return err
}
