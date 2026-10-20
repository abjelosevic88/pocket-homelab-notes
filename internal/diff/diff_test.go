package diff

import (
	"math/rand"
	"strings"
	"testing"
)

func apply(ops []Op) (a, b string) {
	var x, y []string
	for _, o := range ops {
		switch o.Kind {
		case "=":
			x, y = append(x, o.Text), append(y, o.Text)
		case "-":
			x = append(x, o.Text)
		case "+":
			y = append(y, o.Text)
		}
	}
	return strings.Join(x, "\n"), strings.Join(y, "\n")
}

func TestLines(t *testing.T) {
	ops := Lines("a\nb\nc\nd", "a\nc\nd\ne")
	if a, b := apply(ops); a != "a\nb\nc\nd" || b != "a\nc\nd\ne" {
		t.Fatalf("roundtrip %q %q", a, b)
	}
	if ad, rm := Stat(ops); ad != 1 || rm != 1 {
		t.Fatalf("stat %d %d", ad, rm)
	}
	r := rand.New(rand.NewSource(1))
	words := []string{"x", "y", "z", "w"}
	for i := 0; i < 300; i++ {
		var p, q []string
		for j := r.Intn(12); j > 0; j-- {
			p = append(p, words[r.Intn(4)])
		}
		for j := r.Intn(12); j > 0; j-- {
			q = append(q, words[r.Intn(4)])
		}
		a, b := strings.Join(p, "\n"), strings.Join(q, "\n")
		if ga, gb := apply(Lines(a, b)); ga != a || gb != b {
			t.Fatalf("random %q → %q: got %q %q", a, b, ga, gb)
		}
	}
	if len(Lines("", "")) != 0 || len(Lines("", "a")) != 1 {
		t.Fatal("empty")
	}
}
