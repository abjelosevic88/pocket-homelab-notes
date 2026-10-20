package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fakes of the homelab's services, shaped like their real answers.

func fakeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fakeServer(t *testing.T, routes map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for p, f := range routes {
		mux.HandleFunc(p, f)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// needBearer refuses a request without the expected bearer token.
func needBearer(tok string, f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		f(w, r)
	}
}

func connectSvc(t *testing.T, h *harness, uid int64, typ, u, user, key string) {
	t.Helper()
	ctx := context.Background()
	for k, v := range map[string]string{"mode": "direct", "url": u, "user": user, "key": key} {
		if err := h.st.SetUserSetting(ctx, uid, "svc."+typ+"."+k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func connectApp(t *testing.T, h *harness, uid int64, slug, u, key string) {
	t.Helper()
	ctx := context.Background()
	for k, v := range map[string]string{"mode": "direct", "url": u, "key": key} {
		if err := h.st.SetUserSetting(ctx, uid, "app."+slug+"."+k, v); err != nil {
			t.Fatal(err)
		}
	}
}

type reportOut struct {
	Markdown     string `json:"markdown"`
	Empty        bool   `json:"empty"`
	Message      string `json:"message"`
	Error        string `json:"error"`
	NotConnected string `json:"not_connected"`
}

func runReport(t *testing.T, h *harness, tok, kind string, body any) (int, reportOut) {
	t.Helper()
	var out reportOut
	code := h.do(tok, http.MethodPost, "/api/v1/tools/report/"+kind, body, &out)
	return code, out
}

func wantLines(t *testing.T, md string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !strings.Contains(md, l) {
			t.Errorf("missing %q in:\n%s", l, md)
		}
	}
}

func kumaTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.000") }

func fakeKuma(t *testing.T, now time.Time) *httptest.Server {
	return fakeServer(t, map[string]http.HandlerFunc{
		"/api/status-page/home": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"publicGroupList": []any{
				map[string]any{"name": "Services", "monitorList": []any{
					map[string]any{"id": 1, "name": "NAS"},
					map[string]any{"id": 2, "name": "Jellyfin"},
					map[string]any{"id": 3, "name": "Immich"},
				}},
			}})
		},
		"/api/status-page/heartbeat/home": func(w http.ResponseWriter, r *http.Request) {
			beat := func(st int, ago time.Duration) map[string]any {
				return map[string]any{"status": st, "time": kumaTime(now.Add(-ago)), "msg": "", "ping": 12}
			}
			fakeJSON(w, map[string]any{
				"heartbeatList": map[string]any{
					"1": []any{beat(1, 3*time.Hour), beat(0, 2*time.Hour), beat(0, 110*time.Minute), beat(1, 90*time.Minute), beat(1, time.Minute)},
					"2": []any{beat(1, time.Hour), beat(0, 30*time.Minute), beat(0, time.Minute)},
					"3": []any{beat(1, time.Hour), beat(1, time.Minute)},
				},
				"uptimeList": map[string]any{"1_24": 0.98, "2_24": 0.9, "3_24": 1},
			})
		},
	})
}

func fakeScrutiny(t *testing.T) *httptest.Server {
	return fakeServer(t, map[string]http.HandlerFunc{
		"/api/summary": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"success": true, "data": map[string]any{"summary": map[string]any{
				"0x5000c500a1": map[string]any{
					"device": map[string]any{"wwn": "0x5000c500a1", "device_name": "sda", "host_id": "", "device_status": 0, "model_name": "Samsung SSD", "serial_number": "S1", "capacity": 1e12},
					"smart":  map[string]any{"temp": 38, "collector_date": "2026-10-20T03:00:00Z", "power_on_hours": 1200},
				},
				"0x5000c500b2": map[string]any{
					"device": map[string]any{"wwn": "0x5000c500b2", "device_name": "ada1", "host_id": "nas", "device_status": 2, "model_name": "WD Red", "serial_number": "WD-X", "capacity": 8e12},
					"smart":  map[string]any{"temp": 51, "collector_date": "2026-10-20T03:00:00Z", "power_on_hours": 30000},
				},
			}}})
		},
	})
}

