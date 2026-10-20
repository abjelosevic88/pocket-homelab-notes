# API

Base path `/api/v1`. JSON in and out. Authenticate with the browser session or `Authorization: Bearer <token>` (Settings → API tokens; a token acts as the person who made it). Pocket Core device tokens (`pcd_`) and link tokens (`pcx_`, scopes `podcasts:read`, `history:read`, `queue:write`) are accepted when `CORE_URL` is set.

## Discovery
- `GET /search?q=&source=all|itunes|podcastindex` → `{results[], warnings[]}`; results carry `subscribed_id` when already followed.
- `GET /search/trending?lang=` (Podcast Index key needed).
- `POST /preview {url}` → what a feed / YouTube channel / playlist / video is before subscribing.

## Subscriptions
- `GET /podcasts?q=&kind=rss|youtube&sort=title|recent|unplayed|added`
- `POST /podcasts {url, title?, auto_download?, download_limit?, retention_days?, min_duration?, title_filter?, notify?, playback_speed?, pub_date_mode?}` — a feed URL or a YouTube channel/playlist URL. Indexes synchronously, returns the podcast.
- `GET /podcasts/{id}` · `PUT /podcasts/{id}` (any subset of the settings) · `DELETE /podcasts/{id}`
- `POST /podcasts/{id}/refresh` → `{new, podcast}` · `POST /refresh` (all, in the background)
- `POST /podcasts/{id}/favorite {favorite}` · `POST /podcasts/{id}/played {played}` · `POST /podcasts/{id}/download {count}` · `DELETE /podcasts/{id}/files?played=1`
- `GET /podcasts/{id}/episodes?…` (same filters as `/episodes`)
- `GET|POST|DELETE /podcasts/{id}/artwork` (POST: multipart `file` or JSON `{url}`)
- `POST /youtube/video {url}` → downloads one video into your "Picks" show.
- `POST /picks {feed_url, guid|title, download?}` → one episode of any feed into your "Picks" show (no subscription), downloaded unless `download: false`. `guid` comes from `/preview`.

## Episodes
- `GET /episodes?q=&status=unplayed|partial|played|unfinished&downloaded=1|0&days=&kind=&podcast=&min_duration=&max_duration=&sort=newest|oldest|shortest|longest&limit=&offset=` → `{episodes[], total}`
- `GET /episodes/{id}` → episode with `chapters[]` and `sponsors[]`
- `GET /episodes/{id}/stream` — the downloaded file with range support (`?download=1` as attachment); redirects to the enclosure when not downloaded.
- `GET /episodes/{id}/artwork`
- `POST /episodes/{id}/progress {position, listened, duration?, completed?}` — called by the player every ~10 s; `listened` seconds feed the stats.
- `POST|DELETE /episodes/{id}/played` · `DELETE /episodes/{id}/history`
- `POST|DELETE /episodes/{id}/download` · `POST|DELETE /episodes/{id}/queue` (`{front: true}` = play next) · `POST|DELETE /episodes/{id}/save`
- `DELETE /episodes/{id}`

## Lists
- `GET /home` — everything the Home page shows.
- `GET /queue` · `PUT /queue {ids[]}` · `DELETE /queue` · `GET /queue/next?after=`
- `GET /history?limit=&offset=` · `DELETE /history`
- `GET /saved` · `GET /downloads` → `{pending[], done[], bytes, yt_dlp}`
- `GET|POST /playlists` · `GET|PUT|DELETE /playlists/{id}` · `GET /playlists/{id}/episodes`

## You
- `GET /dashboard` — compact numbers for widgets (Homepage `customapi`): `podcasts, unplayed, queued, downloaded, downloaded_mb, listened_today_min, listened_week_min, now_playing, next_up, new_today, downloads_pending`.
- `GET /stats` · `GET /activity?kind=&limit=`
- `GET|PUT /settings` · `GET /settings/system` · `POST /settings/notify/test` · `POST /settings/feed-key` · `POST /settings/ytdlp/update` (admin)
- `GET /auth/me` · `PUT /auth/me` · `GET /tokens` · `POST /tokens {name}` · `DELETE /tokens/{id}`
- `GET /export.opml` · `GET /export.json` · `POST /import/opml` (multipart `file` or raw body) · `POST /import` (the JSON export) · `GET /backup` (admin)

## Public feeds (no login)
- `GET /feed/{key}/` — HTML index of your feeds
- `GET /feed/{key}/{podcast}.xml` — RSS 2.0 with itunes tags; YouTube feeds list downloaded episodes only
- `GET /feed/{key}/e/{episode}.m4a` (any extension), `/feed/{key}/e/{episode}/chapters.json`, `/feed/{key}/art/{podcast}.jpg`, `/feed/{key}/art/e/{episode}.jpg`

The key is in Settings → Public feeds. Feed URLs use `feed_base_url` (Settings) or `BASE_URL`.

## Health
`GET /healthz`, `GET /readyz`, `GET /metrics`.
