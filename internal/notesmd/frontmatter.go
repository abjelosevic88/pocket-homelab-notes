package notesmd

import (
	"strconv"
	"strings"
	"time"
)

// Imported is what an imported note's YAML front matter says about the note
// itself (Pocket Move writes it for Evernote, Keep, Notion and Apple Notes).
type Imported struct {
	Title   string
	Created time.Time
	Updated time.Time
}

// TakeFrontMatter reads title, created and updated from a note's front matter
// and returns the note without those three lines. Everything else in the front
// matter (tags above all, which the index reads from there) stays; a front
// matter left empty is dropped. A note without front matter comes back as it is.
func TakeFrontMatter(content string) (Imported, string) {
	var meta Imported
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return meta, content
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if t := strings.TrimSpace(lines[i]); t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return meta, content
	}
	var keep []string
	for _, raw := range lines[1:end] {
		line := strings.TrimRight(raw, "\r")
		key, val, ok := strings.Cut(line, ":")
		if ok && key == strings.TrimSpace(key) { // top-level keys only
			v := unquote(strings.TrimSpace(val))
			switch strings.ToLower(key) {
			case "title":
				meta.Title = v
				continue
			case "created", "date", "created_at":
				if t, ok := parseWhen(v); ok {
					meta.Created = t
					continue
				}
			case "updated", "modified", "updated_at":
				if t, ok := parseWhen(v); ok {
					meta.Updated = t
					continue
				}
			}
		}
		keep = append(keep, line)
	}
	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimLeft(body, "\r\n")
	if strings.TrimSpace(strings.Join(keep, "")) == "" {
		return meta, body
	}
	return meta, "---" + nl + strings.Join(keep, nl) + nl + "---" + nl + body
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"') {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

var whenLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05",
	"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02",
}

func parseWhen(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	for _, l := range whenLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
