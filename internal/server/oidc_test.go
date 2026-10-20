package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/config"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// fakeIdP is a tiny OpenID provider: it signs whoever `email` says.
type fakeIdP struct {
	*httptest.Server
	key, forger *rsa.PrivateKey
	clientID    string
	secret      string
	email       string
	verified    bool
	forge       bool
	nonces      map[string]string // code → nonce
	challenges  map[string]string // code → PKCE challenge
}

func newFakeIdP(t *testing.T, clientID, secret string) *fakeIdP {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	f, _ := rsa.GenerateKey(rand.Reader, 2048)
	p := &fakeIdP{key: k, forger: f, clientID: clientID, secret: secret, verified: true, nonces: map[string]string{}, challenges: map[string]string{}}
	mux := http.NewServeMux()
	p.Server = httptest.NewServer(mux)
	b64 := base64.RawURLEncoding.EncodeToString
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"issuer": p.URL, "authorization_endpoint": p.URL + "/authorize", "token_endpoint": p.URL + "/token", "jwks_uri": p.URL + "/jwks"})
	})
	mux.HandleFunc("/api/v1/auth/subject", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "iss": p.URL})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "use": "sig", "kid": "k1", "n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes())}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != p.clientID || q.Get("code_challenge_method") != "S256" {
			http.Error(w, "bad request", 400)
			return
		}
		code := store.RandomToken(8)
		p.nonces[code], p.challenges[code] = q.Get("nonce"), q.Get("code_challenge")
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		id, secret, _ := r.BasicAuth()
		code := r.FormValue("code")
		sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if id != p.clientID || secret != p.secret || p.challenges[code] != b64(sum[:]) {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		head, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
		body, _ := json.Marshal(map[string]any{"iss": p.URL, "sub": strings.ToLower(p.email), "auth_revision": int64(1), "aud": p.clientID, "exp": time.Now().Add(time.Hour).Unix(), "nonce": p.nonces[code], "email": p.email, "email_verified": p.verified, "name": "Aleksandar"})
		signing := b64(head) + "." + b64(body)
		h := sha256.Sum256([]byte(signing))
		signer := p.key
		if p.forge {
			signer = p.forger
		}
		sig, _ := rsa.SignPKCS1v15(rand.Reader, signer, crypto.SHA256, h[:])
		json.NewEncoder(w).Encode(map[string]string{"id_token": signing + "." + b64(sig), "access_token": "at", "token_type": "Bearer"})
	})
	t.Cleanup(p.Close)
	return p
}

// oidcHarness starts the app in front of the fake provider and returns the
// server plus helpers that run the browser dance and check the session.
type oidcHarness struct {
	ts     *httptest.Server
	st     *store.Store
	signIn func(next string) (*url.URL, *http.Client)
	me     func(cl *http.Client) int
}

