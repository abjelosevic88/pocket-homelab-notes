package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/config"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

type harness struct {
	t   *testing.T
	s   *Server
	st  *store.Store
	ts  *httptest.Server
	dir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, nil)
}

// newHarnessWith lets a test change the configuration (CORE_URL …) first.
func newHarnessWith(t *testing.T, change func(*config.Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{AuthMode: "local", SessionTTL: time.Hour, DataDir: dir, DBPath: dir + "/t.db", StorageRoots: []string{dir + "/shared"}}
	if change != nil {
		change(cfg)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(cfg, st, log, nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return &harness{t: t, s: s, st: st, ts: ts, dir: dir}
}

// user makes an account (first = admin) and an API token for it.
func (h *harness) user(email string) (*store.User, string) {
	h.t.Helper()
	u, err := h.s.firstOrSystemUser(context.Background(), email, strings.Split(email, "@")[0])
	if err != nil {
		h.t.Fatal(err)
	}
	tok, _, err := h.st.CreateAPIToken(context.Background(), u.ID, "test")
	if err != nil {
		h.t.Fatal(err)
	}
	return u, tok
}

// do sends a request with a bearer token and decodes a JSON answer into out.
func (h *harness) do(tok, method, path string, body any, out any, hdr ...string) int {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		if s, ok := body.(string); ok {
			rd = strings.NewReader(s)
		} else {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rd)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		raw, _ := io.ReadAll(res.Body)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				h.t.Fatalf("%s %s: %v in %s", method, path, err, raw)
			}
		}
	}
	return res.StatusCode
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
