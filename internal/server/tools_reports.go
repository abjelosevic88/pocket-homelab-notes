package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Reports: blocks of Markdown written from what the homelab knows, put into a
// note from the editor's tools. The formats follow the app's (NoteReports,
// HomelabSnapshot and HabitsNotesExport in homelab-app). Each report reads its
// services at once; one that fails gets a line saying so and the others still
// write. Every table says what it is and when it was written, so a note read a
// year later still makes sense.

// reportReq is the optional body: how many days back, or a stretch of days.
type reportReq struct {
	Days int    `json:"days"`
	From string `json:"from"`
	To   string `json:"to"`
}

// reportSource is one service a report reads. run answers errNotConnected
// when the person has not connected it.
type reportSource struct {
	name    string
	timeout time.Duration
	run     func(ctx context.Context) error
}

type reportFunc func(s *Server, ctx context.Context, uid int64, in reportReq, now time.Time) (markdown string, fails []string, err error)

var reportKinds = map[string]reportFunc{
	"snapshot":  (*Server).reportSnapshot,
	"attention": (*Server).reportAttention,
	"media":     (*Server).reportMedia,
	"spent":     (*Server).reportSpent,
	"fuel":      (*Server).reportFuel,
	"hosts":     (*Server).reportHosts,
	"processes": (*Server).reportProcesses,
	"habits":    (*Server).reportHabits,
}

func (s *Server) handleToolReport(w http.ResponseWriter, r *http.Request) {
	build := reportKinds[chi.URLParam(r, "kind")]
	if build == nil {
		writeErr(w, http.StatusNotFound, "unknown report")
		return
	}
	var in reportReq
	if r.Body != nil {
		if err := decode(r, &in); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	if in.Days <= 0 {
		in.Days = 7
	}
	in.Days = min(in.Days, 366)
	for _, d := range []string{in.From, in.To} {
		if d != "" {
			if _, err := time.ParseInLocation("2006-01-02", d, time.Local); err != nil {
				writeErr(w, http.StatusBadRequest, "from and to must be YYYY-MM-DD")
				return
			}
		}
	}
	md, fails, err := build(s, r.Context(), me(r), in, time.Now().In(time.Local))
	if err != nil {
		toolErr(w, err)
		return
	}
	if strings.TrimSpace(md) == "" {
		msg := "There was nothing to write down."
		if len(fails) > 0 {
			msg += " " + strings.Join(fails, ". ") + "."
		}
		writeJSON(w, http.StatusOK, map[string]any{"markdown": "", "empty": true, "message": msg})
		return
	}
	if len(fails) > 0 {
		md = strings.TrimRight(md, "\n") + "\n\n"
		for _, f := range fails {
			md += "_" + strings.ReplaceAll(f, "_", "\\_") + "_\n"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"markdown": md, "empty": false})
}

// gather runs the sources at once, each with its own deadline. It answers the
// lines for the sources that failed; errNotConnected when none is connected;
// the failure itself when every connected one failed.
func gather(ctx context.Context, srcs []reportSource) ([]string, error) {
	errs := make([]error, len(srcs))
	var wg sync.WaitGroup
	for i, src := range srcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := src.timeout
			if t == 0 {
				t = 10 * time.Second
			}
			cctx, cancel := context.WithTimeout(ctx, t)
			defer cancel()
			errs[i] = src.run(cctx)
		}()
	}
	wg.Wait()
	var fails []string
	var firstNC, firstErr error
	answered := 0
	for i, err := range errs {
		var nc errNotConnected
		switch {
		case err == nil:
			answered++
		case errors.As(err, &nc):
			if firstNC == nil {
				firstNC = err
			}
		default:
			if firstErr == nil {
				firstErr = err
			}
			fails = append(fails, serviceNames[srcs[i].name]+" did not answer: "+reportReason(srcs[i].name, err))
		}
	}
	switch {
	case answered > 0:
		return fails, nil
	case len(fails) == 1:
		return nil, firstErr
	case len(fails) > 1:
		return nil, errors.New(strings.Join(fails, "; "))
	case firstNC != nil:
		return nil, firstNC
	}
	return nil, nil
}

// repFirstErr is the first error of several parts, nil when all went well.
func repFirstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// reportReason is why a service failed, without its name twice.
func reportReason(name string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "it took too long"
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if n := serviceNames[name]; n != "" && strings.HasPrefix(msg, n) {
		msg = strings.TrimLeft(strings.TrimPrefix(msg, n), ": ")
	}
	return msg
}

// ---- Markdown and number helpers ----

func repStamp(t time.Time) string    { return t.In(time.Local).Format("2 Jan 2006, 15:04") }
func repStampDay(t time.Time) string { return t.In(time.Local).Format("2 Jan 2006") }

// repEscape keeps a pipe or a line break from ending a table cell.
func repEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}

func repTable(headers []string, rows [][]string) []string {
	line := func(cells []string) string {
		out := make([]string, len(cells))
		for i, c := range cells {
			out[i] = repEscape(c)
		}
		return "| " + strings.Join(out, " | ") + " |"
	}
	lines := []string{line(headers), "|" + strings.Repeat("---|", len(headers))}
	for _, r := range rows {
		lines = append(lines, line(r))
	}
	return lines
}

// repBlock is a titled block with the moment it was written.
func repBlock(title string, now time.Time, body []string) string {
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
	}
	return strings.Join(append([]string{"**" + title + "** · " + repStamp(now), ""}, body...), "\n") + "\n"
}

// repSize is a size in decimal units: 1.4 GB · 512 MB · 12 KB.
func repSize(b float64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", b/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%d MB", int64(math.Round(b/1e6)))
	}
	return fmt.Sprintf("%d KB", int64(math.Round(b/1e3)))
}

func repFixed(v float64, digits int) string { return strconv.FormatFloat(v, 'f', digits, 64) }
func repRound(v float64) string             { return strconv.FormatInt(int64(math.Round(v)), 10) }

