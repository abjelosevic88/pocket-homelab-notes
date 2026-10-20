package server

import (
	"net/url"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-notes/internal/store"
)

// A note is about an item in another app when it links to the item's page.
func TestMentions(t *testing.T) {
	h := newHarness(t)
	_, tok := h.user("me@example.com")
	_, other := h.user("you@example.com")
	mk := func(tok, title, content string) store.Note {
		var n store.Note
		h.do(tok, "POST", "/api/v1/notes", map[string]any{"title": title, "content": content}, &n)
		return n
	}
	a := mk(tok, "Dune notes", "# Dune notes\n\n[📖 Dune](https://Bookshelf.example.com/books/12/)\n")
	b := mk(tok, "Quote", "Said at https://podcasts.example.com/episodes/7?t=95, and more.")
	mk(tok, "Other book", "[x](https://bookshelf.example.com/books/123)")
	mk(tok, "Just text", "bookshelf.example.com/books/12 without a link")
	mk(other, "Not mine", "[Dune](https://bookshelf.example.com/books/12)")

	var got []store.Note
	if c := h.do(tok, "GET", "/api/v1/mentions?url="+url.QueryEscape("https://bookshelf.example.com/books/12"), nil, &got); c != 200 || len(got) != 1 || got[0].ID != a.ID || got[0].Content != "" {
		t.Fatalf("book: %d %+v", c, got)
	}
	if h.do(tok, "GET", "/api/v1/mentions?url="+url.QueryEscape("http://podcasts.example.com/episodes/7"), nil, &got); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("episode at a time: %+v", got)
	}
	if c := h.do(tok, "GET", "/api/v1/mentions?url=nope", nil, nil); c != 400 {
		t.Fatalf("bad url: %d", c)
	}
}
