// Package notesmd reads what the index needs out of a note's Markdown: tags,
// links to other notes, tasks, a short excerpt and a word count. It is a
// line-based reader, not a full Markdown parser: fenced code is skipped, and
// that is the one structure that changes what a line means here.
package notesmd

import (
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Task is one checkbox line.
type Task struct {
	Line   int    `json:"line"` // 0-based line number in the note
	Text   string `json:"text"`
	Done   bool   `json:"done"`
	Due    string `json:"due,omitempty"` // YYYY-MM-DD
	Indent int    `json:"indent"`
}

// Info is everything the index keeps about a note's text.
type Info struct {
	Tags      []string
	Links     []string // [[wiki link]] titles (lower-cased), "path:" relative .md links, "id:" links to a note by id
	Tasks     []Task
	Excerpt   string
	Words     int
	FirstLine string
}

var (
	fenceRe   = regexp.MustCompile("^\\s*(```+|~~~+)")
	taskRe    = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+\[([ xX])\]\s?(.*)$`)
	tagRe     = regexp.MustCompile(`(?:^|[\s(\[,;])#([\p{L}\p{N}_][\p{L}\p{N}_/\-]*)`)
	wikiRe    = regexp.MustCompile(`\[\[([^\[\]|#]+)(?:#[^\[\]|]*)?(?:\|[^\[\]]*)?\]\]`)
	mdLinkRe  = regexp.MustCompile(`\]\(([^()\s]+\.(?:md|txt))(?:#[^)]*)?\)`)
	idLinkRe  = regexp.MustCompile(`\]\((?:https?://[^)\s]*?)?/(?:index\.php/)?(?:apps/notes/)?note/(\d+)\)`)
	dueRe     = regexp.MustCompile(`(?:📅\s*|\bdue:\s*|@due\()(\d{4}-\d{2}-\d{2})\)?`)
	inlineRe  = regexp.MustCompile("`[^`]*`")
	urlRe     = regexp.MustCompile(`https?://\S+`)
	stripRe   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)|\[([^\]]*)\]\([^)]*\)|[*_~>#]+|<[^>]+>`)
	spacesRe  = regexp.MustCompile(`\s+`)
	numericRe = regexp.MustCompile(`^[0-9]+$`)
)

// Lines splits a note into lines the way the task toggles count them.
func Lines(content string) []string { return strings.Split(content, "\n") }

// Analyze reads a note.
func Analyze(content string) Info {
	var info Info
	tags := map[string]bool{}
	links := map[string]bool{}
	inFence, fence := false, ""
	inFront := false
	var excerpt []string
	lines := Lines(content)
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if i == 0 && strings.TrimSpace(line) == "---" {
			inFront = true
			continue
		}
		if inFront {
			if t := strings.TrimSpace(line); t == "---" || t == "..." {
				inFront = false
				continue
			}
			for _, tg := range frontmatterTags(line) {
				tags[tg] = true
			}
			continue
		}
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			marker := m[1][:1]
			if !inFence {
				inFence, fence = true, marker
			} else if marker == fence {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if info.FirstLine == "" && strings.TrimSpace(line) != "" {
			info.FirstLine = strings.TrimSpace(line)
		}
		info.Words += countWords(line)
		if m := taskRe.FindStringSubmatch(line); m != nil {
			t := Task{Line: i, Text: strings.TrimSpace(m[3]), Done: m[2] != " ", Indent: len(strings.ReplaceAll(m[1], "\t", "  "))}
			if d := dueRe.FindStringSubmatch(t.Text); d != nil {
				if _, err := time.Parse("2006-01-02", d[1]); err == nil {
					t.Due = d[1]
				}
			}
			info.Tasks = append(info.Tasks, t)
		}
		bare := inlineRe.ReplaceAllString(line, " ")
		bare = urlRe.ReplaceAllString(bare, " ")
		if !strings.HasPrefix(strings.TrimSpace(line), "#") || !isHeading(line) {
			for _, m := range tagRe.FindAllStringSubmatch(bare, -1) {
				tg := strings.Trim(strings.ToLower(m[1]), "/-")
				if tg != "" && !numericRe.MatchString(tg) {
					tags[tg] = true
				}
			}
		} else {
			// A heading's own '#' are not tags, but tags written after its text are.
			rest := strings.TrimLeft(strings.TrimSpace(bare), "#")
			for _, m := range tagRe.FindAllStringSubmatch(rest, -1) {
				tg := strings.Trim(strings.ToLower(m[1]), "/-")
				if tg != "" && !numericRe.MatchString(tg) {
					tags[tg] = true
				}
			}
		}
		for _, m := range wikiRe.FindAllStringSubmatch(line, -1) {
			if t := strings.ToLower(strings.TrimSpace(m[1])); t != "" {
				links[t] = true
			}
		}
		for _, m := range idLinkRe.FindAllStringSubmatch(line, -1) {
			links["id:"+m[1]] = true
		}
		for _, m := range mdLinkRe.FindAllStringSubmatch(line, -1) {
			if strings.Contains(m[1], "://") {
				continue
			}
			links["path:"+strings.ToLower(m[1])] = true
		}
		if len(strings.Join(excerpt, " ")) < 220 {
			if t := plain(line); t != "" && !(len(excerpt) == 0 && isHeading(line)) {
				excerpt = append(excerpt, t)
			}
		}
	}
	info.Tags = keys(tags)
	info.Links = keys(links)
	info.Excerpt = truncate(strings.Join(excerpt, " "), 220)
	return info
}

func isHeading(line string) bool {
	t := strings.TrimSpace(line)
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	return n > 0 && n <= 6 && (len(t) == n || t[n] == ' ')
}

func frontmatterTags(line string) []string {
	t := strings.TrimSpace(line)
	var vals string
	switch {
	case strings.HasPrefix(strings.ToLower(t), "tags:"):
		vals = strings.TrimSpace(t[5:])
	case strings.HasPrefix(t, "- ") && !strings.Contains(t, ":"):
		vals = strings.TrimSpace(t[2:]) // list item under tags:, close enough for a notes folder
	default:
		return nil
	}
	vals = strings.Trim(vals, "[]")
	var out []string
	for _, v := range strings.Split(vals, ",") {
		v = strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'#`))
		if v != "" && !numericRe.MatchString(v) && !strings.ContainsAny(v, " :") {
			out = append(out, v)
		}
	}
	return out
}

func countWords(line string) int {
	n, in := 0, false
	for _, r := range line {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if !in {
				n++
				in = true
			}
		} else if r != '\'' && r != '’' && r != '-' {
			in = false
		}
	}
	return n
}

