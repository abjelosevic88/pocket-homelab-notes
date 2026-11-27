package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-core/sdk/appkit"
	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

type ctxKey int

const (
	userKey ctxKey = 1
	viaKey  ctxKey = 2
)

// viaApp answers the Pocket app acting for the person in this request
// ("hub", "Pocket Hub"), both empty when the person asked themself.
// requireAuth keeps it from the link token (appkit.Via).
func viaApp(r *http.Request) (slug, name string) {
	v, _ := r.Context().Value(viaKey).(appkit.Via)
	return v.Slug, v.Name
}

const sessionCookie = "pn_session"

func userFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(userKey).(*store.User)
	return u
}

func actorID(ctx context.Context) int64 {
	if u := userFrom(ctx); u != nil {
		return u.ID
	}
	return 0
}

// newAuth wires appkit.Auth (Core SDK) to Notes' store: bearer tokens
// (pcx_ link tokens through linkRoutes, pcd_/pcu_/pco_ through Core, else
// Notes' own API tokens), then Basic auth for Nextcloud Notes clients, then
// AUTH_MODE none/proxy, then the session cookie.
func (s *Server) newAuth() *appkit.Auth[*store.User] {
	return &appkit.Auth[*store.User]{
		Core:             s.core,
		Slug:             "notes",
		Mode:             func() string { return s.cfg.AuthMode },
		ProxyHeader:      s.cfg.AuthProxyHeader,
		ProxyEmailHeader: s.cfg.AuthProxyEmailHeader,
		SessionCookie:    sessionCookie,
		APIToken:         s.store.GetUserByAPIToken,
		Session:          s.store.GetSessionUser,
		Person:           s.firstOrSystemUser,
		Identity:         s.coreIdentityUser,
		Allow:            linkAllows,
		// Nextcloud Notes clients sign in with Basic auth: the password is an
		// API token of this app, a Core token, or (with local accounts) the
		// password itself.
		Fallback: func(r *http.Request) (*store.User, bool, error) {
			user, pass, ok := r.BasicAuth()
			if !ok || pass == "" {
				return nil, false, nil
			}
			u, err := s.basicUser(r.Context(), user, pass)
			return u, true, err
		},
	}
}

// requireAuth resolves the current user (403 for a link token outside its
// scopes, 401 otherwise) and keeps the acting app for viaApp.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return s.auth.Require(func(r *http.Request, u *store.User, via appkit.Via) *http.Request {
		ctx := context.WithValue(r.Context(), viaKey, via)
		return r.WithContext(context.WithValue(ctx, userKey, u))
	})(next)
}

func (s *Server) resolveUser(r *http.Request) (*store.User, error) {
	u, _, err := s.auth.Resolve(r)
	return u, err
}

// basicUser resolves Basic auth: an API token (the username is not needed
// then, though a given one must match) or an account's email and password.
func (s *Server) basicUser(ctx context.Context, user, pass string) (*store.User, error) {
	if strings.HasPrefix(pass, "pn_") {
		u, err := s.store.GetUserByAPIToken(ctx, pass)
		if err != nil {
			return nil, err
		}
		if user != "" && !strings.EqualFold(user, u.Email) && !strings.EqualFold(user, strings.Split(u.Email, "@")[0]) && !strings.EqualFold(user, u.Name) {
			return nil, http.ErrNoCookie
		}
		return u, nil
	}
	if s.core.Configured() && core.IsCoreToken(pass) {
		return s.auth.CoreToken(ctx, pass)
	}
	if s.cfg.AuthMode != "local" {
		return nil, http.ErrNoCookie
	}
	u, err := s.store.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(user)))
	if err != nil || u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pass)) != nil {
		time.Sleep(300 * time.Millisecond)
		return nil, http.ErrNoCookie
	}
	return u, nil
}

