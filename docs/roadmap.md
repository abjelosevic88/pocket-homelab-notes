# Roadmap

Done in 0.1: subscriptions (RSS + YouTube), player with resume/speed/chapters/sleep/media session, queue, saved, history, smart playlists, downloads with retention, SponsorBlock skipping, public feeds, OPML/JSON import and export, PinePods importer, stats, notifications, per-person accounts, OIDC, Pocket Core (events, tokens, links), Homepage dashboard.

Next, roughly in order:
- **gpodder.net-compatible sync API** so AntennaPod, gPodder and friends sync subscriptions and positions with this box.
- **Transcripts** in the episode page (podcast 2.0 `podcast:transcript`, and Whisper for YouTube).
- **Offline in the browser**: PWA cache of downloaded episodes for the phone.
- **Chapters from YouTube descriptions** when the video has none (`0:00 Intro` lines).
- **Per-episode trim** (skip a specific sponsor the community hasn't marked) and a "report segment" button.
- **Shared household shows**: one download for two people who follow the same YouTube channel.
- **Pocket Day**: listening history as a journal source through the `history:read` link.
- Cross-device "now playing" hand-off.