func fakeBackrest(t *testing.T, now time.Time) *httptest.Server {
	ms := func(ago time.Duration) string { return strconv.FormatInt(now.Add(-ago).UnixMilli(), 10) }
	auth := func(f http.HandlerFunc) http.HandlerFunc { return needBearer("br-session", f) }
	return fakeServer(t, map[string]http.HandlerFunc{
		"/v1.Authentication/Login": func(w http.ResponseWriter, r *http.Request) {
			var in struct{ Username, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if r.Method != http.MethodPost || in.Username != "admin" || in.Password != "br-pass" {
				http.Error(w, `{"code":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			fakeJSON(w, map[string]any{"token": "br-session"})
		},
		"/v1.Backrest/GetConfig": auth(func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{
				"instance": "homelab",
				"repos":    []any{map[string]any{"id": "local", "guid": "g-local", "password": "secret", "checkPolicy": map[string]any{"schedule": map[string]any{"maxFrequencyDays": 7}}}},
				"plans": []any{
					map[string]any{"id": "daily", "repo": "local", "schedule": map[string]any{"cron": "0 3 * * *", "clock": "CLOCK_LOCAL"}},
					map[string]any{"id": "photos", "repo": "local", "schedule": map[string]any{"maxFrequencyDays": 1}},
				},
			})
		}),
		"/v1.Backrest/GetSummaryDashboard": auth(func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"planSummaries": []any{map[string]any{"id": "daily", "nextBackupTimeMs": ms(-time.Hour)}}})
		}),
		"/v1.Backrest/GetOperations": auth(func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Selector struct{ RepoGuid, InstanceId string }
				LastN    string
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Selector.RepoGuid != "g-local" || in.Selector.InstanceId != "homelab" {
				http.Error(w, "bad selector", http.StatusBadRequest)
				return
			}
			fakeJSON(w, map[string]any{"operations": []any{
				map[string]any{"id": "1", "planId": "daily", "repoId": "local", "unixTimeStartMs": ms(5 * time.Hour), "unixTimeEndMs": ms(5*time.Hour - 10*time.Minute), "status": "STATUS_SUCCESS",
					"operationBackup": map[string]any{"lastStatus": map[string]any{"summary": map[string]any{"totalBytesProcessed": "1000"}}}},
				map[string]any{"id": "2", "planId": "photos", "repoId": "local", "unixTimeStartMs": ms(3 * time.Hour), "unixTimeEndMs": ms(3*time.Hour - time.Minute), "status": 4, "operationBackup": map[string]any{}},
				map[string]any{"id": "3", "planId": "photos", "repoId": "local", "unixTimeStartMs": ms(27 * time.Hour), "unixTimeEndMs": ms(26 * time.Hour), "status": "STATUS_SUCCESS", "operationBackup": map[string]any{}},
				map[string]any{"id": "4", "planId": "daily", "repoId": "local", "unixTimeStartMs": ms(2 * time.Hour), "status": "STATUS_SUCCESS", "operationStats": map[string]any{"stats": map[string]any{"totalSize": "123000000000", "snapshotCount": "40"}}},
			}})
		}),
	})
}

func fakeGlances(t *testing.T) *httptest.Server {
	return fakeServer(t, map[string]http.HandlerFunc{
		"/api/4/quicklook": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"cpu": 23.4, "cpu_name": "Intel N100", "mem": 40})
		},
		"/api/4/load": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"min1": 1.234, "min5": 1, "min15": 1, "cpucore": 4})
		},
		"/api/4/mem": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"total": 16e9, "available": 10e9, "percent": 37.5, "used": 6e9})
		},
		"/api/4/fs": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, []any{
				map[string]any{"mnt_point": "/", "size": 500e9, "free": 120.3e9, "percent": 75.9, "device_name": "/dev/nvme0n1p2"},
				map[string]any{"mnt_point": "/mnt/storage", "size": 2e12, "free": 1e12, "percent": 50},
			})
		},
		"/api/4/uptime": func(w http.ResponseWriter, r *http.Request) { fakeJSON(w, "20 days, 5:35:18") },
		"/api/4/processlist": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, []any{
				map[string]any{"pid": 101, "name": "jellyfin", "cmdline": []string{"/usr/bin/jellyfin"}, "cpu_percent": 55.5, "memory_percent": 8, "memory_info": map[string]any{"rss": 1.5e9, "vms": 3e9}},
				map[string]any{"pid": 202, "name": "postgres", "cmdline": "postgres: writer", "cpu_percent": 2.25, "memory_percent": 12, "memory_info": map[string]any{"rss": 2.5e9}},
				map[string]any{"pid": 303, "name": "ff|mpeg", "cmdline": []string{}, "cpu_percent": "?", "memory_info": []any{300e6, 900e6}},
			})
		},
	})
}

func fakeBeszel(t *testing.T) *httptest.Server {
	return fakeServer(t, map[string]http.HandlerFunc{
		"/api/collections/users/auth-with-password": func(w http.ResponseWriter, r *http.Request) {
			var in struct{ Identity, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Identity != "me@example.com" || in.Password != "bz-pass" {
				http.Error(w, `{"message":"Failed to authenticate."}`, http.StatusBadRequest)
				return
			}
			fakeJSON(w, map[string]any{"token": "bz-token", "record": map[string]any{"id": "u1"}})
		},
		"/api/collections/systems/records": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "bz-token" {
				http.Error(w, "{}", http.StatusUnauthorized)
				return
			}
			fakeJSON(w, map[string]any{"page": 1, "perPage": 500, "totalItems": 2, "items": []any{
				map[string]any{"id": "abc123", "name": "nas", "host": "10.0.0.5", "status": "up", "info": map[string]any{"h": "truenas", "cpu": 4.6, "mp": 61.2, "dp": 48.7, "dt": 44.5, "u": 302400, "v": "0.12.0"}},
				map[string]any{"id": "def456", "name": "pi", "host": "10.0.0.9", "status": "down", "info": map[string]any{}},
			}})
		},
	})
}

// ---- tests ----

func TestReportNotConnected(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("a@example.com")
	for _, kind := range []string{"snapshot", "attention", "media", "spent", "fuel", "hosts", "processes", "habits"} {
		code, out := runReport(t, h, tok, kind, nil)
		if code != http.StatusPreconditionFailed || out.NotConnected == "" {
			t.Errorf("%s: want 412 with not_connected, got %d %+v", kind, code, out)
		}
	}
	if code, _ := runReport(t, h, tok, "nonsense", nil); code != http.StatusNotFound {
		t.Errorf("unknown kind: want 404, got %d", code)
	}
}

func TestReportAttention(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	now := time.Now()
	connectSvc(t, h, u.ID, "kuma", fakeKuma(t, now).URL, "home", "")
	connectSvc(t, h, u.ID, "scrutiny", fakeScrutiny(t).URL, "", "")
	connectSvc(t, h, u.ID, "backrest", fakeBackrest(t, now).URL, "", "br-pass")

	code, out := runReport(t, h, tok, "attention", map[string]any{"days": 7})
	if code != http.StatusOK || out.Empty {
		t.Fatalf("attention: %d %+v", code, out)
	}
	md := out.Markdown
	wantLines(t, md,
		"**What needs attention** · ",
		"**Monitors:** 1 down: Jellyfin",
		"| Monitor | Down from | For |",
		"| Jellyfin | ",
		"| still down |",
		"| 30 min |",
		"**Disks:** 1 with warnings",
		"| Disk | Host | Problem | Temp |",
		"| ada1 | nas | Over Scrutiny's thresholds | 51 °C |",
		"**Backups:** 1 of 2 plans need a look",
		"| Plan | State | Last good backup |",
		"| photos | Failed | ",
	)
	if strings.Contains(md, "| daily |") {
		t.Errorf("a current plan is listed:\n%s", md)
	}
	if strings.Contains(md, "br-pass") || strings.Contains(md, "secret") {
		t.Errorf("a secret leaked:\n%s", md)
	}
	// The newest outage comes first.
	if strings.Index(md, "| Jellyfin |") > strings.Index(md, "| NAS |") {
		t.Errorf("outages not newest first:\n%s", md)
	}
}

func TestReportAttentionFailingSource(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	broken := fakeServer(t, map[string]http.HandlerFunc{
		"/": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream exploded", http.StatusInternalServerError)
		},
	})
	connectSvc(t, h, u.ID, "kuma", broken.URL, "home", "")
	connectSvc(t, h, u.ID, "scrutiny", fakeScrutiny(t).URL, "", "")

	code, out := runReport(t, h, tok, "attention", nil)
	if code != http.StatusOK {
		t.Fatalf("attention: %d %+v", code, out)
	}
	wantLines(t, out.Markdown, "**Disks:** 1 with warnings", "_Uptime Kuma did not answer: HTTP 500: upstream exploded_")
	if strings.Contains(out.Markdown, "**Monitors:**") {
		t.Errorf("monitors written from a failed source:\n%s", out.Markdown)
	}

	// Only the broken one connected: the failure itself, not a block.
	h2 := newHarness(t)
	u2, tok2 := h2.user("b@example.com")
	connectSvc(t, h2, u2.ID, "kuma", broken.URL, "home", "")
	code, out = runReport(t, h2, tok2, "attention", nil)
	if code != http.StatusBadGateway || !strings.Contains(out.Error, "HTTP 500") {
		t.Fatalf("all failed: want 502, got %d %+v", code, out)
	}
}

func TestReportSnapshot(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	now := time.Now()
	connectSvc(t, h, u.ID, "glances", fakeGlances(t).URL, "", "")
	connectSvc(t, h, u.ID, "beszel", fakeBeszel(t).URL, "me@example.com", "bz-pass")
	connectSvc(t, h, u.ID, "kuma", fakeKuma(t, now).URL, "home", "")
	connectSvc(t, h, u.ID, "scrutiny", fakeScrutiny(t).URL, "", "")
	connectSvc(t, h, u.ID, "backrest", fakeBackrest(t, now).URL, "admin", "br-pass")

	code, out := runReport(t, h, tok, "snapshot", nil)
	if code != http.StatusOK || out.Empty {
		t.Fatalf("snapshot: %d %+v", code, out)
	}
	wantLines(t, out.Markdown,
		"**Homelab snapshot** · ",
		"| What | Now |\n|---|---|",
		"| Server | CPU 23% (load 1.23 on 4 cores) · RAM 6.0 GB of 16.0 GB (38%) · up 20d 5h |",
		"| Storage | / 76% used (120.3 GB free) · hottest disk 51 °C |",
		"| nas | CPU 5% · RAM 61% · disk 49% · 45 °C · up 3d 12h |",
		"| pi | **offline** |",
		"| Monitors | 2 up, **1 down**: Jellyfin |",
		"| Disks | **1 of 2 with warnings**: nas:ada1 |",
		"| Backups | 2 plans, 123.0 GB stored · **photos: failed** |",
	)
	if strings.Contains(out.Markdown, "did not answer") {
		t.Errorf("a source failed:\n%s", out.Markdown)
	}
}

func TestReportMedia(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	now := time.Now().UTC()
	iso := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339) }
	js := fakeServer(t, map[string]http.HandlerFunc{
		"/stats/getPlaybackActivity": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-api-token") != "js-key" {
				http.Error(w, "{}", http.StatusUnauthorized)
				return
			}
			fakeJSON(w, map[string]any{"current_page": 1, "pages": 1, "size": 50, "results": []any{
				map[string]any{"Id": "a1", "UserName": "aleks", "NowPlayingItemName": "Pilot", "SeriesName": "Severance", "PlaybackDuration": "2700", "ActivityDateInserted": iso(24 * time.Hour)},
				map[string]any{"Id": "a2", "UserName": "aleks", "NowPlayingItemName": "Trailer", "PlaybackDuration": 90, "ActivityDateInserted": iso(time.Hour)},
				map[string]any{"Id": "a3", "UserName": "aleks", "NowPlayingItemName": "Old film", "PlaybackDuration": 6000, "ActivityDateInserted": iso(20 * 24 * time.Hour)},
				map[string]any{"Id": "a4", "UserName": "aleks", "NowPlayingItemName": "Dune", "SeriesName": nil, "PlaybackDuration": 9000, "ActivityDateInserted": iso(48 * time.Hour)},
			}})
		},
	})
	pod := fakeServer(t, map[string]http.HandlerFunc{
		"/api/v1/history": needBearer("pod-token", func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"total": 3, "episodes": []any{
				map[string]any{"id": 1, "title": "The Daily: Monday", "podcast_title": "The Daily", "duration": 1800, "play_position": 1800, "completed": true, "last_played_at": iso(3 * time.Hour)},
				map[string]any{"id": 2, "title": "Hardcore History 70", "podcast_title": "Hardcore History", "duration": 20000, "play_position": 5000, "completed": false, "last_played_at": iso(30 * time.Hour)},
				map[string]any{"id": 3, "title": "Way back", "podcast_title": "X", "duration": 100, "completed": true, "last_played_at": iso(30 * 24 * time.Hour)},
			}})
		}),
	})
	abs := fakeServer(t, map[string]http.HandlerFunc{
		"/api/me/items-in-progress": needBearer("abs-token", func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"libraryItems": []any{
				map[string]any{"id": "li1", "mediaType": "book", "media": map[string]any{"metadata": map[string]any{"title": "Project Hail Mary", "authorName": "Andy Weir"}}},
				map[string]any{"id": "li1", "mediaType": "book", "media": map[string]any{"metadata": map[string]any{"title": "Project Hail Mary", "authorName": "Andy Weir"}}},
				map[string]any{"id": "li2", "mediaType": "book", "media": map[string]any{"metadata": map[string]any{"title": "Dune", "authors": []any{map[string]any{"name": "Frank Herbert"}}}}},
			}})
		}),
	})
	connectSvc(t, h, u.ID, "jellystat", js.URL, "", "js-key")
	connectApp(t, h, u.ID, "podcasts", pod.URL, "pod-token")
	connectSvc(t, h, u.ID, "audiobookshelf", abs.URL, "", "abs-token")

	code, out := runReport(t, h, tok, "media", nil)
	if code != http.StatusOK || out.Empty {
		t.Fatalf("media: %d %+v", code, out)
	}
	md := out.Markdown
	wantLines(t, md,
		"**Watched and listened** · ",
		"Since "+repStampDay(time.Now().Add(-7*24*time.Hour))+".",
		"**Watched**\n\n| What |  | When |\n|---|---|---|",
		"| Pilot | Severance · 45 min | "+repStampDay(now.Add(-24*time.Hour))+" |",
		"| Dune | 150 min | ",
		"**Listened**",
		"| The Daily: Monday | The Daily · finished | ",
		"| Hardcore History 70 | Hardcore History · 25% | ",
		"**Listening to**",
		"| Project Hail Mary | Andy Weir |  |",
		"| Dune | Frank Herbert |  |",
	)
	for _, gone := range []string{"Trailer", "Old film", "Way back"} {
		if strings.Contains(md, gone) {
			t.Errorf("%q should be left out:\n%s", gone, md)
		}
	}
	if strings.Count(md, "Project Hail Mary") != 1 {
		t.Errorf("the same book twice:\n%s", md)
	}
	// Newest first within a kind.
	if strings.Index(md, "| Pilot |") > strings.Index(md, "| Dune | 150") {
		t.Errorf("watched not newest first:\n%s", md)
	}
}

func TestReportSpent(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	today := time.Now().Format("2006-01-02")
	monthStart := time.Now().Format("2006-01") + "-01"
	var query string
	money := fakeServer(t, map[string]http.HandlerFunc{
		"/api/v1/transactions": needBearer("money-token", func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			fakeJSON(w, map[string]any{"has_more": false, "totals": map[string]any{"count": 5}, "items": []any{
				map[string]any{"id": 1, "kind": "expense", "date": today, "amount": 4520, "description": "Konzum", "category_name": "Groceries", "currency": "BAM", "splits": []any{}},
				map[string]any{"id": 2, "kind": "expense", "date": today, "amount": 123456, "description": "Rent", "category_name": "Housing", "currency": "BAM", "splits": nil},
				map[string]any{"id": 3, "kind": "expense", "date": monthStart, "amount": 3000, "description": "Market", "currency": "BAM", "splits": []any{
					map[string]any{"amount": 2000, "note": "Fruit", "category_name": "Groceries"},
					map[string]any{"amount": 1000, "note": "", "category_name": ""},
				}},
				map[string]any{"id": 4, "kind": "expense", "date": monthStart, "amount": 999, "description": "Netflix", "category_name": "Subscriptions", "currency": "EUR"},
			}})
		}),
	})
	connectApp(t, h, u.ID, "money", money.URL, "money-token")

	code, out := runReport(t, h, tok, "spent", nil)
	if code != http.StatusOK || out.Empty {
		t.Fatalf("spent: %d %+v", code, out)
	}
	for _, q := range []string{"kind=expense", "from=" + monthStart, "to=" + today} {
		if !strings.Contains(query, q) {
			t.Errorf("query %q lacks %q", query, q)
		}
	}
	wantLines(t, out.Markdown,
		"**Spending** · ",
		"This month so far: **1,309.76 BAM** over 4 transactions.",
		"| Category | Spent | Count |\n|---|---|---|\n| Housing | 1,234.56 BAM | 1 |\n| Groceries | 65.20 BAM | 2 |\n| Uncategorised | 10.00 BAM | 1 |",
		"**Largest**",
		"| Date | What | Amount |\n|---|---|---|\n| "+today+" | Rent | 1,234.56 BAM |\n| "+today+" | Konzum | 45.20 BAM |\n| "+monthStart+" | Fruit | 20.00 BAM |\n| "+monthStart+" | Market | 10.00 BAM |",
		"1 in other currencies left out.",
	)

	// from/to name the stretch.
	_, out = runReport(t, h, tok, "spent", map[string]any{"from": "2026-09-01", "to": "2026-09-30"})
	wantLines(t, out.Markdown, "1 Sep 2026 – 30 Sep 2026: **")
	if !strings.Contains(query, "from=2026-09-01") || !strings.Contains(query, "to=2026-09-30") {
		t.Errorf("stretch not asked for: %s", query)
	}
}

func TestReportFuel(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	now := time.Now()
	today := now.Format("2006-01-02")
	monthsAgo := now.AddDate(0, -3, 0).Format("2006-01-02")
	longAgo := now.AddDate(-2, 0, 0).Format("2006-01-02")
	garage := fakeServer(t, map[string]http.HandlerFunc{
		"/api/v1/vehicles": needBearer("garage-token", func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, []any{
				map[string]any{"id": 1, "name": "Golf", "year": 2019, "make": "Volkswagen", "model": "Golf", "currency": "EUR"},
				map[string]any{"id": 2, "name": "Old bike", "year": 0, "make": "", "model": "", "currency": "EUR"},
			})
		}),
		"/api/v1/vehicles/1/fuel": needBearer("garage-token", func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, []any{
				map[string]any{"id": 1, "date": longAgo, "odometer": 100000, "volume": 40, "cost": 5000, "currency": "EUR", "economy": 0},
				map[string]any{"id": 2, "date": monthsAgo, "odometer": 120500, "volume": 42.5, "cost": 6375, "currency": "EUR", "economy": 6.42},
				map[string]any{"id": 3, "date": today, "odometer": 121234.4, "volume": 38.3, "cost": 5800, "currency": "EUR", "economy": 5.9},
				map[string]any{"id": 4, "date": today, "odometer": 121300, "volume": 10, "cost": 2000, "currency": "BAM", "economy": 0},
			})
		}),
		"/api/v1/vehicles/1/records": needBearer("garage-token", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("kind") != "service,repair" {
				http.Error(w, "kind", http.StatusBadRequest)
				return
			}
			fakeJSON(w, []any{
				map[string]any{"id": 9, "date": today, "kind": "service", "description": "Oil change", "cost": 12000, "currency": "EUR"},
				map[string]any{"id": 8, "date": monthsAgo, "kind": "repair", "description": "", "cost": 30000, "currency": "EUR"},
			})
		}),
		"/api/v1/vehicles/2/fuel":    needBearer("garage-token", func(w http.ResponseWriter, r *http.Request) { fakeJSON(w, []any{}) }),
		"/api/v1/vehicles/2/records": needBearer("garage-token", func(w http.ResponseWriter, r *http.Request) { fakeJSON(w, []any{}) }),
	})
	connectApp(t, h, u.ID, "garage", garage.URL, "garage-token")

	code, out := runReport(t, h, tok, "fuel", nil)
	if code != http.StatusOK || out.Empty {
		t.Fatalf("fuel: %d %+v", code, out)
	}
	md := out.Markdown
	wantLines(t, md,
		"**Vehicle log** · ",
		"**2019 Volkswagen Golf**",
		"- **This month:** fuel 58.00 EUR (38.3 l, 1 fill-up) · service and repairs 120.00 EUR — **178.00 EUR** (1 in other currencies left out)",
		"- **Last 12 months:** fuel 121.75 EUR (80.8 l, 2 fill-ups) · service and repairs 420.00 EUR — **541.75 EUR** (1 in other currencies left out)",
		"| Date | Odometer | Fuel | Cost | Economy |",
		"| "+today+" | 121,234 | 38.3 | 58.00 EUR | 5.9 |",
		"| "+longAgo+" | 100,000 | 40 | 50.00 EUR | — |",
		"| Date | Work | Kind | Cost |",
		"| "+today+" | Oil change | Service | 120.00 EUR |",
		"| "+monthsAgo+" | Repair | Repair | 300.00 EUR |",
	)
	if strings.Contains(md, "Old bike") {
		t.Errorf("a vehicle without records is written:\n%s", md)
	}
}

func TestReportHosts(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	var findQuery string
	ag := fakeServer(t, map[string]http.HandlerFunc{
		"/control/stats": func(w http.ResponseWriter, r *http.Request) {
			if user, pass, ok := r.BasicAuth(); !ok || user != "admin" || pass != "ag-pass" {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			fakeJSON(w, map[string]any{"time_units": "hours", "num_dns_queries": 123456, "num_blocked_filtering": 7890, "avg_processing_time": 0.004,
				"top_clients": []any{map[string]any{"192.168.1.10": 50000}, map[string]any{"10.220.28.4": 30000}, map[string]any{"192.168.1.77": 1234}}})
		},
		"/control/clients/find": func(w http.ResponseWriter, r *http.Request) {
			findQuery = r.URL.RawQuery
			fakeJSON(w, []any{
				map[string]any{"192.168.1.10": map[string]any{"name": "macbook", "ids": []string{"192.168.1.10"}}},
				map[string]any{"10.220.28.4": map[string]any{"name": "", "ids": []string{}}},
				map[string]any{"192.168.1.77": []any{map[string]any{"name": "iphone"}}},
			})
		},
	})
	connectSvc(t, h, u.ID, "adguard", ag.URL, "admin", "ag-pass")

	code, out := runReport(t, h, tok, "hosts", nil)
	if code != http.StatusOK || out.Empty {
		t.Fatalf("hosts: %d %+v", code, out)
	}
	if !strings.Contains(findQuery, "ip0=192.168.1.10") || !strings.Contains(findQuery, "ip2=192.168.1.77") {
		t.Errorf("names asked for wrongly: %s", findQuery)
	}
	wantLines(t, out.Markdown,
		"**Hosts on the network** · ",
		"AdGuard's busiest clients, 123,456 queries in its window, 6.4% blocked.",
		"| Name | Address | Queries |\n|---|---|---|\n| macbook | 192.168.1.10 | 50,000 |\n| docker container | 10.220.28.4 | 30,000 |\n| iphone | 192.168.1.77 | 1,234 |",
	)
}

func TestReportHostsEmpty(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	ag := fakeServer(t, map[string]http.HandlerFunc{
		"/control/stats": func(w http.ResponseWriter, r *http.Request) {
			fakeJSON(w, map[string]any{"num_dns_queries": 0, "num_blocked_filtering": 0, "top_clients": []any{}})
		},
	})
	connectSvc(t, h, u.ID, "adguard", ag.URL, "admin", "ag-pass")
	code, out := runReport(t, h, tok, "hosts", nil)
	if code != http.StatusOK || !out.Empty || out.Markdown != "" || out.Message != "There was nothing to write down." {
		t.Fatalf("want empty, got %d %+v", code, out)
	}
}

func TestReportProcesses(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	connectSvc(t, h, u.ID, "glances", fakeGlances(t).URL, "", "")
	code, out := runReport(t, h, tok, "processes", nil)
	if code != http.StatusOK || out.Empty {
		t.Fatalf("processes: %d %+v", code, out)
	}
	wantLines(t, out.Markdown,
		"**Server processes** · ",
		"**By CPU**\n\n| Process | PID | CPU | Memory |\n|---|---|---|---|\n| jellyfin | 101 | 55.5% | 1.5 GB |\n| postgres | 202 | 2.2% | 2.5 GB |\n| ff\\|mpeg | 303 | 0.0% | 300 MB |",
		"**By memory**\n\n| Process | PID | Memory | CPU |\n|---|---|---|---|\n| postgres | 202 | 2.5 GB | 2.2% |\n| jellyfin | 101 | 1.5 GB | 55.5% |",
	)
}

func TestReportHabits(t *testing.T) {
	h := newHarness(t)
	u, tok := h.user("a@example.com")
	today := time.Now()
	d1 := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -2)
	d2 := d1.AddDate(0, 0, 1)
	day := func(d time.Time) string { return d.Format("2006-01-02") }
	pages := map[string]any{
		day(d1): map[string]any{"date": day(d1), "weekday": 1, "journal": "Long walk.", "journal_bad": "", "important": "",
			"habits": []any{
				map[string]any{"id": 1, "name": "Read", "days": "1234567", "status": 1, "scheduled": true, "streak": 3},
				map[string]any{"id": 2, "name": "Gym", "days": "135", "status": -1, "scheduled": true},
				map[string]any{"id": 3, "name": "Stretch", "days": "246", "status": 0, "scheduled": false},
			},
			"trackers": []any{
				map[string]any{"id": 1, "name": "Mood", "kind": "scale", "step": 1, "unit": "", "value": 4},
				map[string]any{"id": 2, "name": "Weight", "kind": "number", "step": 0.1, "unit": "kg", "value": 81.2},
				map[string]any{"id": 3, "name": "Alcohol", "kind": "yesno", "step": 1, "unit": "", "value": 0},
			}},
		day(d2): map[string]any{"date": day(d2), "weekday": 2, "journal": "", "journal_bad": "Slept badly.", "important": "Moved in.",
			"habits": []any{
				map[string]any{"id": 1, "name": "Read", "status": 0, "scheduled": true},
				map[string]any{"id": 2, "name": "Gym", "status": 1, "scheduled": true},
			},
			"trackers": []any{
				map[string]any{"id": 1, "name": "Mood", "kind": "scale", "step": 1, "value": 3},
				map[string]any{"id": 2, "name": "Weight", "kind": "number", "step": 0.1, "unit": "kg", "value": 81},
				map[string]any{"id": 3, "name": "Alcohol", "kind": "yesno", "value": nil},
			}},
	}
	var mu sync.Mutex
	asked := map[string]bool{}
	hb := fakeServer(t, map[string]http.HandlerFunc{
		"/api/v1/days/{date}": needBearer("habits-token", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			asked[r.PathValue("date")] = true
			mu.Unlock()
			p, ok := pages[r.PathValue("date")]
			if !ok {
				http.Error(w, "unexpected day", http.StatusBadRequest)
				return
			}
			fakeJSON(w, p)
		}),
	})
	connectApp(t, h, u.ID, "habits", hb.URL, "habits-token")

	code, out := runReport(t, h, tok, "habits", map[string]any{"from": day(d1), "to": day(d2)})
	if code != http.StatusOK || out.Empty {
		t.Fatalf("habits: %d %+v", code, out)
	}
	if len(asked) != 2 {
		t.Errorf("asked for %v", asked)
	}
	want := fmt.Sprintf("## Habits · %s – %s\n\n", d1.Format("2 Jan 2006"), d2.Format("2 Jan 2006")) +
		"### The period in numbers\n\n" +
		"- **Read**: ✓ 1 · ✗ 0 · ○ 1 · 100 %\n" +
		"- **Gym**: ✓ 1 · ✗ 1 · 50 %\n" +
		"- **Mood**: avg 3.5 · 3.0–4.0 · 2 days\n" +
		"- **Weight**: avg 81.1 kg · 81.0–81.2 kg · 2 days\n" +
		"- **Alcohol**: yes 0 · no 1\n\n" +
		"### " + d2.Format("Mon 2 Jan") + "\n\n" +
		"- ○ Read · ✓ Gym\n" +
		"- Mood 3 · Weight 81.0 kg\n" +
		"- **Important:** Moved in.\n" +
		"- − Slept badly.\n\n" +
		"### " + d1.Format("Mon 2 Jan") + "\n\n" +
		"- ✓ Read · ✗ Gym\n" +
		"- Mood 4 · Weight 81.2 kg · Alcohol No\n" +
		"- Long walk.\n"
	if out.Markdown != want {
		t.Errorf("habits markdown:\n%s\nwant:\n%s", out.Markdown, want)
	}

	// The default is the last seven days, ending today.
	asked = map[string]bool{}
	runReport(t, h, tok, "habits", nil)
	if len(asked) != 7 || !asked[time.Now().Format("2006-01-02")] {
		t.Errorf("default days: %v", asked)
	}
}

func TestReportCronPeriod(t *testing.T) {
	now := float64(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC).UnixMilli())
	for expr, hours := range map[string]float64{"0 3 * * *": 24, "*/15 * * * *": 0.25, "0 4 * * SUN": 168, "30 2 1 * *": 31 * 24} {
		p, ok := repNominalPeriod(repSchedule{cron: expr}, now)
		if !ok || p != hours*repHourMs {
			t.Errorf("%s: got %v h (%v), want %v h", expr, p/repHourMs, ok, hours)
		}
	}
	if _, ok := repNominalPeriod(repSchedule{cron: "nonsense"}, now); ok {
		t.Error("a bad cron has a period")
	}
}