func (s *Server) firstOrSystemUser(ctx context.Context, email, name string) (*store.User, error) {
	if u, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return u, nil
	}
	role := "member"
	if n, _ := s.store.CountUsers(ctx); n == 0 {
		role = "admin"
	}
	u := &store.User{Email: email, Name: name, Role: role}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	s.seedUser(ctx, u.ID)
	s.log.Info("auto-provisioned user", "email", email, "mode", s.cfg.AuthMode, "role", role)
	return u, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, id string, ttl time.Duration) {
	appkit.SessionCookie{Name: sessionCookie, Secure: s.cfg.SecureCookies}.Set(w, id, ttl)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, err, "count users")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), 0)
	u, _ := s.resolveUser(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup":   n == 0 && s.cfg.AuthMode == "local",
		"auth_mode":     s.cfg.AuthMode,
		"authenticated": u != nil,
		"user":          u,
		"version":       Version,
		"title":         st.Title,
		"oidc":          s.rp.Enabled() && s.cfg.AuthMode == "local",
		"oidc_name":     s.rp.Name(),
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		s.fail(w, err, "count users")
		return
	}
	if n > 0 || s.cfg.AuthMode != "local" {
		writeErr(w, http.StatusForbidden, "setup already completed")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if in.Email == "" || len(in.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "email and a password of at least 8 characters are required")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	u := &store.User{Email: in.Email, Name: in.Name, PasswordHash: string(hash), Role: "admin"}
	if err := s.store.CreateUser(ctx, u); err != nil {
		s.fail(w, err, "create user")
		return
	}
	s.seedUser(ctx, u.ID)
	st, _ := s.store.GetSettings(ctx, 0)
	st.SetupComplete = true
	_ = s.store.SaveBoxSettings(ctx, st)
	id, err := s.store.CreateSession(ctx, u.ID, s.cfg.SessionTTL)
	if err != nil {
		s.fail(w, err, "create session")
		return
	}
	s.setSessionCookie(w, id, s.cfg.SessionTTL)
	s.log.Info("initial setup completed", "email", u.Email)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.store.GetUserByEmail(r.Context(), strings.TrimSpace(strings.ToLower(in.Email)))
	if err != nil || u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		time.Sleep(300 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	id, err := s.store.CreateSession(r.Context(), u.ID, s.cfg.SessionTTL)
	if err != nil {
		s.fail(w, err, "create session")
		return
	}
	s.setSessionCookie(w, id, s.cfg.SessionTTL)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), c.Value)
	}
	appkit.SessionCookie{Name: sessionCookie, Secure: s.cfg.SecureCookies}.Clear(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var in struct {
		Name            string `json:"name"`
		Email           string `json:"email"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != "" {
		u.Name = in.Name
	}
	if in.Email != "" {
		u.Email = strings.ToLower(strings.TrimSpace(in.Email))
	}
	if in.NewPassword != "" {
		if u.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.CurrentPassword)) != nil {
			writeErr(w, http.StatusBadRequest, "current password is incorrect")
			return
		}
		if len(in.NewPassword) < 8 {
			writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
		u.PasswordHash = string(h)
	}
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		s.fail(w, err, "update user")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// ---- users (household members) ----

// seedUser starts a new person's notes in the built-in folder.
func (s *Server) seedUser(ctx context.Context, uid int64) {
	_ = s.store.SetUserSetting(ctx, uid, "storage_kind", "builtin")
}

// handleListUsers lists the accounts for admins, who manage them; a member
// sees only their own (notes are private, nothing here links to others).
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if u := userFrom(r.Context()); u == nil || u.Role != "admin" {
		writeJSON(w, http.StatusOK, []*store.User{u})
		return
	}
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.fail(w, err, "list users")
		return
	}
	if users == nil {
		users = []store.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if u := userFrom(r.Context()); u == nil || u.Role != "admin" {
		writeErr(w, http.StatusForbidden, "only an admin can manage users")
		return false
	}
	return true
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if in.Email == "" {
		writeErr(w, http.StatusBadRequest, "email is required")
		return
	}
	if s.cfg.AuthMode == "local" && len(in.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if in.Role != "admin" {
		in.Role = "member"
	}
	u := &store.User{Email: in.Email, Name: strings.TrimSpace(in.Name), Role: in.Role}
	if in.Password != "" {
		h, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		u.PasswordHash = string(h)
	}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		s.fail(w, err, "create user")
		return
	}
	s.seedUser(r.Context(), u.ID)
	s.log.Info("user created", "email", u.Email, "role", u.Role, "by", userFrom(r.Context()).Email)
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	u, err := s.store.GetUser(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get user")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Email != "" {
		u.Email = strings.TrimSpace(strings.ToLower(in.Email))
	}
	if in.Name != "" {
		u.Name = strings.TrimSpace(in.Name)
	}
	if in.Role == "admin" || in.Role == "member" {
		if in.Role == "member" && u.Role == "admin" {
			if n, _ := s.store.CountAdmins(r.Context()); n <= 1 {
				writeErr(w, http.StatusBadRequest, "there must be at least one admin")
				return
			}
		}
		u.Role = in.Role
	}
	if in.Password != "" {
		if len(in.Password) < 8 {
			writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		u.PasswordHash = string(h)
	}
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		s.fail(w, err, "update user")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id := idParam(r, "id")
	if id == userFrom(r.Context()).ID {
		writeErr(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	target, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		s.fail(w, err, "get user")
		return
	}
	if target.Role == "admin" {
		if n, _ := s.store.CountAdmins(r.Context()); n <= 1 {
			writeErr(w, http.StatusBadRequest, "there must be at least one admin")
			return
		}
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		s.fail(w, err, "delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- tokens ----

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.ListAPITokens(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "list tokens")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	_ = decode(r, &in)
	if in.Name == "" {
		in.Name = "token"
	}
	plain, t, err := s.store.CreateAPIToken(r.Context(), userFrom(r.Context()).ID, in.Name)
	if err != nil {
		s.fail(w, err, "create token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": plain, "info": t})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAPIToken(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "delete token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// whoFor names the signed-in person for a client-error report ("" if none).
func (s *Server) whoFor(r *http.Request) string {
	if u, err := s.resolveUser(r); err == nil && u != nil {
		return u.Email
	}
	return ""
}
