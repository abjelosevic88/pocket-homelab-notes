package server

import (
	"context"
	"net/http"

	"github.com/abjelosevic88/pocket-homelab-core/sdk/appkit"
)

// Sign-in with an OpenID Connect provider (Pocket Core, PocketID, …). The
// relying party itself — PKCE, discovery, the ID token's signature, the
// return path — is appkit.RP in the Core SDK; this file only says where its
// settings come from and how a verified person becomes a Notes session.
//
// On a box with Pocket Core nothing needs configuring: the app asks for a
// relying-party client when it registers (only with a BASE_URL, which the
// redirect URI needs) and uses the one Core issues. Explicit OIDC_*
// variables take precedence when set.

func (s *Server) newRP() *appkit.RP {
	return &appkit.RP{
		Settings: func() appkit.OIDCSettings {
			// Core's client is only asked for (and usable) with a BASE_URL
			return appkit.EnvOrCoreAt(s.core, s.cfg.BaseURL, s.cfg.OIDCIssuer, s.cfg.OIDCClientID, s.cfg.OIDCClientSecret, s.cfg.OIDCName, s.cfg.OIDCAutoCreate)
		},
		CallbackURL: s.oidcCallbackURL,
		Login: func(ctx context.Context, id appkit.Identity, autoCreate bool) (string, error) {
			u, err := s.identityUser(ctx, id.Issuer, id.Subject, id.Email, id.Name, autoCreate)
			if err != nil {
				return "", err
			}
			sid, err := s.store.CreateSession(ctx, u.ID, s.cfg.SessionTTL)
			if err != nil {
				return "", &appkit.LoginError{Message: "Could not start a session", Err: err}
			}
			return sid, nil
		},
		EndSession: func(ctx context.Context, sid string) { _ = s.store.DeleteSession(ctx, sid) },
		SetSession: func(w http.ResponseWriter, sid string) { s.setSessionCookie(w, sid, s.cfg.SessionTTL) },
		Core:       s.core,
		Cookie:     "pp_oidc",
		Secure:     s.cfg.SecureCookies,
		AppName:    "Pocket Notes",
		Log:        s.log,
	}
}

func (s *Server) oidcCallbackURL() string { return s.cfg.BaseURL + "/api/v1/auth/oidc/callback" }
