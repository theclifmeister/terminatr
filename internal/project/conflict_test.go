package project

import (
	"reflect"
	"strings"
	"testing"
)

func TestConflicts(t *testing.T) {
	retries := ParseNote("memory/retries.md", "+++\ntitle = \"Retries stay at 4 for the gh poller\"\ntouches = [\"internal/ticker/\"]\n+++\n"+
		"## Decided\nKeep retries at 4; poll every 30s.\n## Ruled out\nExponential backoff with jitter: gh rate limits.\n")
	repoWide := ParseNote("memory/api.md", "+++\ntouches = [\"repo:api\"]\n+++\n# Api\n## Decided\nPort 8080.\n")
	free := ParseNote("memory/free.md", "# Free\n## Decided\nKeep 4.\n")
	notes := []Note{retries, repoWide, free}
	patch := func(lines ...string) string {
		return "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,3 +1,3 @@\n" + strings.Join(lines, "\n") + "\n"
	}
	for _, c := range []struct {
		name string
		repo string
		d    FileDiff
		want []Conflict
	}{
		{"changes a decided number", "", FileDiff{Path: "internal/ticker/pr.go", Patch: patch(" func poll() {", "-\tretries := 4", "+\tretries := 5", " }")},
			[]Conflict{{Note: "memory/retries.md", Title: retries.Title, File: "internal/ticker/pr.go", Kind: ConflictDecided, Decided: "4", Changed: "5"}}},
		{"a unit after the number", "", FileDiff{Path: "internal/ticker/pr.go", Patch: patch("-\tevery = 30 * time.Second", "+\tevery = 10 * time.Second")},
			[]Conflict{{Note: "memory/retries.md", Title: retries.Title, File: "internal/ticker/pr.go", Kind: ConflictDecided, Decided: "30", Changed: "10"}}},
		{"keeps the number", "", FileDiff{Path: "internal/ticker/pr.go", Patch: patch("-\tretries := 4 // old", "+\tretries := 4 // T12, v1.2")}, nil},
		{"adds a line only", "", FileDiff{Path: "internal/ticker/pr.go", Patch: patch("+\twait := 7")}, nil},
		{"adds ruled-out words", "", FileDiff{Path: "internal/ticker/pr.go", Patch: patch("+\td := backoffJitter(exponential)")},
			[]Conflict{{Note: "memory/retries.md", Title: retries.Title, File: "internal/ticker/pr.go", Kind: ConflictRuledOut, Terms: []string{"exponential", "backoff", "jitter"}}}},
		{"one ruled-out word", "", FileDiff{Path: "internal/ticker/pr.go", Patch: patch("+\td := backoff()")}, nil},
		{"moves ruled-out words", "", FileDiff{Path: "internal/ticker/pr.go", Patch: patch("-// backoff jitter", " x", "+// backoff jitter")}, nil},
		{"untouched file", "", FileDiff{Path: "internal/cli/pr.go", Patch: patch("-\tretries := 4", "+\tretries := 5")}, nil},
		{"renamed from a touched file", "", FileDiff{Path: "internal/poll/pr.go", Old: "internal/ticker/pr.go", Patch: patch("-\tretries := 4", "+\tretries := 5")},
			[]Conflict{{Note: "memory/retries.md", Title: retries.Title, File: "internal/poll/pr.go", Kind: ConflictDecided, Decided: "4", Changed: "5"}}},
		{"repo touch", "api", FileDiff{Path: "main.go", Patch: patch("-\tlisten(8080)", "+\tlisten(9090)")},
			[]Conflict{{Note: "memory/api.md", Title: "Api", File: "main.go", Kind: ConflictDecided, Decided: "8080", Changed: "9090"}}},
		{"other repo", "web", FileDiff{Path: "main.go", Patch: patch("-\tlisten(8080)", "+\tlisten(9090)")}, nil},
	} {
		if got := Conflicts(notes, c.repo, []FileDiff{c.d}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
	got := Conflicts(notes, "", []FileDiff{{Path: "internal/ticker/pr.go", Patch: patch("-\tretries := 4", "+\tretries := 5")}})
	if want := "internal/ticker/pr.go changes 4 to 5; memory/retries.md (Retries stay at 4 for the gh poller) decided 4"; len(got) != 1 || got[0].String() != want {
		t.Errorf("String: %v", got)
	}
}

func TestNumbers(t *testing.T) {
	if got, want := numbers("4 retries, T481, v0.30, 1.2.3, 2.5 and 30s; 4 again, x_9, 0x10"), []string{"4", "2.5", "30", "0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("numbers = %v, want %v", got, want)
	}
	set := identSet("maxRetries HTTPServer snake_case")
	for _, w := range []string{"max", "retries", "maxretries", "http", "server", "snake", "case"} {
		if !set[w] {
			t.Errorf("identSet lacks %q: %v", w, set)
		}
	}
}