// repGroup puts thousands separators into a formatted number.
func repGroup(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	ip, fp, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range ip {
		if i > 0 && (len(ip)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if hasFrac {
		return sign + b.String() + "." + fp
	}
	return sign + b.String()
}

func repCount(n int) string { return repGroup(strconv.Itoa(n)) }

// repNumber is a number with up to digits decimals and no trailing zeros.
func repNumber(v float64, digits int) string {
	s := strconv.FormatFloat(v, 'f', digits, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return repGroup(s)
}

// repCurrencyDecimals is the minor-unit exponent of a currency.
func repCurrencyDecimals(code string) int {
	switch strings.ToUpper(code) {
	case "BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "PYG", "RWF", "UGX", "UYI", "VND", "VUV", "XAF", "XOF", "XPF":
		return 0
	case "BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND":
		return 3
	}
	return 2
}

// repMoney writes minor units of a currency: "1,234.50 EUR".
func repMoney(minor int64, currency string) string {
	d := repCurrencyDecimals(currency)
	s := repGroup(strconv.FormatFloat(float64(minor)/math.Pow10(d), 'f', d, 64))
	if currency == "" {
		return s
	}
	return s + " " + strings.ToUpper(currency)
}

// Lenient readers for loosely typed JSON (numbers as strings, nulls).

func repObj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func repArr(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func repStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func repNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// repParseTime reads the times services write: ISO 8601, or "2026-09-20
// 10:00:00.000" in UTC (Uptime Kuma, PocketBase).
func repParseTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000Z", "2006-01-02 15:04:05Z", "2006-01-02 15:04:05.000", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var repUptimeRe = regexp.MustCompile(`(?:(\d+)\s+days?,\s*)?(\d+):(\d+):\d+`)

// repCompactUptime turns "20 days, 5:35:18" into "20d 5h" and "5:35:18" into "5h 35m".
func repCompactUptime(raw string) string {
	m := repUptimeRe.FindStringSubmatch(raw)
	if m == nil {
		return raw
	}
	days, _ := strconv.Atoi(m[1])
	hours, _ := strconv.Atoi(m[2])
	mins, _ := strconv.Atoi(m[3])
	return repUptimeParts(days, hours, mins)
}

func repUptimeParts(days, hours, mins int) string {
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// ---- Uptime Kuma ----

type repMonitor struct {
	name   string
	status int // 0 down, 1 up, 2 pending, 3 maintenance
}

type repBeat struct {
	at     time.Time
	status int
}

type repKuma struct {
	monitors []repMonitor
	slug     bool                 // a status page slug is set, so the beats are known
	beats    map[string][]repBeat // by monitor name, oldest first
}

func (k *repKuma) down() []string {
	var out []string
	for _, m := range k.monitors {
		if m.status == 0 {
			out = append(out, m.name)
		}
	}
	return out
}

var (
	repMetricRe = regexp.MustCompile(`^(monitor_status|monitor_response_time)\{([^}]*)\}\s+([\d.]+)`)
	repNameRe   = regexp.MustCompile(`monitor_name="((?:[^"\\]|\\.)*)"`)
)

// repLoadKuma reads the monitors (from /metrics with an API key, else the
// status page) and, when asked and a slug is set, their recent beats.
func (s *Server) repLoadKuma(ctx context.Context, c *svcCred, withBeats bool) (*repKuma, error) {
	slug := c.Field("STATUS_SLUG")
	key := firstNonEmpty(c.Field("API_KEY"), c.Key)
	if slug == "" && key == "" {
		return nil, errors.New("needs a status page slug or an API key (Settings → Integrations)")
	}
	out := &repKuma{slug: slug != "", beats: map[string][]repBeat{}}
	var pg struct {
		PublicGroupList []struct {
			MonitorList []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"monitorList"`
		} `json:"publicGroupList"`
	}
	var hb struct {
		HeartbeatList map[string][]struct {
			Status int    `json:"status"`
			Time   string `json:"time"`
		} `json:"heartbeatList"`
	}
	var pageErr error
	if slug != "" && (key == "" || withBeats) {
		esc := url.PathEscape(slug)
		var wg sync.WaitGroup
		var e1, e2 error
		wg.Add(2)
		go func() {
			defer wg.Done()
			e1 = s.callJSON(ctx, "kuma", http.MethodGet, c.API("/api/status-page/"+esc), nil, nil, &pg)
		}()
		go func() {
			defer wg.Done()
			e2 = s.callJSON(ctx, "kuma", http.MethodGet, c.API("/api/status-page/heartbeat/"+esc), nil, nil, &hb)
		}()
		wg.Wait()
		pageErr = repFirstErr(e1, e2)
		if pageErr == nil {
			for _, g := range pg.PublicGroupList {
				for _, m := range g.MonitorList {
					var list []repBeat
					for _, b := range hb.HeartbeatList[strconv.FormatInt(m.ID, 10)] {
						if t, ok := repParseTime(b.Time); ok {
							list = append(list, repBeat{at: t, status: b.Status})
						}
					}
					sort.SliceStable(list, func(i, j int) bool { return list[i].at.Before(list[j].at) })
					if len(list) > 0 {
						out.beats[m.Name] = list
					}
				}
			}
		}
	}
	if key != "" {
		// With a key, the beats are a bonus: a status page that fails leaves them out.
		var raw []byte
		if err := s.callJSON(ctx, "kuma", http.MethodGet, c.API("/metrics"), map[string]string{"Authorization": basicAuth("", key), "Accept": "text/plain"}, nil, &raw); err != nil {
			return nil, err
		}
		byName := map[string]int{}
		for _, line := range strings.Split(string(raw), "\n") {
			m := repMetricRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			n := repNameRe.FindStringSubmatch(m[2])
			if n == nil || n[1] == "" {
				continue
			}
			i, ok := byName[n[1]]
			if !ok {
				i = len(out.monitors)
				byName[n[1]] = i
				out.monitors = append(out.monitors, repMonitor{name: n[1], status: 1})
			}
			if m[1] == "monitor_status" {
				v, _ := strconv.ParseFloat(m[3], 64)
				out.monitors[i].status = int(v)
			}
		}
		return out, nil
	}
	if pageErr != nil {
		return nil, pageErr
	}
	for _, g := range pg.PublicGroupList {
		for _, m := range g.MonitorList {
			st := 2
			if b := out.beats[m.Name]; len(b) > 0 {
				st = b[len(b)-1].status
			}
			out.monitors = append(out.monitors, repMonitor{name: m.Name, status: st})
		}
	}
	return out, nil
}

// repOutage is one stretch of a monitor being down; until is zero while it still is.
type repOutage struct {
	monitor     string
	from, until time.Time
}

// repOutages finds the down stretches inside since…now from each monitor's
// beats. A stretch that started before since counts from since.
func repOutages(beats map[string][]repBeat, since time.Time) []repOutage {
	var out []repOutage
	later := func(a, b time.Time) time.Time {
		if a.After(b) {
			return a
		}
		return b
	}
	for name, list := range beats {
		var start time.Time
		for _, b := range list {
			if b.status == 0 {
				if start.IsZero() {
					start = b.at
				}
			} else if !start.IsZero() {
				if !b.at.Before(since) {
					out = append(out, repOutage{monitor: name, from: later(start, since), until: b.at})
				}
				start = time.Time{}
			}
		}
		if !start.IsZero() {
			out = append(out, repOutage{monitor: name, from: later(start, since)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].from.Equal(out[j].from) {
			return out[i].from.After(out[j].from)
		}
		return out[i].monitor < out[j].monitor
	})
	return out
}

// repDuration is how long something lasted: "12 min", "3 h 5 min", "2 d 4 h".
func repDuration(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m < 1:
		return "under a minute"
	case m < 60:
		return fmt.Sprintf("%d min", m)
	}
	h := m / 60
	if h < 48 {
		return fmt.Sprintf("%d h %d min", h, m%60)
	}
	return fmt.Sprintf("%d d %d h", h/24, h%24)
}

// ---- Scrutiny ----

type repDisk struct {
	name, host, serial string
	hostID             string // as Scrutiny has it ("" when none)
	status             int    // 0 passed; bit 1 SMART failed, bit 2 over Scrutiny's thresholds
	temp               *float64
}

func (s *Server) repLoadDisks(ctx context.Context, c *svcCred) ([]repDisk, error) {
	var body struct {
		Data struct {
			Summary map[string]struct {
				Device struct {
					DeviceName   string `json:"device_name"`
					HostID       string `json:"host_id"`
					DeviceStatus int    `json:"device_status"`
					SerialNumber string `json:"serial_number"`
				} `json:"device"`
				Smart *struct {
					Temp *float64 `json:"temp"`
				} `json:"smart"`
			} `json:"summary"`
		} `json:"data"`
	}
	if err := s.callJSON(ctx, "scrutiny", http.MethodGet, c.API("/api/summary"), nil, nil, &body); err != nil {
		return nil, err
	}
	out := []repDisk{}
	for _, e := range body.Data.Summary {
		d := repDisk{name: firstNonEmpty(e.Device.DeviceName, "?"), hostID: e.Device.HostID, host: firstNonEmpty(e.Device.HostID, "server"), serial: e.Device.SerialNumber, status: e.Device.DeviceStatus}
		if e.Smart != nil {
			d.temp = e.Smart.Temp
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].host != out[j].host {
			return out[i].host < out[j].host
		}
		return out[i].name < out[j].name
	})
	return out, nil
}

// ---- Backrest (a port of the app's loadBackupHealth) ----

type repBackupPlan struct {
	id, label   string
	attention   bool
	lastSuccess time.Time // zero when never
}

type repBackups struct {
	plans       []repBackupPlan
	warnings    []string
	storedBytes int64
	measured    bool
}

type repSchedule struct {
	disabled    bool
	cron        string
	days, hours float64
}

func (sc repSchedule) enabled() bool {
	return !sc.disabled && (sc.cron != "" || sc.days != 0 || sc.hours != 0)
}

type repOp struct {
	planID      string
	started     float64 // epoch ms
	finished    float64 // 0 = not finished
	status      string
	kind        string
	dryRun      bool
	storedBytes float64
	hasStored   bool
}

var repBackrestStatus = []string{"STATUS_UNKNOWN", "STATUS_PENDING", "STATUS_INPROGRESS", "STATUS_SUCCESS", "STATUS_ERROR", "STATUS_SYSTEM_CANCELLED", "STATUS_USER_CANCELLED", "STATUS_WARNING"}

const repHourMs = 3_600_000.0

// repBrNum is a non-negative whole number from a JSON number or numeric string
// (Backrest writes int64 as strings).
func repBrNum(v any) (float64, bool) {
	n, ok := repNum(v)
	if _, isBool := v.(bool); isBool || !ok || n < 0 || n != math.Trunc(n) || n > 9_007_199_254_740_991 {
		return 0, false
	}
	return n, true
}

func repBrTime(v any) float64 {
	if n, ok := repBrNum(v); ok && n != 0 && n <= 8.64e15 {
		return n
	}
	return 0
}

func repBrSchedule(v any) repSchedule {
	m := repObj(v)
	cron, _ := m["cron"].(string)
	sc := repSchedule{disabled: m["disabled"] == true, cron: cron}
	sc.days, _ = repBrNum(m["maxFrequencyDays"])
	sc.hours, _ = repBrNum(m["maxFrequencyHours"])
	return sc
}

func repParseOps(body map[string]any) ([]repOp, error) {
	var out []repOp
	for _, raw := range repArr(body["operations"]) {
		o := repObj(raw)
		payload := o
		if p, ok := o["op"]; ok {
			payload = repObj(p)
		}
		kind := "other"
		for _, k := range []string{"operationBackup", "operationCheck", "operationStats"} {
			if _, ok := payload[k]; ok {
				kind = k
				break
			}
		}
		detail := repObj(payload[kind])
		started := repBrTime(o["unixTimeStartMs"])
		if kind != "other" && started == 0 {
			return nil, errors.New("Backrest returned an operation without a valid timestamp")
		}
		status := "STATUS_UNKNOWN"
		switch st := o["status"].(type) {
		case float64:
			if i := int(st); float64(i) == st && i >= 0 && i < len(repBackrestStatus) {
				status = repBackrestStatus[i]
			}
		case string:
			if st != "" {
				status = st
			}
		}
		op := repOp{planID: repStr(o["planId"]), started: started, finished: repBrTime(o["unixTimeEndMs"]), status: status, kind: kind, dryRun: detail["dryRun"] == true}
		op.storedBytes, op.hasStored = repBrNum(repObj(detail["stats"])["totalSize"])
		out = append(out, op)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].started > out[j].started })
	return out, nil
}

func repSuccess(ops []repOp) *repOp {
	for i := range ops {
		if ops[i].status == "STATUS_SUCCESS" {
			return &ops[i]
		}
	}
	return nil
}

func repCompletedAt(o *repOp) float64 {
	if o.finished != 0 {
		return o.finished
	}
	return o.started
}

// repBackupHealth is the state of a plan from its backups (newest first): a
// label and whether it needs a look.
func repBackupHealth(ops []repOp, sc repSchedule, now, grace float64) (string, bool) {
	var latest *repOp
	running := false
	for i := range ops {
		switch ops[i].status {
		case "STATUS_INPROGRESS":
			running = true
		case "STATUS_PENDING":
		default:
			if latest == nil {
				latest = &ops[i]
			}
		}
	}
	suffix := ""
	if running {
		suffix = " · running"
	}
	if latest != nil && latest.status != "STATUS_SUCCESS" {
		label := "Failed"
		if latest.status == "STATUS_WARNING" {
			label = "Warnings"
		} else if strings.Contains(latest.status, "CANCELLED") {
			label = "Cancelled"
		}
		return label + suffix, true
	}
	if !sc.enabled() {
		if running {
			return "Running", false
		}
		return "Manual", false
	}
	good := repSuccess(ops)
	if good == nil {
		if running {
			return "First run in progress", true
		}
		return "Never run", true
	}
	period, ok := repNominalPeriod(sc, now)
	if !ok {
		return "Freshness unknown", true
	}
	if now > repCompletedAt(good)+period+grace {
		return "Overdue" + suffix, true
	}
	if running {
		return "Running", false
	}
	return "Current", false
}

// repNominalPeriod is the longest gap between runs of a schedule, in ms.
func repNominalPeriod(sc repSchedule, now float64) (float64, bool) {
	if !sc.enabled() {
		return 0, false
	}
	if sc.hours != 0 {
		return sc.hours * repHourMs, true
	}
	if sc.days != 0 {
		return sc.days * 24 * repHourMs, true
	}
	cr := repParseCron(sc.cron)
	if cr == nil {
		return 0, false
	}
	prev, ok := cr.next(time.UnixMilli(int64(now)))
	if !ok {
		return 0, false
	}
	longest := 0.0
	for range 8 {
		next, ok := cr.next(prev)
		if !ok {
			return 0, false
		}
		longest = math.Max(longest, float64(next.Sub(prev).Milliseconds()))
		prev = next
	}
	return longest, longest != 0
}

func (s *Server) repLoadBackups(ctx context.Context, c *svcCred, now time.Time) (*repBackups, error) {
	user := firstNonEmpty(c.Field("USERNAME"), "admin")
	pass := firstNonEmpty(c.Field("PASSWORD"), c.Key)
	if pass == "" {
		return nil, errors.New("needs the Backrest password (Settings → Integrations)")
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := s.callJSON(ctx, "backrest", http.MethodPost, c.API("/v1.Authentication/Login"), nil, map[string]string{"username": user, "password": pass}, &login); err != nil {
		return nil, err
	}
	if login.Token == "" {
		return nil, errors.New("Backrest login returned no session token")
	}
	rpc := func(method string, body any) (map[string]any, error) {
		if body == nil {
			body = map[string]any{}
		}
		var out map[string]any
		err := s.callJSON(ctx, "backrest", http.MethodPost, c.API("/v1.Backrest/"+method), map[string]string{"Authorization": "Bearer " + login.Token}, body, &out)
		return out, err
	}
	cfg, err := rpc("GetConfig", nil)
	if err != nil {
		return nil, err
	}
	type planCfg struct {
		id, repo string
		sched    repSchedule
	}
	type repoCfg struct{ id, guid string }
	var plans []planCfg
	var repos []repoCfg
	for _, p := range repArr(cfg["plans"]) {
		m := repObj(p)
		plans = append(plans, planCfg{id: firstNonEmpty(repStr(m["id"]), "?"), repo: repStr(m["repo"]), sched: repBrSchedule(m["schedule"])})
	}
	for _, r := range repArr(cfg["repos"]) {
		m := repObj(r)
		repos = append(repos, repoCfg{id: repStr(m["id"]), guid: repStr(m["guid"])})
	}
	instance := firstNonEmpty(repStr(cfg["instance"]), repStr(cfg["instanceId"]))

	type history struct {
		ops []repOp
		err error
	}
	hist := make([]history, len(repos))
	var summaryErr error
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, summaryErr = rpc("GetSummaryDashboard", nil)
	}()
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if r.guid == "" {
				hist[i].err = errors.New("repository GUID is unavailable")
				return
			}
			sel := map[string]any{"repoGuid": r.guid}
			if instance != "" {
				sel["instanceId"] = instance
			}
			body, err := rpc("GetOperations", map[string]any{"selector": sel, "lastN": "0"})
			if err == nil {
				hist[i].ops, err = repParseOps(body)
			}
			hist[i].err = err
		}()
	}
	wg.Wait()
	byRepo := map[string]*history{}
	for i, r := range repos {
		byRepo[r.id] = &hist[i]
	}

	nowMs := float64(now.UnixMilli())
	grace := 2 * repHourMs
	out := &repBackups{}
	if summaryErr != nil {
		out.warnings = append(out.warnings, "Backrest's next-run summary is unavailable. Pending operations are used when available.")
	}
	for _, p := range plans {
		h := byRepo[p.repo]
		bp := repBackupPlan{id: p.id}
		if h == nil || h.err != nil {
			bp.label, bp.attention = "Unavailable", true
		} else {
			var ops []repOp
			for _, o := range h.ops {
				if o.kind == "operationBackup" && o.planID == p.id && !o.dryRun {
					ops = append(ops, o)
				}
			}
			bp.label, bp.attention = repBackupHealth(ops, p.sched, nowMs, grace)
			if good := repSuccess(ops); good != nil {
				bp.lastSuccess = time.UnixMilli(int64(repCompletedAt(good)))
			}
		}
		out.plans = append(out.plans, bp)
	}
	for i := range repos {
		for _, o := range hist[i].ops {
			if o.kind == "operationStats" && o.status == "STATUS_SUCCESS" && o.hasStored {
				out.storedBytes += int64(o.storedBytes)
				out.measured = true
				break
			}
		}
	}
	return out, nil
}

// repCron is the five-field cron grammar Backrest schedules use (minute hour
// day-of-month month day-of-week, with *, ?, lists, ranges, steps and English
// names), read in UTC; day-of-month and day-of-week are OR-ed when both are set.
type repCron struct {
	min, hour, dom, mon, dow map[int]bool
	domSet, dowSet           bool
}

var (
	repMonthNames = map[string]int{"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12}
	repDayNames   = map[string]int{"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6}
)

// repCronField reads one field: its values, whether it narrows anything, ok.
func repCronField(s string, lo, hi int, names map[string]int) (map[int]bool, bool, bool) {
	vals := map[int]bool{}
	restricted := false
	val := func(t string) (int, bool) {
		if n, err := strconv.Atoi(t); err == nil {
			return n, true
		}
		n, ok := names[strings.ToUpper(t)]
		return n, ok
	}
	for _, part := range strings.Split(s, ",") {
		pieces := strings.Split(part, "/")
		if len(pieces) > 2 || pieces[0] == "" {
			return nil, false, false
		}
		step := 1
		if len(pieces) == 2 {
			n, err := strconv.Atoi(pieces[1])
			if err != nil || n <= 0 {
				return nil, false, false
			}
			step = n
		}
		base := pieces[0]
		var a, b int
		switch {
		case base == "*" || base == "?":
			a, b = lo, hi
			if len(pieces) == 2 {
				restricted = true
			}
		case strings.Contains(base, "-"):
			x, y, _ := strings.Cut(base, "-")
			var ok1, ok2 bool
			a, ok1 = val(x)
			b, ok2 = val(y)
			if !ok1 || !ok2 || a > b {
				return nil, false, false
			}
			restricted = true
		default:
			var ok bool
			if a, ok = val(base); !ok {
				return nil, false, false
			}
			b = a
			if len(pieces) == 2 {
				b = hi
			}
			restricted = true
		}
		if a < lo || b > hi {
			return nil, false, false
		}
		for v := a; v <= b; v += step {
			vals[v] = true
		}
	}
	return vals, restricted, len(vals) > 0
}

func repParseCron(expr string) *repCron {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil
	}
	c := &repCron{}
	var ok bool
	if c.min, _, ok = repCronField(f[0], 0, 59, nil); !ok {
		return nil
	}
	if c.hour, _, ok = repCronField(f[1], 0, 23, nil); !ok {
		return nil
	}
	if c.dom, c.domSet, ok = repCronField(f[2], 1, 31, nil); !ok {
		return nil
	}
	if c.mon, _, ok = repCronField(f[3], 1, 12, repMonthNames); !ok {
		return nil
	}
	var dow map[int]bool
	if dow, c.dowSet, ok = repCronField(f[4], 0, 7, repDayNames); !ok {
		return nil
	}
	c.dow = map[int]bool{}
	for d := range dow {
		c.dow[d%7] = true
	}
	return c
}

func (c *repCron) dayMatches(dom, dow int) bool {
	switch {
	case c.domSet && c.dowSet:
		return c.dom[dom] || c.dow[dow]
	case c.domSet:
		return c.dom[dom]
	case c.dowSet:
		return c.dow[dow]
	}
	return true
}

// next is the first matching minute strictly after t, within eight years.
func (c *repCron) next(t time.Time) (time.Time, bool) {
	t = t.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(8, 0, 0)
	for steps := 0; !t.After(limit) && steps < 250_000; steps++ {
		switch {
		case !c.mon[int(t.Month())]:
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		case !c.dayMatches(t.Day(), int(t.Weekday())):
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
		case !c.hour[t.Hour()]:
			t = t.Truncate(time.Hour).Add(time.Hour)
		case !c.min[t.Minute()]:
			t = t.Add(time.Minute)
		default:
			return t, true
		}
	}
	return time.Time{}, false
}

// ---- Glances ----

// repGlances is the person's Glances: its own connection, or the address a
// Beszel connection carries (GLANCES_URL).
func (s *Server) repGlances(ctx context.Context, uid int64) (*svcCred, error) {
	c, err := s.service(ctx, uid, "glances")
	if err == nil {
		return c, nil
	}
	var nc errNotConnected
	if !errors.As(err, &nc) {
		return nil, err
	}
	if b, berr := s.service(ctx, uid, "beszel"); berr == nil {
		if g := strings.TrimRight(b.Field("GLANCES_URL"), "/"); g != "" {
			return &svcCred{Type: "glances", URL: g, PublicURL: g, Fields: map[string]string{}, Source: b.Source}, nil
		}
	}
	return nil, err
}

type repServer struct {
	cpu, load1      float64
	cores           int
	hasCPU          bool
	memTotal, free  float64
	memPct          float64
	hasMem          bool
	rootPct, rootFr float64
	hasRoot         bool
	uptime          string
}

// repLoadServer reads CPU, memory, the root file system and uptime; a part
// that fails is left out, all of them failing is an error.
func (s *Server) repLoadServer(ctx context.Context, c *svcCred) (*repServer, error) {
	out := &repServer{}
	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	get := func(path string, v any) error {
		return s.callJSON(ctx, "glances", http.MethodGet, c.API("/api/4/"+path), nil, nil, v)
	}
	run := func(f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	run(func() error {
		var q struct {
			CPU float64 `json:"cpu"`
		}
		var l struct {
			Min1    float64 `json:"min1"`
			CPUCore int     `json:"cpucore"`
		}
		if err := get("quicklook", &q); err != nil {
			return err
		}
		if err := get("load", &l); err != nil {
			return err
		}
		mu.Lock()
		out.cpu, out.load1, out.cores, out.hasCPU = q.CPU, l.Min1, l.CPUCore, true
		mu.Unlock()
		return nil
	})
	run(func() error {
		var m struct {
			Total     float64 `json:"total"`
			Available float64 `json:"available"`
			Percent   float64 `json:"percent"`
		}
		if err := get("mem", &m); err != nil {
			return err
		}
		mu.Lock()
		out.memTotal, out.free, out.memPct, out.hasMem = m.Total, m.Available, m.Percent, true
		mu.Unlock()
		return nil
	})
	run(func() error {
		var list []struct {
			Mount   string  `json:"mnt_point"`
			Free    float64 `json:"free"`
			Percent float64 `json:"percent"`
		}
		if err := get("fs", &list); err != nil {
			return err
		}
		mu.Lock()
		for _, f := range list {
			if f.Mount == "/" {
				out.rootPct, out.rootFr, out.hasRoot = f.Percent, f.Free, true
			}
		}
		mu.Unlock()
		return nil
	})
	run(func() error {
		var u string
		if err := get("uptime", &u); err != nil {
			return err
		}
		mu.Lock()
		out.uptime = repCompactUptime(u)
		mu.Unlock()
		return nil
	})
	wg.Wait()
	if len(errs) == 4 {
		return nil, errs[0]
	}
	return out, nil
}

type repProc struct {
	pid    int64
	name   string
	cpu    float64
	memRss float64
}

func (s *Server) repLoadProcesses(ctx context.Context, c *svcCred) ([]repProc, error) {
	var raw []map[string]any
	if err := s.callJSON(ctx, "glances", http.MethodGet, c.API("/api/4/processlist"), nil, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]repProc, 0, len(raw))
	for _, p := range raw {
		pr := repProc{name: repStr(p["name"])}
		pid, _ := repNum(p["pid"])
		pr.pid = int64(pid)
		if v, ok := p["cpu_percent"].(float64); ok {
			pr.cpu = v
		}
		switch mi := p["memory_info"].(type) {
		case map[string]any:
			pr.memRss, _ = repNum(mi["rss"])
		case []any:
			if len(mi) > 0 {
				pr.memRss, _ = repNum(mi[0])
			}
		}
		out = append(out, pr)
	}
	return out, nil
}

// ---- Beszel ----

type repSystem struct {
	name, status         string
	cpu, mem, disk, temp float64
	uptime               float64
}

func (s *Server) repLoadSystems(ctx context.Context, c *svcCred) ([]repSystem, error) {
	email, pass := c.Field("EMAIL"), firstNonEmpty(c.Field("PASSWORD"), c.Key)
	if email == "" || pass == "" {
		return nil, errors.New("needs the email and password (Settings → Integrations)")
	}
	var auth struct {
		Token string `json:"token"`
	}
	if err := s.callJSON(ctx, "beszel", http.MethodPost, c.API("/api/collections/users/auth-with-password"), nil, map[string]string{"identity": email, "password": pass}, &auth); err != nil {
		return nil, err
	}
	var page struct {
		Items []struct {
			ID     string         `json:"id"`
			Name   string         `json:"name"`
			Host   string         `json:"host"`
			Status string         `json:"status"`
			Info   map[string]any `json:"info"`
		} `json:"items"`
	}
	if err := s.callJSON(ctx, "beszel", http.MethodGet, c.API("/api/collections/systems/records?perPage=500&sort=name"), map[string]string{"Authorization": auth.Token}, nil, &page); err != nil {
		return nil, err
	}
	out := []repSystem{}
	for _, it := range page.Items {
		sy := repSystem{name: firstNonEmpty(it.Name, it.Host, it.ID), status: firstNonEmpty(it.Status, "pending")}
		sy.cpu, _ = repNum(it.Info["cpu"])
		sy.mem, _ = repNum(it.Info["mp"])
		sy.disk, _ = repNum(it.Info["dp"])
		sy.temp, _ = repNum(it.Info["dt"])
		sy.uptime, _ = repNum(it.Info["u"])
		out = append(out, sy)
	}
	return out, nil
}

// ---- snapshot ----

// reportSnapshot is the homelab as the app's Home shows it, as a table: the
// server, each computer Beszel watches, monitors, disks and backups. Every row
// states a number, not a colour.
func (s *Server) reportSnapshot(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	var (
		srv     *repServer
		systems []repSystem
		kuma    *repKuma
		disks   []repDisk
		backups *repBackups
	)
	fails, err := gather(ctx, []reportSource{
		{name: "glances", run: func(ctx context.Context) error {
			c, err := s.repGlances(ctx, uid)
			if err == nil {
				srv, err = s.repLoadServer(ctx, c)
			}
			return err
		}},
		{name: "beszel", run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "beszel")
			if err == nil {
				systems, err = s.repLoadSystems(ctx, c)
			}
			return err
		}},
		{name: "kuma", run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "kuma")
			if err == nil {
				kuma, err = s.repLoadKuma(ctx, c, false)
			}
			return err
		}},
		{name: "scrutiny", run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "scrutiny")
			if err == nil {
				disks, err = s.repLoadDisks(ctx, c)
			}
			return err
		}},
		{name: "backrest", timeout: 15 * time.Second, run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "backrest")
			if err == nil {
				backups, err = s.repLoadBackups(ctx, c, now)
			}
			return err
		}},
	})
	if err != nil {
		return "", nil, err
	}
	type row struct{ label, value string }
	var rows []row
	var storage []string
	if srv != nil {
		var parts []string
		if srv.hasCPU {
			t := "CPU " + repRound(srv.cpu) + "%"
			if srv.cores > 0 {
				t += fmt.Sprintf(" (load %s on %d cores)", repFixed(srv.load1, 2), srv.cores)
			}
			parts = append(parts, t)
		}
		if srv.hasMem && srv.memTotal > 0 {
			parts = append(parts, fmt.Sprintf("RAM %s of %s (%s%%)", repSize(srv.memTotal-srv.free), repSize(srv.memTotal), repRound(srv.memPct)))
		}
		if srv.uptime != "" {
			parts = append(parts, "up "+srv.uptime)
		}
		if len(parts) > 0 {
			rows = append(rows, row{"Server", strings.Join(parts, " · ")})
		}
		if srv.hasRoot {
			storage = append(storage, fmt.Sprintf("/ %s%% used (%s free)", repRound(srv.rootPct), repSize(srv.rootFr)))
		}
	}
	hottest, hot := 0.0, false
	for _, d := range disks {
		if d.temp != nil && *d.temp > 0 && (!hot || *d.temp > hottest) {
			hottest, hot = *d.temp, true
		}
	}
	if hot {
		storage = append(storage, "hottest disk "+repRound(hottest)+" °C")
	}
	if len(storage) > 0 {
		rows = append(rows, row{"Storage", strings.Join(storage, " · ")})
	}
	for _, sy := range systems {
		var v string
		switch sy.status {
		case "up":
			parts := []string{"CPU " + repRound(sy.cpu) + "%", "RAM " + repRound(sy.mem) + "%", "disk " + repRound(sy.disk) + "%"}
			if sy.temp > 0 {
				parts = append(parts, repRound(sy.temp)+" °C")
			}
			if sy.uptime > 0 {
				sec := int(sy.uptime)
				parts = append(parts, "up "+repUptimeParts(sec/86400, sec%86400/3600, sec%3600/60))
			}
			v = strings.Join(parts, " · ")
		case "down":
			v = "**offline**"
		case "paused":
			v = "paused"
		default:
			v = "waiting for its agent"
		}
		rows = append(rows, row{sy.name, v})
	}
	if kuma != nil {
		down := kuma.down()
		t := fmt.Sprintf("%d up", len(kuma.monitors)-len(down))
		if len(down) == 0 {
			t += ", none down"
		} else {
			t += fmt.Sprintf(", **%d down**: %s", len(down), strings.Join(down, ", "))
		}
		rows = append(rows, row{"Monitors", t})
	}
	if disks != nil {
		var warn []string
		for _, d := range disks {
			if d.status != 0 {
				n := d.name
				if d.hostID != "" {
					n = d.hostID + ":" + n
				}
				warn = append(warn, n)
			}
		}
		sort.Strings(warn)
		if len(warn) == 0 {
			rows = append(rows, row{"Disks", fmt.Sprintf("all %d passed", len(disks))})
		} else {
			rows = append(rows, row{"Disks", fmt.Sprintf("**%d of %d with warnings**: %s", len(warn), len(disks), strings.Join(warn, ", "))})
		}
	}
	if backups != nil {
		t := fmt.Sprintf("%d plans", len(backups.plans))
		if len(backups.plans) == 1 {
			t = "1 plan"
		}
		if backups.measured {
			t += ", " + repSize(float64(backups.storedBytes)) + " stored"
		}
		var warnings []string
		for _, p := range backups.plans {
			if p.attention {
				warnings = append(warnings, p.id+": "+strings.ToLower(p.label))
			}
		}
		warnings = append(warnings, backups.warnings...)
		if len(warnings) == 0 {
			t += ", no warnings"
		} else {
			t += " · **" + strings.Join(warnings, "; ") + "**"
		}
		rows = append(rows, row{"Backups", t})
	}
	if len(rows) == 0 {
		return "", fails, nil
	}
	lines := []string{"**Homelab snapshot** · " + repStamp(now), "", "| What | Now |", "|---|---|"}
	for _, r := range rows {
		lines = append(lines, "| "+repEscape(r.label)+" | "+repEscape(r.value)+" |")
	}
	return strings.Join(lines, "\n") + "\n", fails, nil
}

// ---- attention ----

// reportAttention is what needs a look: monitors down and the outages of the
// last days, disks with warnings, backups that are not current. Empty
// sections say so: "nothing down" is worth writing down too.
func (s *Server) reportAttention(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	since := now.Add(-time.Duration(in.Days) * 24 * time.Hour)
	var (
		kuma    *repKuma
		disks   []repDisk
		backups *repBackups
	)
	fails, err := gather(ctx, []reportSource{
		{name: "kuma", run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "kuma")
			if err == nil {
				kuma, err = s.repLoadKuma(ctx, c, true)
			}
			return err
		}},
		{name: "scrutiny", run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "scrutiny")
			if err == nil {
				disks, err = s.repLoadDisks(ctx, c)
			}
			return err
		}},
		{name: "backrest", timeout: 15 * time.Second, run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "backrest")
			if err == nil {
				backups, err = s.repLoadBackups(ctx, c, now)
			}
			return err
		}},
	})
	if err != nil {
		return "", nil, err
	}
	if kuma == nil && len(disks) == 0 && backups == nil {
		return "", fails, nil
	}
	var body []string
	if kuma != nil {
		down := kuma.down()
		if len(down) == 0 {
			body = append(body, fmt.Sprintf("**Monitors:** all %d up", len(kuma.monitors)))
		} else {
			body = append(body, fmt.Sprintf("**Monitors:** %d down: %s", len(down), strings.Join(down, ", ")))
		}
		outages := repOutages(kuma.beats, since)
		switch {
		case len(outages) > 0:
			var rows [][]string
			for _, o := range outages {
				d := "still down"
				if !o.until.IsZero() {
					d = repDuration(o.until.Sub(o.from))
				}
				rows = append(rows, []string{o.monitor, repStamp(o.from), d})
			}
			body = append(body, "")
			body = append(body, repTable([]string{"Monitor", "Down from", "For"}, rows)...)
		case kuma.slug:
			body = append(body, "No outages since "+repStamp(since)+".")
		default:
			body = append(body, "Outage history needs a status page slug in Settings; with an API key alone only the moment is known.")
		}
		body = append(body, "")
	}
	if len(disks) > 0 {
		var rows [][]string
		for _, d := range disks {
			if d.status == 0 {
				continue
			}
			name := d.name
			if name == "?" {
				name = d.serial
			}
			problem := "Over Scrutiny's thresholds"
			if d.status&1 != 0 {
				problem = "SMART failed"
			}
			temp := "—"
			if d.temp != nil {
				temp = repRound(*d.temp) + " °C"
			}
			rows = append(rows, []string{name, d.host, problem, temp})
		}
		if len(rows) == 0 {
			body = append(body, fmt.Sprintf("**Disks:** all %d passed", len(disks)))
		} else {
			body = append(body, fmt.Sprintf("**Disks:** %d with warnings", len(rows)), "")
			body = append(body, repTable([]string{"Disk", "Host", "Problem", "Temp"}, rows)...)
		}
		body = append(body, "")
	}
	if backups != nil {
		var rows [][]string
		for _, p := range backups.plans {
			if !p.attention {
				continue
			}
			last := "never"
			if !p.lastSuccess.IsZero() {
				last = repStamp(p.lastSuccess)
			}
			rows = append(rows, []string{p.id, p.label, last})
		}
		if len(rows) == 0 {
			body = append(body, fmt.Sprintf("**Backups:** all %d plans healthy", len(backups.plans)))
		} else {
			body = append(body, fmt.Sprintf("**Backups:** %d of %d plans need a look", len(rows), len(backups.plans)), "")
			body = append(body, repTable([]string{"Plan", "State", "Last good backup"}, rows)...)
		}
		for i, w := range backups.warnings {
			if i == 5 {
				break
			}
			body = append(body, "- "+w)
		}
		body = append(body, "")
	}
	return repBlock("What needs attention", now, body), fails, nil
}

// ---- media ----

type repMedia struct {
	kind    string // Watched, Listened, Listening to, Finished
	title   string
	detail  string
	date    time.Time // zero = no date
	minutes int       // 0 = not known
}

var repMediaKinds = []string{"Watched", "Listened", "Listening to", "Finished"}

// reportMedia is what was watched and listened to: Jellystat's plays,
// Pocket Podcasts' history, the books in progress in Audiobookshelf.
func (s *Server) reportMedia(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	since := now.Add(-time.Duration(in.Days) * 24 * time.Hour)
	var watched, listened, reading []repMedia
	fails, err := gather(ctx, []reportSource{
		{name: "jellystat", run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "jellystat")
			if err != nil {
				return err
			}
			var page struct {
				Results []map[string]any `json:"results"`
			}
			if err := s.callJSON(ctx, "jellystat", http.MethodGet, c.API("/stats/getPlaybackActivity?size=50&page=1"), map[string]string{"x-api-token": c.Key}, nil, &page); err != nil {
				return err
			}
			for _, r := range page.Results {
				secs, _ := repNum(r["PlaybackDuration"])
				if secs < 300 { // a few minutes of something is not watching it
					continue
				}
				m := repMedia{kind: "Watched", title: firstNonEmpty(repStr(r["NowPlayingItemName"]), "?"), detail: repStr(r["SeriesName"]), minutes: int(secs / 60)}
				m.date, _ = repParseTime(repStr(r["ActivityDateInserted"]))
				watched = append(watched, m)
			}
			return nil
		}},
		{name: "podcasts", run: func(ctx context.Context) error {
			base, tok, err := s.appToken(ctx, uid, "podcasts", "history:read")
			if err != nil {
				return err
			}
			var page struct {
				Episodes []struct {
					Title        string `json:"title"`
					PodcastTitle string `json:"podcast_title"`
					Duration     int    `json:"duration"`
					PlayPosition int    `json:"play_position"`
					Completed    bool   `json:"completed"`
					LastPlayedAt string `json:"last_played_at"`
				} `json:"episodes"`
			}
			if err := s.callJSON(ctx, "podcasts", http.MethodGet, base+"/api/v1/history?limit=200", map[string]string{"Authorization": "Bearer " + tok}, nil, &page); err != nil {
				return err
			}
			for _, e := range page.Episodes {
				t, ok := repParseTime(e.LastPlayedAt)
				if !ok || t.Before(since) {
					continue
				}
				state := "finished"
				if !e.Completed {
					pct := 0
					if e.Duration > 0 {
						pct = int(float64(e.PlayPosition) / float64(e.Duration) * 100)
					}
					state = fmt.Sprintf("%d%%", pct)
				}
				detail := state
				if e.PodcastTitle != "" {
					detail = e.PodcastTitle + " · " + state
				}
				listened = append(listened, repMedia{kind: "Listened", title: e.Title, detail: detail, date: t})
			}
			return nil
		}},
		{name: "audiobookshelf", run: func(ctx context.Context) error {
			c, err := s.service(ctx, uid, "audiobookshelf")
			if err != nil {
				return err
			}
			var body struct {
				LibraryItems []map[string]any `json:"libraryItems"`
			}
			if err := s.callJSON(ctx, "audiobookshelf", http.MethodGet, c.API("/api/me/items-in-progress?limit=10"), map[string]string{"Authorization": "Bearer " + c.Key}, nil, &body); err != nil {
				return err
			}
			seen := map[string]bool{}
			for _, it := range body.LibraryItems {
				key := repStr(it["id"]) + "|" + repStr(repObj(it["recentEpisode"])["id"])
				if seen[key] {
					continue
				}
				seen[key] = true
				meta := repObj(repObj(it["media"])["metadata"])
				author := firstNonEmpty(repStr(meta["authorName"]), repStr(meta["author"]))
				if author == "" {
					var names []string
					for _, a := range repArr(meta["authors"]) {
						if n := repStr(repObj(a)["name"]); n != "" {
							names = append(names, n)
						}
					}
					author = strings.Join(names, ", ")
				}
				reading = append(reading, repMedia{kind: "Listening to", title: firstNonEmpty(repStr(meta["title"]), "Untitled"), detail: author})
			}
			return nil
		}},
	})
	if err != nil {
		return "", nil, err
	}
	// The same title from two sources is one line.
	var items []repMedia
	seen := map[string]bool{}
	for _, m := range append(append(watched, listened...), reading...) {
		k := m.kind + "|" + strings.ToLower(m.title)
		if seen[k] || (!m.date.IsZero() && m.date.Before(since)) {
			continue
		}
		seen[k] = true
		items = append(items, m)
	}
	if len(items) == 0 {
		return "", fails, nil
	}
	body := []string{"Since " + repStampDay(since) + ".", ""}
	for _, kind := range repMediaKinds {
		var rows []repMedia
		for _, m := range items {
			if m.kind == kind {
				rows = append(rows, m)
			}
		}
		if len(rows) == 0 {
			continue
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].date.After(rows[j].date) })
		var cells [][]string
		for _, r := range rows {
			var parts []string
			if r.detail != "" {
				parts = append(parts, r.detail)
			}
			if r.minutes > 0 {
				parts = append(parts, fmt.Sprintf("%d min", r.minutes))
			}
			when := ""
			if !r.date.IsZero() {
				when = repStampDay(r.date)
			}
			cells = append(cells, []string{r.title, strings.Join(parts, " · "), when})
		}
		body = append(body, "**"+kind+"**", "")
		body = append(body, repTable([]string{"What", "", "When"}, cells)...)
		body = append(body, "")
	}
	return repBlock("Watched and listened", now, body), fails, nil
}

// ---- spent ----

type repSpend struct {
	date, desc, category, currency string
	amount                         int64
}

// reportSpent is this month's spending in Pocket Money by category, and the
// largest ones. from/to choose another stretch.
func (s *Server) reportSpent(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	to := now.Format("2006-01-02")
	title := "This month so far"
	if in.From != "" || in.To != "" {
		from, to = firstNonEmpty(in.From, from), firstNonEmpty(in.To, to)
		if from > to {
			from, to = to, from
		}
		f, _ := time.ParseInLocation("2006-01-02", from, time.Local)
		t, _ := time.ParseInLocation("2006-01-02", to, time.Local)
		title = repStampDay(f)
		if from != to {
			title += " – " + repStampDay(t)
		}
	}
	var entries []repSpend
	fails, err := gather(ctx, []reportSource{{name: "money", run: func(ctx context.Context) error {
		base, tok, err := s.appToken(ctx, uid, "money", "transactions:read")
		if err != nil {
			return err
		}
		for offset, page := 0, 0; page < 20; page++ {
			var body struct {
				Items []struct {
					Date         string `json:"date"`
					Amount       int64  `json:"amount"`
					Description  string `json:"description"`
					Payee        string `json:"payee"`
					CategoryName string `json:"category_name"`
					Currency     string `json:"currency"`
					Splits       []struct {
						Amount       int64  `json:"amount"`
						Note         string `json:"note"`
						CategoryName string `json:"category_name"`
					} `json:"splits"`
				} `json:"items"`
				HasMore bool `json:"has_more"`
			}
			q := url.Values{"kind": {"expense"}, "from": {from}, "to": {to}, "limit": {"1000"}, "offset": {strconv.Itoa(offset)}}
			if err := s.callJSON(ctx, "money", http.MethodGet, base+"/api/v1/transactions?"+q.Encode(), map[string]string{"Authorization": "Bearer " + tok}, nil, &body); err != nil {
				return err
			}
			for _, t := range body.Items {
				desc := firstNonEmpty(t.Description, t.Payee, t.CategoryName, "—")
				if len(t.Splits) == 0 {
					entries = append(entries, repSpend{date: t.Date, desc: desc, category: t.CategoryName, currency: t.Currency, amount: t.Amount})
					continue
				}
				for _, sp := range t.Splits {
					entries = append(entries, repSpend{date: t.Date, desc: firstNonEmpty(sp.Note, desc), category: sp.CategoryName, currency: t.Currency, amount: sp.Amount})
				}
			}
			offset += len(body.Items)
			if !body.HasMore || len(body.Items) == 0 {
				break
			}
		}
		return nil
	}}})
	if err != nil {
		return "", nil, err
	}
	if len(entries) == 0 {
		return "", fails, nil
	}
	currency := entries[0].currency
	type cat struct {
		name   string
		amount int64
		count  int
	}
	var same []repSpend
	byCat := map[string]*cat{}
	var cats []*cat
	var total int64
	for _, e := range entries {
		if e.currency != currency {
			continue
		}
		same = append(same, e)
		total += e.amount
		name := firstNonEmpty(e.category, "Uncategorised")
		c := byCat[name]
		if c == nil {
			c = &cat{name: name}
			byCat[name] = c
			cats = append(cats, c)
		}
		c.amount += e.amount
		c.count++
	}
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].amount > cats[j].amount })
	plural := "s"
	if len(same) == 1 {
		plural = ""
	}
	body := []string{fmt.Sprintf("%s: **%s** over %d transaction%s.", title, repMoney(total, currency), len(same), plural), ""}
	var rows [][]string
	for _, c := range cats {
		rows = append(rows, []string{c.name, repMoney(c.amount, currency), strconv.Itoa(c.count)})
	}
	body = append(body, repTable([]string{"Category", "Spent", "Count"}, rows)...)
	sort.SliceStable(same, func(i, j int) bool { return same[i].amount > same[j].amount })
	rows = nil
	for _, e := range same[:min(5, len(same))] {
		rows = append(rows, []string{repDay(e.date), e.desc, repMoney(e.amount, currency)})
	}
	body = append(body, "", "**Largest**", "")
	body = append(body, repTable([]string{"Date", "What", "Amount"}, rows)...)
	if n := len(entries) - len(same); n > 0 {
		body = append(body, "", fmt.Sprintf("%d in other currencies left out.", n))
	}
	return repBlock("Spending", now, body), fails, nil
}

func repDay(raw string) string {
	if len(raw) > 10 {
		return raw[:10]
	}
	return raw
}

// ---- fuel ----

type repFill struct {
	date             string
	odometer, volume float64
	cost             int64
	currency         string
	economy          float64
}

type repWork struct {
	date, title, kind, currency string
	cost                        int64
}

type repVehicle struct {
	name, currency string
	fills          []repFill
	work           []repWork
}

// reportFuel is the vehicle log from Pocket Garage: what this month and the
// last twelve cost, the last fill-ups and the last services and repairs.
func (s *Server) reportFuel(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	var vehicles []repVehicle
	fails, err := gather(ctx, []reportSource{{name: "garage", run: func(ctx context.Context) error {
		base, tok, err := s.appToken(ctx, uid, "garage", "vehicles:read")
		if err != nil {
			return err
		}
		hdr := map[string]string{"Authorization": "Bearer " + tok}
		var list []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Year     int    `json:"year"`
			Make     string `json:"make"`
			Model    string `json:"model"`
			Currency string `json:"currency"`
		}
		if err := s.callJSON(ctx, "garage", http.MethodGet, base+"/api/v1/vehicles?bare=1", hdr, nil, &list); err != nil {
			return err
		}
		vehicles = make([]repVehicle, len(list))
		errs := make([]error, len(list))
		var wg sync.WaitGroup
		for i, v := range list {
			name := v.Name
			if v.Make != "" || v.Model != "" {
				var parts []string
				if v.Year > 0 {
					parts = append(parts, strconv.Itoa(v.Year))
				}
				name = strings.Join(append(parts, strings.Fields(v.Make+" "+v.Model)...), " ")
			}
			vehicles[i] = repVehicle{name: firstNonEmpty(name, "Vehicle"), currency: v.Currency}
			wg.Add(1)
			go func() {
				defer wg.Done()
				var fuel []struct {
					Date     string  `json:"date"`
					Odometer float64 `json:"odometer"`
					Volume   float64 `json:"volume"`
					Cost     int64   `json:"cost"`
					Currency string  `json:"currency"`
					Economy  float64 `json:"economy"`
				}
				var recs []struct {
					Date        string `json:"date"`
					Kind        string `json:"kind"`
					Description string `json:"description"`
					Cost        int64  `json:"cost"`
					Currency    string `json:"currency"`
				}
				p := fmt.Sprintf("%s/api/v1/vehicles/%d", base, v.ID)
				if errs[i] = s.callJSON(ctx, "garage", http.MethodGet, p+"/fuel", hdr, nil, &fuel); errs[i] != nil {
					return
				}
				if errs[i] = s.callJSON(ctx, "garage", http.MethodGet, p+"/records?kind=service,repair", hdr, nil, &recs); errs[i] != nil {
					return
				}
				for _, f := range fuel {
					vehicles[i].fills = append(vehicles[i].fills, repFill{date: f.Date, odometer: f.Odometer, volume: f.Volume, cost: f.Cost, currency: f.Currency, economy: f.Economy})
				}
				for _, r := range recs {
					kind := map[string]string{"service": "Service", "repair": "Repair"}[r.Kind]
					if kind == "" {
						continue
					}
					vehicles[i].work = append(vehicles[i].work, repWork{date: r.Date, title: firstNonEmpty(r.Description, kind), kind: kind, cost: r.Cost, currency: r.Currency})
				}
			}()
		}
		wg.Wait()
		return repFirstErr(errs...)
	}}})
	if err != nil {
		return "", nil, err
	}
	today := now.Format("2006-01-02")
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	yearStart := now.AddDate(0, -12, 0).Format("2006-01-02")
	var body []string
	for _, v := range vehicles {
		if len(v.fills) == 0 && len(v.work) == 0 {
			continue
		}
		cur := v.currency
		other := func(c string) bool { return c != "" && cur != "" && !strings.EqualFold(c, cur) }
		// line is what a stretch cost: fuel and work apart, in the vehicle's
		// currency; a record in another currency is counted, not added.
		line := func(title, start string) string {
			var fuel, work int64
			var litres float64
			fills, others := 0, 0
			for _, f := range v.fills {
				if d := repDay(f.date); d < start || d > today {
					continue
				}
				if other(f.currency) {
					others++
					continue
				}
				fuel += f.cost
				litres += f.volume
				fills++
			}
			for _, w := range v.work {
				if d := repDay(w.date); d < start || d > today {
					continue
				}
				if other(w.currency) {
					others++
					continue
				}
				work += w.cost
			}
			first := "fuel " + repMoney(fuel, cur)
			if litres > 0 {
				plural := "s"
				if fills == 1 {
					plural = ""
				}
				first += fmt.Sprintf(" (%s l, %d fill-up%s)", repNumber(litres, 1), fills, plural)
			}
			text := fmt.Sprintf("- **%s:** %s · service and repairs %s — **%s**", title, first, repMoney(work, cur), repMoney(fuel+work, cur))
			if others > 0 {
				text += fmt.Sprintf(" (%d in other currencies left out)", others)
			}
			return text
		}
		body = append(body, "**"+v.name+"**", "", line("This month", monthStart), line("Last 12 months", yearStart), "")
		if len(v.fills) > 0 {
			fills := append([]repFill(nil), v.fills...)
			sort.SliceStable(fills, func(i, j int) bool { return fills[i].date > fills[j].date })
			var rows [][]string
			for _, f := range fills[:min(8, len(fills))] {
				econ := "—"
				if f.economy > 0 {
					econ = repNumber(f.economy, 1)
				}
				rows = append(rows, []string{repDay(f.date), repNumber(f.odometer, 0), repNumber(f.volume, 2), repMoney(f.cost, firstNonEmpty(f.currency, cur)), econ})
			}
			body = append(body, repTable([]string{"Date", "Odometer", "Fuel", "Cost", "Economy"}, rows)...)
			body = append(body, "")
		}
		if len(v.work) > 0 {
			work := append([]repWork(nil), v.work...)
			sort.SliceStable(work, func(i, j int) bool { return work[i].date > work[j].date })
			var rows [][]string
			for _, w := range work[:min(8, len(work))] {
				rows = append(rows, []string{repDay(w.date), w.title, w.kind, repMoney(w.cost, firstNonEmpty(w.currency, cur))})
			}
			body = append(body, repTable([]string{"Date", "Work", "Kind", "Cost"}, rows)...)
			body = append(body, "")
		}
	}
	if len(body) == 0 {
		return "", fails, nil
	}
	return repBlock("Vehicle log", now, body), fails, nil
}

// ---- hosts ----

var (
	repDockerNet = regexp.MustCompile(`^10\.2\d\d\.`)
	repBridgeNet = regexp.MustCompile(`^172\.(1[6-9]|2\d|3[01])\.`)
)

// reportHosts is AdGuard's busiest clients, with the names it knows for them.
func (s *Server) reportHosts(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	type client struct {
		ip    string
		count int
	}
	var (
		clients          []client
		names            = map[string]string{}
		queries, blocked int
	)
	fails, err := gather(ctx, []reportSource{{name: "adguard", run: func(ctx context.Context) error {
		c, err := s.service(ctx, uid, "adguard")
		if err != nil {
			return err
		}
		hdr := map[string]string{"Authorization": basicAuth(c.Field("USERNAME"), firstNonEmpty(c.Field("PASSWORD"), c.Key))}
		var st struct {
			Queries    int              `json:"num_dns_queries"`
			Blocked    int              `json:"num_blocked_filtering"`
			TopClients []map[string]int `json:"top_clients"`
		}
		if err := s.callJSON(ctx, "adguard", http.MethodGet, c.API("/control/stats"), hdr, nil, &st); err != nil {
			return err
		}
		queries, blocked = st.Queries, st.Blocked
		q := url.Values{}
		for _, o := range st.TopClients {
			cl := client{ip: "?"}
			for k, v := range o {
				cl = client{ip: k, count: v}
			}
			q.Set(fmt.Sprintf("ip%d", len(clients)), cl.ip)
			clients = append(clients, cl)
		}
		if len(clients) == 0 {
			return nil
		}
		// A failed name lookup leaves the addresses alone.
		var found []map[string]any
		if s.callJSON(ctx, "adguard", http.MethodGet, c.API("/control/clients/find?"+q.Encode()), hdr, nil, &found) == nil {
			for _, entry := range found {
				for ip, info := range entry {
					name := repStr(repObj(info)["name"])
					if l := repArr(info); len(l) > 0 {
						name = repStr(repObj(l[0])["name"])
					}
					if name != "" {
						names[ip] = name
					}
				}
			}
		}
		return nil
	}}})
	if err != nil {
		return "", nil, err
	}
	if len(clients) == 0 {
		return "", fails, nil
	}
	pct := 0.0
	if queries > 0 {
		pct = float64(blocked) / float64(queries) * 100
	}
	var rows [][]string
	for _, c := range clients {
		name := names[c.ip]
		if name == "" && (repDockerNet.MatchString(c.ip) || repBridgeNet.MatchString(c.ip)) {
			name = "docker container"
		}
		rows = append(rows, []string{firstNonEmpty(name, "—"), c.ip, repCount(c.count)})
	}
	body := append([]string{fmt.Sprintf("AdGuard's busiest clients, %s queries in its window, %s%% blocked.", repCount(queries), repFixed(pct, 1)), ""},
		repTable([]string{"Name", "Address", "Queries"}, rows)...)
	return repBlock("Hosts on the network", now, body), fails, nil
}

// ---- processes ----

// reportProcesses is what the server is busy with: Glances' top processes by
// CPU and by memory.
func (s *Server) reportProcesses(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	var procs []repProc
	fails, err := gather(ctx, []reportSource{{name: "glances", run: func(ctx context.Context) error {
		c, err := s.repGlances(ctx, uid)
		if err == nil {
			procs, err = s.repLoadProcesses(ctx, c)
		}
		return err
	}}})
	if err != nil {
		return "", nil, err
	}
	if len(procs) == 0 {
		return "", fails, nil
	}
	top := func(less func(a, b repProc) bool) []repProc {
		list := append([]repProc(nil), procs...)
		sort.SliceStable(list, func(i, j int) bool { return less(list[i], list[j]) })
		return list[:min(8, len(list))]
	}
	var cpuRows, memRows [][]string
	for _, p := range top(func(a, b repProc) bool { return a.cpu > b.cpu }) {
		cpuRows = append(cpuRows, []string{p.name, strconv.FormatInt(p.pid, 10), repFixed(p.cpu, 1) + "%", repSize(p.memRss)})
	}
	for _, p := range top(func(a, b repProc) bool { return a.memRss > b.memRss }) {
		memRows = append(memRows, []string{p.name, strconv.FormatInt(p.pid, 10), repSize(p.memRss), repFixed(p.cpu, 1) + "%"})
	}
	body := []string{"**By CPU**", ""}
	body = append(body, repTable([]string{"Process", "PID", "CPU", "Memory"}, cpuRows)...)
	body = append(body, "", "**By memory**", "")
	body = append(body, repTable([]string{"Process", "PID", "Memory", "CPU"}, memRows)...)
	return repBlock("Server processes", now, body), fails, nil
}

// ---- habits ----

// repHabitDay is Pocket Habits' page for one day (GET /api/v1/days/{date}):
// the habits active that day and the trackers with their values.
type repHabitDay struct {
	Habits []struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Status    int    `json:"status"` // 1 done, -1 missed, 0 not marked
		Scheduled bool   `json:"scheduled"`
	} `json:"habits"`
	Trackers []struct {
		ID    int64    `json:"id"`
		Name  string   `json:"name"`
		Kind  string   `json:"kind"` // scale | number | yesno
		Step  float64  `json:"step"`
		Unit  string   `json:"unit"`
		Value *float64 `json:"value"`
	} `json:"trackers"`
	Journal    string `json:"journal"`
	JournalBad string `json:"journal_bad"`
	Important  string `json:"important"`
}

// repHabitDecimals is how many decimals a number tracker shows, from its step.
func repHabitDecimals(kind string, step float64) int {
	if kind != "number" {
		return 0
	}
	if step <= 0 {
		step = 1
	}
	n := 0
	for n < 3 && math.Abs(step-math.Round(step)) > 1e-9 {
		step *= 10
		n++
	}
	return n
}

func repHabitNumber(v float64, decimals int) string {
	whole := math.Abs(v-math.Round(v)) < 1e-9
	if decimals == 0 && whole {
		return strconv.Itoa(int(math.Round(v)))
	}
	if !whole {
		decimals = max(decimals, 1)
	}
	return repFixed(v, decimals)
}

// reportHabits is days of habits from Pocket Habits, as the app's "Days of
// habits" writes them: the period in numbers, then each day, newest first.
func (s *Server) reportHabits(ctx context.Context, uid int64, in reportReq, now time.Time) (string, []string, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	to := today
	if in.To != "" {
		to, _ = time.ParseInLocation("2006-01-02", in.To, time.Local)
	}
	from := to.AddDate(0, 0, -(in.Days - 1))
	if in.From != "" {
		from, _ = time.ParseInLocation("2006-01-02", in.From, time.Local)
		if in.To == "" && from.After(to) {
			to = from
		}
	}
	if from.After(to) {
		from, to = to, from
	}
	if to.After(today) {
		to = today
	}
	if from.After(to) {
		from = to
	}
	if to.Sub(from) > 92*24*time.Hour {
		from = to.AddDate(0, 0, -92)
	}
	var days []time.Time
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	pages := make([]repHabitDay, len(days))
	fails, err := gather(ctx, []reportSource{{name: "habits", timeout: 15 * time.Second, run: func(ctx context.Context) error {
		base, tok, err := s.appToken(ctx, uid, "habits", "habits:read")
		if err != nil {
			return err
		}
		errs := make([]error, len(days))
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for i, d := range days {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				errs[i] = s.callJSON(ctx, "habits", http.MethodGet, base+"/api/v1/days/"+d.Format("2006-01-02"), map[string]string{"Authorization": "Bearer " + tok}, nil, &pages[i])
			}()
		}
		wg.Wait()
		return repFirstErr(errs...)
	}}})
	if err != nil {
		return "", nil, err
	}

	title := from.Format("Monday 2 January 2006")
	if !from.Equal(to) {
		title = from.Format("2 Jan 2006") + " – " + to.Format("2 Jan 2006")
	}
	var sections []string

	type habitSum struct {
		name               string
		done, missed, open int
	}
	type trackerSum struct {
		name, unit, kind string
		values           []float64
	}
	var hOrder, tOrder []int64
	hSum := map[int64]*habitSum{}
	tSum := map[int64]*trackerSum{}
	for _, p := range pages {
		for _, h := range p.Habits {
			if !h.Scheduled {
				continue
			}
			hs := hSum[h.ID]
			if hs == nil {
				hs = &habitSum{name: h.Name}
				hSum[h.ID] = hs
				hOrder = append(hOrder, h.ID)
			}
			switch h.Status {
			case 1:
				hs.done++
			case -1:
				hs.missed++
			default:
				hs.open++
			}
		}
		for _, t := range p.Trackers {
			ts := tSum[t.ID]
			if ts == nil {
				ts = &trackerSum{name: t.Name, unit: t.Unit, kind: t.Kind}
				tSum[t.ID] = ts
				tOrder = append(tOrder, t.ID)
			}
			if t.Value != nil {
				ts.values = append(ts.values, *t.Value)
			}
		}
	}
	var numbers []string
	for _, id := range hOrder {
		h := hSum[id]
		line := fmt.Sprintf("- **%s**: ✓ %d · ✗ %d", h.name, h.done, h.missed)
		if h.open > 0 {
			line += fmt.Sprintf(" · ○ %d", h.open)
		}
		if n := h.done + h.missed; n > 0 {
			line += fmt.Sprintf(" · %d %%", int(math.Round(float64(h.done)/float64(n)*100)))
		}
		numbers = append(numbers, line)
	}
	for _, id := range tOrder {
		t := tSum[id]
		if len(t.values) == 0 {
			continue
		}
		if t.kind == "yesno" {
			yes := 0
			for _, v := range t.values {
				if v >= 0.5 {
					yes++
				}
			}
			numbers = append(numbers, fmt.Sprintf("- **%s**: yes %d · no %d", t.name, yes, len(t.values)-yes))
			continue
		}
		sum, lo, hi := 0.0, t.values[0], t.values[0]
		for _, v := range t.values {
			sum += v
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
		unit := ""
		if t.unit != "" {
			unit = " " + t.unit
		}
		n := "days"
		if len(t.values) == 1 {
			n = "day"
		}
		numbers = append(numbers, fmt.Sprintf("- **%s**: avg %s%s · %s–%s%s · %d %s", t.name, repHabitNumber(sum/float64(len(t.values)), 1), unit,
			repHabitNumber(lo, 1), repHabitNumber(hi, 1), unit, len(t.values), n))
	}
	if len(numbers) > 0 {
		sections = append(sections, "### The period in numbers\n\n"+strings.Join(numbers, "\n"))
	}
	for i := len(days) - 1; i >= 0; i-- {
		p := pages[i]
		var lines, marks, vals []string
		for _, h := range p.Habits {
			if !h.Scheduled {
				continue
			}
			mark := "○ "
			switch h.Status {
			case 1:
				mark = "✓ "
			case -1:
				mark = "✗ "
			}
			marks = append(marks, mark+h.Name)
		}
		if len(marks) > 0 {
			lines = append(lines, "- "+strings.Join(marks, " · "))
		}
		for _, t := range p.Trackers {
			if t.Value == nil {
				continue
			}
			var v string
			if t.Kind == "yesno" {
				v = "No"
				if *t.Value >= 0.5 {
					v = "Yes"
				}
			} else {
				v = repHabitNumber(*t.Value, repHabitDecimals(t.Kind, t.Step))
				if t.Unit != "" {
					v += " " + t.Unit
				}
			}
			vals = append(vals, t.Name+" "+v)
		}
		if len(vals) > 0 {
			lines = append(lines, "- "+strings.Join(vals, " · "))
		}
		if p.Important != "" {
			lines = append(lines, "- **Important:** "+p.Important)
		}
		if p.Journal != "" {
			lines = append(lines, "- "+p.Journal)
		}
		if p.JournalBad != "" {
			lines = append(lines, "- − "+p.JournalBad)
		}
		if len(lines) > 0 {
			sections = append(sections, "### "+days[i].Format("Mon 2 Jan")+"\n\n"+strings.Join(lines, "\n"))
		}
	}
	if len(sections) == 0 {
		return "", fails, nil
	}
	return "## Habits · " + title + "\n\n" + strings.Join(sections, "\n\n") + "\n", fails, nil
}
