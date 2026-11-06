package releases

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
)

// TestTagTable is the design page's table (board-library, Which tags
// count as releases), row by row, then a few the rule must also answer.
func TestTagTable(t *testing.T) {
	for _, c := range []struct {
		tag, part, version string
		counts             bool
	}{
		{"v0.82.1", "", "0.82.1", true},
		{"1.4.0", "", "1.4.0", true},
		{"v1.2", "", "1.2", true},
		{"release-1.2", "", "1.2", true},
		{"api/v1.2", "api", "1.2", true},
		{"web/2.0.1", "web", "2.0.1", true},
		{"pkg@1.3.0", "pkg", "1.3.0", true},
		{"2026.11.06", "", "2026.11.06", true},
		{"v0.27.0-rc1", "", "", false},
		{"v1.2.3-beta.1", "", "", false},
		{"nightly-20261106", "", "", false},
		{"nightly/1.2.3", "", "", false},
		{"v1", "", "", false},
		{"latest", "", "", false},
		{"stable", "", "", false},
		{"prod-2026-11-06", "", "", false},
		{"20261106", "", "", false},
		{"build-4411", "", "", false},
		{"test/1.0.0", "", "", false},
		// Beyond the table.
		{"release/1.2", "", "1.2", true},
		{"release-v1.2.3", "", "1.2.3", true},
		{"@scope/pkg@1.3.0", "@scope/pkg", "1.3.0", true},
		{"services/api/v1.2", "services/api", "1.2", true},
		{"V2.0", "", "2.0", true},
		{"api/v1", "", "", false},
		{"api/latest", "", "", false},
		{"nightly/api/1.2", "", "", false},
		{"pkg-canary@1.2.0", "", "", false},
		{"/1.2", "", "", false},
		{"2026/1.2", "", "", false},
		{"1.2.", "", "", false},
		{"", "", "", false},
	} {
		r, ok := Classify(c.tag, nil)
		if ok != c.counts || r.Part != c.part || r.Version != c.version {
			t.Errorf("%q: %+v %v, want part %q version %q %v", c.tag, r, ok, c.part, c.version, c.counts)
		}
	}
}

func TestPatternsReplaceTheRule(t *testing.T) {
	prod := []string{"prod-*"}
	if r, ok := Classify("prod-2026-11-06", prod); !ok || r.Version != "prod-2026-11-06" || r.Part != "" {
		t.Errorf("a deploy under its pattern: %+v %v", r, ok)
	}
	if _, ok := Classify("v1.2.3", prod); ok {
		t.Error("the rule still applies beside patterns")
	}
	// A pattern admits what the rule leaves out, and shows what has the
	// rule's shape as the rule would.
	if r, ok := Classify("api/v1.2", []string{"api/*"}); !ok || r.Part != "api" || r.Version != "1.2" {
		t.Errorf("a part under a pattern: %+v %v", r, ok)
	}
	if _, ok := Classify("v0.27.0-rc1", []string{"v*"}); !ok {
		t.Error("a pattern is the user's word, a pre-release too")
	}
	for _, tag := range []string{"v1.2.3", "prod-1", "none"} {
		if _, ok := Classify(tag, []string{config.ReleaseTagsOff}); ok {
			t.Errorf("none admits %q", tag)
		}
	}
}

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		p, s string
		want bool
	}{
		{"prod-*", "prod-1", true}, {"prod-*", "prod-", true}, {"prod-*", "pro", false},
		{"*", "x/y", true}, {"a*/*z", "ab/cz", true}, {"a*/*z", "abz", false},
		{"exact", "exact", true}, {"exact", "exact2", false}, {"a*a", "a", false}, {"*-1", "x-1", true}, {"*-1", "x-2", false},
	} {
		if Match(c.p, c.s) != c.want {
			t.Errorf("Match(%q, %q) != %v", c.p, c.s, c.want)
		}
	}
}

func TestCount(t *testing.T) {
	day := func(d string, h int) time.Time {
		at, err := time.Parse(time.DateOnly, d)
		if err != nil {
			t.Fatal(err)
		}
		return at.Add(time.Duration(h) * time.Hour)
	}
	tags := []Tag{
		{"v0.9.0", "old", day("2026-10-01", 12)},
		{"v1.2", "c1", day("2026-10-05", 9)},
		{"v1.2.0", "c1", day("2026-10-05", 10)},
		{"1.2.0", "c1", day("2026-10-05", 11)},
		{"api/v1.2", "c2", day("2026-10-06", 9)},
		{"web/2.0.1", "c2", day("2026-10-06", 9)},
		{"v2.0.0-rc1", "c3", day("2026-10-07", 9)},
	}
	got := Count(tags, nil, "2026-10-05", time.UTC)
	var names []string
	for _, r := range got {
		names = append(names, r.Tag)
	}
	want := []string{"1.2.0", "api/v1.2", "web/2.0.1"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
	// The first record's day counts whole, in loc: a tag late on the
	// evening before, by this zone's clock, doesn't.
	east := time.FixedZone("east", 3*3600)
	if got := Count([]Tag{{"v1.0.0", "c", day("2026-10-04", 22)}}, nil, "2026-10-05", east); len(got) != 1 {
		t.Errorf("late, in another zone: %+v", got)
	}
	if got := Count([]Tag{{"v1.0.0", "c", day("2026-10-04", 20)}}, nil, "2026-10-05", east); len(got) != 0 {
		t.Errorf("before the first record: %+v", got)
	}
	if got := Count(tags, nil, "", time.UTC); got != nil {
		t.Errorf("no first record: %+v", got)
	}
	if got := Count(tags, []string{config.ReleaseTagsOff}, "2026-10-05", time.UTC); len(got) != 0 {
		t.Errorf("none: %+v", got)
	}
}
