package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Books in Pocket Bookshelf and episodes in Pocket Podcasts for "Link to
// something". The link goes to the thing's page in its app, which is what
// that app's Notes box looks for (docs/notes-from-apps.md), so a note linked
// here is listed on the book's or episode's page.

var bookStatus = map[string]string{"want": "want to read", "reading": "reading", "read": "read", "abandoned": "stopped"}

func linkBooks(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	var books []struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Authors   string `json:"authors"`
		Published string `json:"published"`
		Status    string `json:"status"`
	}
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(linkPerSource)}}
	if err := s.callJSON(ctx, "bookshelf", http.MethodGet, c.API("/api/v1/books?"+v.Encode()), map[string]string{"Authorization": "Bearer " + c.Key}, nil, &books); err != nil {
		return nil, "", err
	}
	items := []linkItem{}
	for _, b := range books {
		if len(items) == linkPerSource {
			break
		}
		id := strconv.FormatInt(b.ID, 10)
		title := firstNonEmpty(strings.TrimSpace(b.Title), "Book "+id)
		label := "📖 " + title
		if a := strings.TrimSpace(b.Authors); a != "" {
			label += " — " + a
		}
		var detail []string
		for _, part := range []string{b.Authors, b.Published, bookStatus[b.Status]} {
			if part = strings.TrimSpace(part); part != "" {
				detail = append(detail, part)
			}
		}
		target := c.Link("/books/" + id)
		items = append(items, linkItem{ID: "book-" + id, Title: title, Detail: strings.Join(detail, " · "), URL: target, Icon: "book", Markdown: webLinkMarkdown(label, target)})
	}
	return items, "", nil
}

func linkEpisodes(s *Server, ctx context.Context, c *svcCred, q string) ([]linkItem, string, error) {
	var res struct {
		Episodes []struct {
			ID           int64   `json:"id"`
			Title        string  `json:"title"`
			PodcastTitle string  `json:"podcast_title"`
			PubDate      string  `json:"pub_date"`
			Duration     float64 `json:"duration"`
			PlayPosition float64 `json:"play_position"`
			Completed    bool    `json:"completed"`
		} `json:"episodes"`
	}
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(linkPerSource)}}
	if err := s.callJSON(ctx, "podcasts", http.MethodGet, c.API("/api/v1/episodes?"+v.Encode()), map[string]string{"Authorization": "Bearer " + c.Key}, nil, &res); err != nil {
		return nil, "", err
	}
	items := []linkItem{}
	for _, e := range res.Episodes {
		if len(items) >= linkPerSource {
			break
		}
		id := strconv.FormatInt(e.ID, 10)
		title := firstNonEmpty(strings.TrimSpace(e.Title), "Episode "+id)
		show := strings.TrimSpace(e.PodcastTitle)
		label := "🎧 " + title
		if show != "" && !strings.HasSuffix(title, show) {
			label += " — " + show
		}
		var detail []string
		for _, part := range []string{show, shortDay(e.PubDate), minutes(e.Duration)} {
			if part != "" {
				detail = append(detail, part)
			}
		}
		target := c.Link("/episodes/" + id)
		items = append(items, linkItem{ID: "episode-" + id, Title: title, Detail: strings.Join(detail, " · "), URL: target, Icon: "podcast", Markdown: webLinkMarkdown(label, target)})
		// Part-way through: the same episode at the moment reached, which the
		// episode's page offers to play from.
		if at := int(e.PlayPosition); at >= 5 && !e.Completed {
			at := strconv.Itoa(at)
			when := clockTime(e.PlayPosition)
			items = append(items, linkItem{ID: "episode-" + id + "-at", Title: title + " — at " + when, Detail: "where you are in it", URL: target + "?t=" + at, Icon: "moment", Markdown: webLinkMarkdown(label+" at "+when, target+"?t="+at)})
		}
	}
	return items, "", nil
}

// minutes is a length as "45 min" or "1 h 5 min" ("" when unknown).
func minutes(sec float64) string {
	m := int(sec+30) / 60
	switch {
	case sec <= 0:
		return ""
	case m < 60:
		return fmt.Sprintf("%d min", max(m, 1))
	case m%60 == 0:
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// clockTime is a moment as "12:34" or "1:02:03".
func clockTime(sec float64) string {
	t := int(sec)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t%3600/60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}
