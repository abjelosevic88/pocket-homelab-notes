// Package diff compares two versions of a note line by line (Myers' algorithm)
// for the history view.
package diff

import "strings"

// Op is one line of a diff: "=" unchanged, "-" only in the old text, "+" only in the new.
type Op struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Lines returns the line diff between a and b.
func Lines(a, b string) []Op {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	if a == "" {
		x = nil
	}
	if b == "" {
		y = nil
	}
	n, m := len(x), len(y)
	max := n + m
	if max == 0 {
		return nil
	}
	v := make([]int, 2*max+1)
	var trace [][]int
	off := max
	found := false
	for d := 0; d <= max && !found; d++ {
		snap := make([]int, len(v))
		copy(snap, v)
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var px int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				px = v[off+k+1]
			} else {
				px = v[off+k-1] + 1
			}
			py := px - k
			for px < n && py < m && x[px] == y[py] {
				px++
				py++
			}
			v[off+k] = px
			if px >= n && py >= m {
				found = true
				break
			}
		}
	}
	// Walk back through the saved frontiers.
	var ops []Op
	px, py := n, m
	for d := len(trace) - 1; d >= 0 && (px > 0 || py > 0); d-- {
		vv := trace[d]
		k := px - py
		var prevK int
		if k == -d || (k != d && vv[off+k-1] < vv[off+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vv[off+prevK]
		prevY := prevX - prevK
		for px > prevX && py > prevY {
			px--
			py--
			ops = append(ops, Op{"=", x[px]})
		}
		if d > 0 {
			if px == prevX {
				py--
				ops = append(ops, Op{"+", y[py]})
			} else {
				px--
				ops = append(ops, Op{"-", x[px]})
			}
		}
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

// Stat counts added and removed lines.
func Stat(ops []Op) (added, removed int) {
	for _, o := range ops {
		switch o.Kind {
		case "+":
			added++
		case "-":
			removed++
		}
	}
	return
}

// Compact keeps changed lines with ctx unchanged lines around them; longer
// unchanged stretches become one "…" op.
func Compact(ops []Op, ctx int) []Op {
	keep := make([]bool, len(ops))
	for i, o := range ops {
		if o.Kind != "=" {
			for j := i - ctx; j <= i+ctx; j++ {
				if j >= 0 && j < len(ops) {
					keep[j] = true
				}
			}
		}
	}
	var out []Op
	skipped := false
	for i, o := range ops {
		if keep[i] {
			out = append(out, o)
			skipped = false
		} else if !skipped {
			out = append(out, Op{"…", ""})
			skipped = true
		}
	}
	return out
}
