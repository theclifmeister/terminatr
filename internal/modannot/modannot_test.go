package modannot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetReadClean(t *testing.T) {
	dir := t.TempDir()
	spans, err := Clean([]Span{{Text: "do\x1b[31mcs\n", Role: "info"}, {Text: "  "}, {Text: "x", Role: "nope"}})
	if err != nil || len(spans) != 2 || spans[0].Text != "do[31mcs" || spans[1].Role != "" {
		t.Fatalf("Clean = %+v, %v", spans, err)
	}
	if _, err := Clean(make([]Span, MaxSpans+1)); err == nil {
		t.Fatal("too many spans accepted")
	}
	if ch, err := Set(dir, "demo", Task("T1"), "labels", spans); !ch || err != nil {
		t.Fatalf("Set = %v, %v", ch, err)
	}
	if ch, _ := Set(dir, "demo", Task("T1"), "labels", spans); ch {
		t.Fatal("same spans reported as a change")
	}
	got := Read(dir, "lab", "demo")[Task("T1")]
	if len(got) != 1 || got[0].Mod != "lab" || got[0].Key != "labels" || len(got[0].Spans) != 2 {
		t.Fatalf("Read = %+v", got)
	}
	if Read(dir, "lab", "other") != nil && len(Read(dir, "lab", "other")) != 0 {
		t.Fatal("another project's badges")
	}
	if _, err := Set(dir, "demo", Task("T1"), "Bad Key", spans); err == nil {
		t.Fatal("bad key accepted")
	}
	if ch, _ := Set(dir, "demo", Task("T1"), "labels", nil); !ch || len(Read(dir, "lab", "demo")) != 0 {
		t.Fatal("remove failed")
	}
}

func TestLimitsAndHostileFile(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < MaxKeys; i++ {
		if _, err := Set(dir, "p", Task("T1"), "k"+strings.Repeat("a", i), []Span{{Text: "x"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Set(dir, "p", Task("T1"), "extra", []Span{{Text: "x"}}); err == nil {
		t.Fatal("over MaxKeys accepted")
	}
	// A file the mod wrote itself is cleaned on read.
	os.WriteFile(filepath.Join(dir, File), []byte(`{"projects":{"p":{"task:T2":{"ok":[{"text":"\u001b[2Jhi","role":"bad"}],"BAD":[{"text":"x"}]}}}}`), 0o600)
	got := Read(dir, "m", "p")["task:T2"]
	if len(got) != 1 || got[0].Spans[0].Text != "[2Jhi" {
		t.Fatalf("Read = %+v", got)
	}
	os.WriteFile(filepath.Join(dir, File), []byte("{nope"), 0o600)
	if Read(dir, "m", "p") != nil {
		t.Fatal("broken file read")
	}
}
