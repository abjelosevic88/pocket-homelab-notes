export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type Kind = 'rss' | 'youtube'
export type DownloadState = 'none' | 'queued' | 'downloading' | 'done' | 'failed' | 'culled'

export interface Podcast {
  id: number; user_id: number; kind: Kind; title: string; feed_url: string; site_url: string; youtube_id: string; youtube_kind: string
  author: string; description: string; artwork_url: string; artwork_file: string; artwork: string; language: string; categories: string
  explicit: boolean; favorite: boolean; playback_speed: number; start_skip: number; end_skip: number; auto_play_next: number; notify: boolean
  auto_download: boolean; download_limit: number; retention_days: number; min_duration: number; title_filter: string; feed_cutoff_days: number
  pub_date_mode: 'published' | 'downloaded'; sponsorblock: boolean; refresh_minutes: number
  last_refresh_at: string; last_refresh_ok: string; last_error: string; failures: number; subscribed_at: string; created_at: string; updated_at: string
  episode_count: number; unplayed_count: number; downloaded_count: number; downloaded_bytes: number; latest_pub_date: string; refreshing: boolean
}

export interface Episode {
  id: number; podcast_id: number; guid: string; title: string; description: string; link: string
  enclosure_url: string; enclosure_type: string; enclosure_bytes: number; duration: number; pub_date: string; artwork_url: string
  season: number; episode_no: number; episode_type: string; explicit: boolean; is_video: boolean; chapters_url: string; transcript_url: string
  youtube_id: string; position: number; file_bytes: number; file_type: string; downloaded_at: string; download: DownloadState; download_error: string; created_at: string
  podcast_title: string; podcast_kind: Kind; podcast_artwork: string; playback_speed: number; start_skip: number; end_skip: number
  play_position: number; completed: boolean; last_played_at: string; play_count: number; saved: boolean; queue_pos: number
  has_chapters: boolean; has_sponsors: boolean; stream_url: string
}

export interface Chapter { start: number; end?: number; title: string; url?: string; img?: string }
export interface Segment { start: number; end: number; category: string }
export interface EpisodeSource { feed_url: string; title: string; kind: Kind; subscribed_id?: number }
export interface EpisodeDetail extends Episode { chapters: Chapter[]; sponsors: Segment[]; source?: EpisodeSource }

export interface Playlist {
  id: number; user_id: number; name: string; description: string; icon: string; podcast_ids: number[]
  include_unplayed: boolean; include_partial: boolean; include_played: boolean; min_duration: number; max_duration: number; max_age_days: number
  downloaded_only: boolean; sort: string; limit: number; pos: number; created_at: string; count: number
}

export interface Activity { id: number; user_id: number; kind: string; message: string; podcast_id: number; episode_id: number; created_at: string }

export interface Settings {
  title: string; setup_complete: boolean; landing: 'home' | 'podcasts' | 'queue' | 'episodes'
  playback_speed: number; skip_forward: number; skip_back: number; auto_play_next: boolean; complete_at: number
  auto_skip_sponsors: boolean; skip_categories: string; queue_next: boolean
  refresh_minutes: number; refresh_default: number; auto_download_default: boolean; retention_days: number; youtube_limit: number; youtube_min_duration: number; keep_played: boolean
  search_source: 'all' | 'itunes' | 'podcastindex'; podcastindex_key: string; podcastindex_secret_set: boolean; podcastindex_env: boolean
  notify_kind: 'apprise' | 'ntfy' | 'webhook'; notify_url: string; notify_token_set: boolean; notify_env: boolean
  feed_key: string; feed_base_url: string; feed_url: string; yt_dlp: string; yt_dlp_updatable: boolean
}

export interface Dashboard {
  podcasts: number; unplayed: number; queued: number; downloaded: number; downloaded_mb: number; listened_today_min: number; listened_week_min: number
  now_playing: EpisodeLine | null; next_up: EpisodeLine | null; new_today: number; downloads_pending: number
}
export interface EpisodeLine { id: number; title: string; podcast: string; progress: number; date?: string }

export interface SearchResult {
  title: string; author: string; feed_url: string; site_url: string; image: string; description: string; language: string; categories: string[]
  episodes: number; source: string; source_id: string; last_update?: string; subscribed_id?: number
}

export interface Preview {
  kind: 'rss' | 'youtube' | 'video'; youtube_kind?: string; url: string; title: string; author: string; image: string; description: string
  episodes?: { title: string; duration: number; date: string; guid?: string; description?: string; image?: string; has_audio?: boolean }[]; episode_count?: number; duration?: number; subscribed_id?: number
}

export interface Stats {
  total_seconds: number; year_seconds: number; month_seconds: number; week_seconds: number; completed: number; completed_year: number; started: number
  podcasts: number; episodes: number; downloaded: number; downloaded_mb: number; active_days: number; streak: number; first_listen: string
  days: { day: string; seconds: number }[]; months: { day: string; seconds: number }[]; hours: number[]; weekdays: number[]
  top_podcasts: { id: number; title: string; seconds: number; episodes: number }[]; top_episodes: { id: number; title: string; podcast: string; seconds: number }[]
}

export interface Home { continue: Episode[]; queue: Episode[]; fresh: Episode[]; recent: Episode[]; podcasts: Podcast[]; dashboard: Dashboard }
