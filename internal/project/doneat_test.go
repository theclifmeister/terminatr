package project

import (
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeJournalMonth(t *testing.T, p *Project, month, text string) {
	t.Helper()
	if err := os.MkdirAll(p.Path("journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	z.Write([]byte(text))
	z.Close()
	if err := os.WriteFile(p.Path("journal", month+".md.gz"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDoneTimes reads what each actor's lines say, month archives before
// JOURNAL.md, and takes the last time a task became done.
func TestDoneTimes(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "demo-app"})
	if err != nil {
		t.Fatal(err)
	}
	writeJournalMonth(t, p, "2026-09", `2026-09-01T10:00:00Z coordinator task.status T1 started
2026-09-02T10:00:00Z human task.status T1 done
2026-09-03T10:00:00Z ticker task.done T2 merged (PR #5)
2026-09-04T10:00:00Z coordinator task.status T3 done: yes (approved by the user)
2026-09-05T10:00:00Z human (iphone: iPhone Duo) task.status T4 done
2026-09-06T10:00:00Z coordinator task.status T5 done (approved by the user)
`)
	writeJournalMonth(t, p, "2026-08", "2026-08-30T10:00:00Z human (app) task.status T6 done\n")
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte(`# Journal

2026-10-01T10:00:00Z coordinator task.status T1 review: back it comes
2026-10-02T10:00:00Z peer:mini (director x) task.status T1 done
2026-10-03T10:00:00Z human task.status T1 done: and a later word
2026-10-04T10:00:00Z human task.status T5 dropped: no
2026-10-05T10:00:00Z t-0007 note T9 task.status T9 done
2026-10-06T10:00:00Z coordinator task.status T8 doneish
2026-10-07T10:00:00Z coordinator task.edit T8 done
`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := p.doneTimes()
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{1: "2026-10-02T10:00:00Z", 2: "2026-09-03T10:00:00Z", 3: "2026-09-04T10:00:00Z", 4: "2026-09-05T10:00:00Z", 6: "2026-08-30T10:00:00Z"}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for id, at := range want {
		if got[id] != at {
			t.Errorf("T%d: %q, want %q", id, got[id], at)
		}
	}
}

// TestFillDoneAtNoGit: the fill is the same for a plain folder, a repo
// with no tags, several repos and a repo on another host, and git is
// never run: a git on the PATH that leaves a mark stays unmarked.
func TestFillDoneAtNoGit(t *testing.T) {
	setup(t)
	bin := t.TempDir()
	mark := filepath.Join(bin, "git-ran")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\ntouch '"+mark+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := func(remote string) string {
		t.Helper()
		dir := t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", args, err, out)
			}
		}
		return dir
	}
	a, b, c := repo("https://github.com/o/a"), repo("https://github.com/o/b"), repo("https://gitlab.example/o/c")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for slug, repos := range map[string][]string{"plain": nil, "one-untagged": {a}, "several": {a, b}, "other-host": {c}} {
		p, err := New(Options{Slug: slug, Repos: repos})
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		if err := os.WriteFile(p.Tasks().TasksPath(), []byte("# Tasks\n\n## Done\n\n### T1 Journal\nstatus: done · updated: 2026-10-30\n\n### T2 Hand, no line\nstatus: done · updated: 2026-10-30\n\n### T3 Note\nstatus: done · updated: 2026-10-30\n\ndone (2026-10-09): ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n2026-10-10T08:00:00Z human task.status T1 done\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Remove(mark) // New may look at the repos; the fill may not.
		if n, err := p.FillDoneAt(); err != nil || n != 3 {
			t.Fatalf("%s: filled %d, %v", slug, n, err)
		}
		if _, err := os.Stat(mark); err == nil {
			t.Errorf("%s: the fill ran git", slug)
		}
		board, _ := p.Tasks().Load()
		for id, at := range map[int]string{1: "2026-10-10T08:00:00Z", 2: "2026-10-30", 3: "2026-10-09"} {
			if got := board.Find(id).DoneAt; got != at {
				t.Errorf("%s: T%d done_at %q, want %q", slug, id, got, at)
			}
		}
	}
}

// TestFillDoneAtThreeTasks: a small project reads no journal it has not
// got, and has nothing to do once filled.
func TestFillDoneAtThreeTasks(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "small"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Tasks().TasksPath(), []byte("# Tasks\n\n## Done\n\n### T1 One\nstatus: done · updated: 2026-10-30\n\n### T2 Two\nstatus: done · updated: 2026-10-31\n\n### T3 Three\nstatus: done · updated: 2026-11-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p.Path("JOURNAL.md")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if n, err := p.FillDoneAt(); err != nil || n != 3 {
		t.Fatalf("filled %d, %v", n, err)
	}
	if n, err := p.FillDoneAt(); err != nil || n != 0 {
		t.Fatalf("again: %d, %v", n, err)
	}
}
