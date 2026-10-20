package notesmd

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnalyze(t *testing.T) {
	note := strings.Join([]string{
		"---",
		"tags: [work, Athena]",
		"---",
		"# Plan #heading-tag",
		"",
		"Talk to Arno about #memory and the #chatbot/core, not about IKP #362.",
		"See [[Memory and context FF]] and [the PR](../Projects/Athena PR’s.md) — https://x.y/#nottag",
		"- [ ] run memory tests 📅 2026-10-21",
		"  - [x] done thing",
		"1. [ ] numbered due:2026-11-01",
		"```sh",
		"# not a heading #nottag",
		"- [ ] not a task",
		"```",
		"`#inline` stays out",
	}, "\n")
	in := Analyze(note)
	if want := []string{"athena", "chatbot/core", "heading-tag", "memory", "work"}; !reflect.DeepEqual(in.Tags, want) {
		t.Errorf("tags %v", in.Tags)
	}
	if want := []string{"memory and context ff"}; !reflect.DeepEqual(in.Links, want) {
		t.Errorf("links %v", in.Links)
	}
	if len(in.Tasks) != 3 || in.Tasks[0].Due != "2026-10-21" || in.Tasks[0].Line != 7 || !in.Tasks[1].Done || in.Tasks[1].Indent != 2 || in.Tasks[2].Due != "2026-11-01" {
		t.Errorf("tasks %+v", in.Tasks)
	}
	if !strings.HasPrefix(in.Excerpt, "Talk to Arno") {
		t.Errorf("excerpt %q", in.Excerpt)
	}
}

func TestToggleAndNames(t *testing.T) {
	c := "# T\n- [ ] a\n- [ ] b\n"
	out, ok := ToggleTask(c, 2, "b", true)
	if !ok || out != "# T\n- [ ] a\n- [x] b\n" {
		t.Fatalf("toggle %q", out)
	}
	out, ok = ToggleTask(out, 5, "a", true) // line moved: found by text
	if !ok || !strings.Contains(out, "- [x] a") {
		t.Fatalf("toggle by text %q", out)
	}
	if _, ok := ToggleTask(c, 1, "zzz", true); ok {
		t.Fatal("toggled a task that is not there")
	}
	if got := SafeName("https://github.com/a/b"); got != "https   github.com a b" {
		t.Errorf("SafeName %q", got)
	}
	if got := SafeName("..."); got != "New note" {
		t.Errorf("SafeName dots %q", got)
	}
	if got := TitleFromContent("\n## Daily Monday, October 19\n1. run"); got != "Daily Monday, October 19" {
		t.Errorf("title %q", got)
	}
	if got := SetFirstHeading("# Old\n\ntext", "Old", "New"); got != "# New\n\ntext" {
		t.Errorf("heading %q", got)
	}
	if got := SafeFolder(" Projects / ../Athena/"); got != "Projects/Athena" {
		t.Errorf("folder %q", got)
	}
}