func newOIDCHarness(t *testing.T, cfg *config.Config, bootstrap bool, before func(dir string)) *oidcHarness {
	dir := t.TempDir()
	cfg.DataDir, cfg.DBPath = dir, dir+"/t.db"
	if before != nil {
		before(dir)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ts := httptest.NewUnstartedServer(nil)
	cfg.BaseURL = "http://" + ts.Listener.Addr().String()
	s := New(cfg, st, log, nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if bootstrap {
		if err := s.Bootstrap(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ts.Config.Handler = s.Router()
	ts.Start()
	t.Cleanup(ts.Close)
	h := &oidcHarness{ts: ts, st: st}
	h.signIn = func(next string) (*url.URL, *http.Client) {
		jar, _ := cookiejar.New(nil)
		var last *url.URL
		cl := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			last = req.URL
			if strings.HasPrefix(req.URL.String(), ts.URL) && !strings.HasPrefix(req.URL.Path, "/api/") {
				return http.ErrUseLastResponse // back in the app
			}
			return nil
		}}
		res, err := cl.Get(ts.URL + "/api/v1/auth/oidc/start?next=" + url.QueryEscape(next))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return last, cl
	}
	h.me = func(cl *http.Client) int {
		res, err := cl.Get(ts.URL + "/api/v1/auth/me")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	return h
}

func authStatus(t *testing.T, ts *httptest.Server) map[string]any {
	res, err := http.Get(ts.URL + "/api/v1/auth/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var status map[string]any
	json.NewDecoder(res.Body).Decode(&status)
	return status
}

func TestOIDCSignIn(t *testing.T) {
	idp := newFakeIdP(t, "podcasts", "shh")
	cfg := &config.Config{AuthMode: "local", SessionTTL: time.Hour, OIDCIssuer: idp.URL, OIDCClientID: "podcasts", OIDCClientSecret: "shh", OIDCName: "PocketID", OIDCAutoCreate: false}
	h := newOIDCHarness(t, cfg, false, nil)
	ctx := context.Background()
	_ = h.st.CreateUser(ctx, &store.User{Email: "aleksandar@example.com", Name: "A", Role: "admin", PasswordHash: "x"})

	idp.email = "Aleksandar@Example.com"
	end, cl := h.signIn("/library")
	if end == nil || end.Path != "/library" || h.me(cl) != 200 {
		t.Fatalf("happy path ended at %v, me=%d", end, h.me(cl))
	}

	// the login page learns about the button from the status
	if status := authStatus(t, h.ts); status["oidc"] != true || status["oidc_name"] != "PocketID" {
		t.Errorf("status %v", status)
	}
	// password login keeps working next to the provider
	res, _ := http.Post(h.ts.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"email":"aleksandar@example.com","password":"x"}`))
	if res.StatusCode != 401 {
		t.Errorf("password login with a wrong password: %d (want 401, not switched off)", res.StatusCode)
	}

	for _, c := range []struct {
		name, email string
		verified    bool
		forge       bool
		next, want  string
	}{
		{"unknown email", "stranger@example.com", true, false, "/", "No Pocket Notes account"},
		{"unverified email", "aleksandar@example.com", false, false, "/", "verified email"},
		{"forged signature", "aleksandar@example.com", true, true, "/", "did not check out"},
	} {
		idp.email, idp.verified, idp.forge = c.email, c.verified, c.forge
		end, cl := h.signIn(c.next)
		if end == nil || end.Path != "/login" || !strings.Contains(end.Query().Get("error"), c.want) || h.me(cl) != 401 {
			t.Errorf("%s: ended at %v", c.name, end)
		}
	}

	// the return path cannot leave the site
	idp.email, idp.verified, idp.forge = "aleksandar@example.com", true, false
	if end, _ := h.signIn("//evil.example/x"); end == nil || end.Host != strings.TrimPrefix(h.ts.URL, "http://") || end.Path != "/" {
		t.Errorf("open redirect: %v", end)
	}
	// a callback without the browser's cookie is refused
	res, _ = (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(h.ts.URL + "/api/v1/auth/oidc/callback?code=x&state=y")
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/login?error=") {
		t.Errorf("cookieless callback → %q", loc)
	}
}

// TestOIDCFromCore covers the zero-configuration path: no OIDC_* variables,
// the client Core issued at registration (persisted by the SDK) is used, the
// button says "Pocket Core" and unknown people get an account because Core
// vouched for them. Explicit OIDC_* variables still win when set.
func TestOIDCFromCore(t *testing.T) {
	idp := newFakeIdP(t, "pco_podcasts", "core-secret")
	coreClient := func(dir string) {
		b, _ := json.Marshal(map[string]string{"issuer": idp.URL, "client_id": "pco_podcasts", "client_secret": "core-secret"})
		if err := os.WriteFile(dir+"/core.token.oidc.json", b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// CORE_URL set (Configured), nothing listens there; Bootstrap is skipped so no registration loop runs.
	cfg := &config.Config{AuthMode: "local", SessionTTL: time.Hour, CoreURL: idp.URL, OIDCName: "single sign-on", OIDCAutoCreate: false}
	h := newOIDCHarness(t, cfg, false, coreClient)

	if status := authStatus(t, h.ts); status["oidc"] != true || status["oidc_name"] != "Pocket Core" {
		t.Fatalf("status with the Core-issued client: %v", status)
	}
	idp.email = "newcomer@example.com"
	end, cl := h.signIn("/library")
	if end == nil || end.Path != "/library" || h.me(cl) != 200 {
		t.Fatalf("Core-vouched newcomer ended at %v, me=%d", end, h.me(cl))
	}
	if _, err := h.st.GetUserByEmail(context.Background(), "newcomer@example.com"); err != nil {
		t.Errorf("account not created for the Core-vouched person: %v", err)
	}

	// explicit OIDC_* variables take precedence over the Core-issued client
	envIdP := newFakeIdP(t, "env-client", "env-secret")
	cfg2 := &config.Config{AuthMode: "local", SessionTTL: time.Hour, CoreURL: idp.URL,
		OIDCIssuer: envIdP.URL, OIDCClientID: "env-client", OIDCClientSecret: "env-secret", OIDCName: "Authelia", OIDCAutoCreate: false}
	h2 := newOIDCHarness(t, cfg2, false, coreClient)
	if status := authStatus(t, h2.ts); status["oidc"] != true || status["oidc_name"] != "Authelia" {
		t.Fatalf("status with OIDC_* set: %v", status)
	}
	envIdP.email = "stranger@example.com"
	if end, cl := h2.signIn("/"); end == nil || end.Path != "/login" || !strings.Contains(end.Query().Get("error"), "No Pocket Notes account") || h2.me(cl) != 401 {
		t.Errorf("env provider with OIDC_AUTO_CREATE=false let a stranger in: %v", end)
	}

	// without Core and without OIDC_* there is nothing: no button, 404 on start
	h3 := newOIDCHarness(t, &config.Config{AuthMode: "local", SessionTTL: time.Hour, OIDCName: "single sign-on", OIDCAutoCreate: true}, false, nil)
	if status := authStatus(t, h3.ts); status["oidc"] != false {
		t.Errorf("status without a provider: %v", status)
	}
	res, err := http.Get(h3.ts.URL + "/api/v1/auth/oidc/start")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("start without a provider: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
}