func plain(line string) string {
	t := strings.TrimSpace(line)
	t = taskRe.ReplaceAllString(t, "$3")
	t = strings.TrimLeft(t, "-*+ ")
	if len(t) > 2 && t[0] >= '0' && t[0] <= '9' {
		if i := strings.IndexAny(t, ".)"); i > 0 && i < 4 {
			t = strings.TrimSpace(t[i+1:])
		}
	}
	t = stripRe.ReplaceAllString(t, "$1")
	t = strings.ReplaceAll(t, "|", " ")
	t = strings.Trim(t, " :-")
	return strings.TrimSpace(spacesRe.ReplaceAllString(t, " "))
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	cut := string(r[:n])
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TitleFromContent is what a note without a title is called: its first line,
// without the Markdown around it, at most 100 characters.
func TitleFromContent(content string) string {
	for _, line := range Lines(content) {
		t := strings.TrimSpace(line)
		if t == "" || t == "---" {
			continue
		}
		t = strings.TrimSpace(strings.TrimLeft(t, "#"))
		t = plain(t)
		if t == "" {
			continue
		}
		return truncate(t, 100)
	}
	return ""
}

// SafeName turns a title into a file name the way Nextcloud Notes does: the
// characters a file name may not hold become spaces, a leading dot goes.
func SafeName(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case strings.ContainsRune(`\/:*?"<>|`, r), r < 32, r == 0x7f:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	s = strings.TrimLeft(s, ". ")
	for len(s) > 200 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		s = "New note"
	}
	return s
}

// SafeFolder cleans a category ("Projects/Athena") part by part.
func SafeFolder(category string) string {
	var parts []string
	for _, p := range strings.Split(strings.ReplaceAll(category, "\\", "/"), "/") {
		p = strings.TrimSpace(p)
		if p == "" || p == "." || p == ".." {
			continue
		}
		parts = append(parts, SafeName(p))
	}
	return strings.Join(parts, "/")
}

// ToggleTask ticks or unticks the task on line (0-based). The line must still
// hold text (as the client saw it); otherwise the first open task with that
// text is used. ok is false when no such task exists.
func ToggleTask(content string, line int, text string, done bool) (string, bool) {
	lines := Lines(content)
	match := func(i int) bool {
		m := taskRe.FindStringSubmatch(strings.TrimRight(lines[i], "\r"))
		return m != nil && (text == "" || strings.TrimSpace(m[3]) == strings.TrimSpace(text))
	}
	idx := -1
	if line >= 0 && line < len(lines) && match(line) {
		idx = line
	} else {
		for i := range lines {
			if match(i) {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return content, false
	}
	l := lines[idx]
	open := strings.Index(l, "[")
	if open < 0 || open+2 >= len(l) {
		return content, false
	}
	mark := " "
	if done {
		mark = "x"
	}
	lines[idx] = l[:open+1] + mark + l[open+2:]
	return strings.Join(lines, "\n"), true
}

// SetFirstHeading puts title into the note's first line when that line is a
// heading (or the note starts with the old title as plain text): renaming a
// note keeps the "# Title" line in step, as Nextcloud Notes does.
func SetFirstHeading(content, oldTitle, newTitle string) string {
	lines := Lines(content)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if isHeading(t) {
			level := strings.IndexFunc(t, func(r rune) bool { return r != '#' })
			if strings.TrimSpace(t[level:]) == strings.TrimSpace(oldTitle) {
				lines[i] = strings.Repeat("#", level) + " " + newTitle
			}
		} else if t == strings.TrimSpace(oldTitle) {
			lines[i] = newTitle
		}
		break
	}
	return strings.Join(lines, "\n")
}
