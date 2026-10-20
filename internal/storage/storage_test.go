package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"golang.org/x/net/webdav"
)

func TestCleanAndResolve(t *testing.T) {
	for in, want := range map[string]string{"a/b.md": "a/b.md", "/a//b.md": "a/b.md", "./a/./b": "a/b", "": ""} {
		if got, err := Clean(in); err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := Clean("a/../../b"); err == nil {
		t.Error("Clean let .. through")
	}
	if got, err := Resolve("Daily", "../attachments/x.jpg"); err != nil || got != "attachments/x.jpg" {
		t.Errorf("Resolve = %q, %v", got, err)
	}
	if _, err := Resolve("", "../x.jpg"); err == nil {
		t.Error("Resolve climbed out of the root")
	}
	if _, err := Resolve("a", "https://x/y.png"); err == nil {
		t.Error("Resolve took an address")
	}
	if got := Rel("Daily", "attachments/x.jpg"); got != "../attachments/x.jpg" {
		t.Errorf("Rel = %q", got)
	}
	if got := Rel("", "attachments/x.jpg"); got != "attachments/x.jpg" {
		t.Errorf("Rel root = %q", got)
	}
	if got := Rel("Projects/Athena", "Projects/Athena/.attachments.5/x.jpg"); got != ".attachments.5/x.jpg" {
		t.Errorf("Rel same = %q", got)
	}
}

func exercise(t *testing.T, b Backend) {
	ctx := context.Background()
	if err := b.Test(ctx); err != nil {
		t.Fatalf("test: %v", err)
	}
	e, err := b.Create(ctx, "Projects/Plan č.md", []byte("# Plan"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.ETag == "" {
		t.Fatal("no etag")
	}
	if _, err := b.Create(ctx, "Projects/Plan č.md", []byte("x")); !errors.Is(err, ErrExists) {
		t.Fatalf("create twice: %v", err)
	}
	if _, err := b.Write(ctx, "Projects/Plan č.md", []byte("# Plan 2"), "nope"); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("write stale: %v", err)
	}
	e2, err := b.Write(ctx, "Projects/Plan č.md", []byte("# Plan 2"), e.ETag)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _, err := b.Read(ctx, "Projects/Plan č.md")
	if err != nil || string(data) != "# Plan 2" {
		t.Fatalf("read: %q %v", data, err)
	}
	if _, err := b.Write(ctx, "New/Deep/n.md", []byte("x"), ""); err != nil {
		t.Fatalf("write into new folders: %v", err)
	}
	if _, err := b.Move(ctx, "Projects/Plan č.md", "Archive/Plan.md"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, _, err := b.Read(ctx, "Projects/Plan č.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old still there: %v", err)
	}
	_ = e2
	if err := b.Mkdir(ctx, "Empty/Inside"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := b.Create(ctx, ".hidden/x.md", []byte("x")); err != nil {
		t.Fatalf("hidden create: %v", err)
	}
	list, err := b.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range list {
		if !e.IsDir {
			files = append(files, e.Path)
		}
	}
	sort.Strings(files)
	if len(files) != 2 || files[0] != "Archive/Plan.md" || files[1] != "New/Deep/n.md" {
		t.Fatalf("list: %v", files)
	}
	if err := b.Delete(ctx, "Archive/Plan.md"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, "Archive/Plan.md"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestLocal(t *testing.T) {
	exercise(t, NewLocal(t.TempDir()+"/notes", "builtin", "test"))
}

func TestWebDAV(t *testing.T) {
	h := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.NewMemFS(), LockSystem: webdav.NewMemLS()}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "pw" {
			w.WriteHeader(401)
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer ts.Close()
	// The parent must exist for a plain WebDAV server.
	req, _ := http.NewRequest("MKCOL", ts.URL+"/dav/files/", nil)
	req.SetBasicAuth("me", "pw")
	if res, err := http.DefaultClient.Do(req); err == nil {
		res.Body.Close()
	}
	creds := func(context.Context) (string, string, error) { return "me", "pw", nil }
	b, err := NewWebDAV(ts.URL+"/dav/files/My Notes", creds, "test")
	if err != nil {
		t.Fatal(err)
	}
	exercise(t, b)

	bad, _ := NewWebDAV(ts.URL+"/dav/files/My Notes", func(context.Context) (string, string, error) { return "me", "wrong", nil }, "x")
	if _, err := bad.List(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatalf("bad password: %v", err)
	}
}

func TestCopy(t *testing.T) {
	ctx := context.Background()
	a := NewLocal(t.TempDir(), "builtin", "a")
	b := NewLocal(t.TempDir(), "folder", "b")
	_, _ = a.Create(ctx, "x.md", []byte("one"))
	_, _ = a.Create(ctx, "d/y.md", []byte("two"))
	_, _ = b.Create(ctx, "x.md", []byte("other"))
	_, _ = b.Create(ctx, "d/y.md", []byte("two"))
	copied, renamed, err := Copy(ctx, a, b, nil)
	if err != nil || copied != 1 || renamed != 1 {
		t.Fatalf("copy: %d %d %v", copied, renamed, err)
	}
	if data, _, _ := b.Read(ctx, "x (copy).md"); string(data) != "one" {
		t.Fatalf("copy kept: %q", data)
	}
}
