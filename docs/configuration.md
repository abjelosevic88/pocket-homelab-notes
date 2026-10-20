# Configuration

Everything is an environment variable; every one has a default, so `docker run` with one volume works.
Each person's own choices (where their notes live, the editor, daily notes, integrations) are in
Settings, not here.

| Variable | Default | What it does |
| --- | --- | --- |
| `PORT` / `HOST` | `8080` / `0.0.0.0` | Where the server listens. |
| `BASE_URL` | — | The public address (`https://notes.example.com`). Needed for sign-in with Pocket Core / OIDC and used in links. |
| `DATA_DIR` | `./data` (`/data` in the image) | The database and the built-in notes folders (`data/notes/<user id>/`). |
| `DB_PATH` | `$DATA_DIR/pocket-notes.db` | The SQLite index: notes' text, tags, links, history, trash, shares, settings. |
| `STORAGE_ROOTS` | — | Comma separated folders on the server that people may keep their notes in (a NAS mount, a Syncthing or Obsidian folder). Each person picks a folder inside one; two people cannot share one. |
| `AUTH_MODE` | `local` | `local` (accounts with passwords), `proxy` (trust `Remote-User` from an auth proxy), `none` (everyone is the admin; for a demo). |
| `AUTH_PROXY_HEADER` / `AUTH_PROXY_EMAIL_HEADER` | `Remote-User` / `Remote-Email` | Headers read in proxy mode. |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | — | Create the first admin on start instead of the setup screen. |
| `SESSION_TTL` | `720h` | How long a sign-in lasts. |
| `SECURE_COOKIES` | `false` | Set behind HTTPS. |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_NAME` / `OIDC_AUTO_CREATE` | — | Sign in with an OpenID Connect provider. Not needed with Pocket Core. |
| `CORE_URL` / `CORE_REGISTRATION_KEY` / `CORE_APP_URL` | — | Pocket Core: the app registers, offers "Sign in with Pocket Core", publishes `notes.*` events, and reaches each person's services (Nextcloud, Immich, Forgejo …) and Pocket apps (Habits, Money, Garage, Podcasts, Documents) through Core. |
| `NOTIFY_URL` / `NOTIFY_KIND` | — / `apprise` | Where the owner hears that their notes folder stopped answering (`apprise`, `ntfy`, `webhook`). Everyone can set their own in Settings. |
| `TRUST_PROXY` | `true` | Read the client address from `X-Forwarded-For`. |
| `METRICS_ENABLED` | `true` | Prometheus metrics at `/metrics`. |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug`…`error`; `text` or `json`. |
| `DEMO_DATA` | `false` | Seed a few made-up notes into an empty box. |
| `TZ` | UTC | Daily notes, capture times and "today" follow it. On Alpine, mount `/usr/share/zoneinfo` or the image's tzdata answers. |
