# Configuration

Everything is an environment variable; every one has a default that works with a single volume.

| Variable | Default | What it does |
|---|---|---|
| `PORT` / `HOST` | `8080` / `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (`https://podcasts.example.com`). Needed for OIDC, public feed URLs and notification links |
| `DATA_DIR` | `./data` (`/data` in the image) | Database, media, artwork, backups |
| `DB_PATH` | `$DATA_DIR/pocket-notes.db` | SQLite file (WAL mode) |
| `MEDIA_DIR` | `$DATA_DIR/media` | Downloaded episodes, one folder per subscription |
| `ARTWORK_DIR` | `$DATA_DIR/artwork` | Cached and custom show artwork |
| `TZ` | – | Timezone for daily stats (`Europe/Sarajevo`). Alpine images need `/usr/share/zoneinfo` for anything but UTC |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug` logs every request; `json` for log shippers |
| `TRUST_PROXY` | `true` | Use `X-Forwarded-For` for client IPs |
| `METRICS_ENABLED` | `true` | Prometheus text at `/metrics` |

## Feeds and downloads

| Variable | Default | What it does |
|---|---|---|
| `REFRESH_MINUTES` | `60` | Box default for checking feeds; people and podcasts can set their own (min 5) |
| `FEED_TIMEOUT` | `45s` | One feed fetch |
| `DOWNLOAD_WORKERS` | `2` | Parallel downloads (RSS files and YouTube) |
| `MAX_EPISODES_PER_FEED` | `0` | Keep at most this many episodes per RSS feed in the database (never played/downloaded ones go). 0 = all |

## YouTube

| Variable | Default | What it does |
|---|---|---|
| `YTDLP_PATH` | – | Explicit `yt-dlp` executable. Default: `python3 -m yt_dlp` with `$DATA_DIR/ytdlp` on `PYTHONPATH` when an in-app update installed one there, else `yt-dlp` on `PATH` |
| `YTDLP_EXTRA_ARGS` | – | Extra arguments for every call (e.g. `--js-runtimes node`, `--proxy …`) |
| `YTDLP_COOKIES` | – | `cookies.txt` for age-restricted or members-only content |
| `YOUTUBE_INDEX_LIMIT` | `30` | How many of a channel's newest videos are indexed per refresh |

The image has `yt-dlp` (from PyPI, with its JavaScript-challenge helper), `deno` and `ffmpeg`. For the bare binary install them yourself; YouTube needs a JavaScript runtime (`deno` is what yt-dlp enables by default; `node` works with `YTDLP_EXTRA_ARGS=--js-runtimes node`). **Settings → Feeds & downloads → Update now** installs the newest yt-dlp into `$DATA_DIR/ytdlp` with pip, no rebuild.

## Search

| Variable | Default | What it does |
|---|---|---|
| `PODCASTINDEX_KEY` / `PODCASTINDEX_SECRET` | – | Podcast Index credentials for the box owner (free at api.podcastindex.org). Everyone can also set their own in Settings → Search |

## Notifications

| Variable | Default | What it does |
|---|---|---|
| `NOTIFY_URL` | – | Where the owner's new-episode notifications go (Apprise route, ntfy topic URL, webhook) |
| `NOTIFY_KIND` | `apprise` | `apprise`, `ntfy` or `webhook` |

Everyone can set their own in Settings → Notifications; a podcast only notifies when its "Notify me" switch is on.

## Accounts

| Variable | Default | What it does |
|---|---|---|
| `AUTH_MODE` | `local` | `local` (built-in login), `proxy` (trust a header from an auth proxy), `none` (single user, no login) |
| `AUTH_PROXY_HEADER` / `AUTH_PROXY_EMAIL_HEADER` | `Remote-User` / `Remote-Email` | Headers read in proxy mode |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | – | Create the first admin on start (skips the setup screen) |
| `SESSION_TTL` | `720h` | Browser session length |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_NAME` / `OIDC_AUTO_CREATE` | – | Sign in with any OpenID Connect provider (callback `BASE_URL/api/v1/auth/oidc/callback`). With Pocket Core these are not needed |

## Pocket Core

| Variable | What it does |
|---|---|
| `CORE_URL` | Pocket Core's address; registers on start, publishes events, accepts Core tokens, offers "Sign in with Pocket Core" |
| `CORE_REGISTRATION_KEY` | Core's registration key |
| `CORE_APP_URL` | How Core reaches this app (default `BASE_URL`) |

## Other

| Variable | Default | What it does |
|---|---|---|
| `DEMO_DATA` | `false` | Seed three made-up shows into an empty database |

## CLI

```
pocket-notes                 start
pocket-notes backup [-o F]   consistent SQLite copy (VACUUM INTO)
pocket-notes reset-password [-email USER] [-password NEW]
pocket-notes version
```
