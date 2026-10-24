package codehost

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSetRefs(t *testing.T) {
	oid, oid2 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, c := range []struct {
		name                          string
		state                         string
		mergedAt, head, merge, base   string
		wantAt                        time.Time
		wantHead, wantMerge, wantBase string
	}{
		{"merged", "MERGED", "2026-10-14T12:00:00Z", oid, oid2, "main",
			time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), oid, oid2, "main"},
		{"azure fractions and offset", "MERGED", "2026-10-14T14:00:00.1234567+02:00", oid, oid2, "release/1.2",
			time.Date(2026, 10, 14, 12, 0, 0, 123456700, time.UTC), oid, oid2, "release/1.2"},
		{"open keeps no merge", "OPEN", "2026-10-14T12:00:00Z", oid, oid2, "main",
			time.Time{}, oid, "", "main"},
		{"year 2000 or before", "MERGED", "0001-01-01T00:00:00Z", "", "", "",
			time.Time{}, "", "", ""},
		{"not a time", "MERGED", "yesterday", "", "", "",
			time.Time{}, "", "", ""},
		{"short or upper oid", "MERGED", "", "abc123", strings.ToUpper(oid), "",
			time.Time{}, "", "", ""},
		{"bad branch", "OPEN", "", "", "", "-rf",
			time.Time{}, "", "", ""},
		{"branch with space", "OPEN", "", "", "", "a b",
			time.Time{}, "", "", ""},
	} {
		pr := PR{State: c.state}
		pr.setRefs(c.mergedAt, c.head, c.merge, c.base)
		if !pr.MergedAt.Equal(c.wantAt) || pr.Head != c.wantHead || pr.Merge != c.wantMerge || pr.Base != c.wantBase {
			t.Errorf("%s: got %v %q %q %q", c.name, pr.MergedAt, pr.Head, pr.Merge, pr.Base)
		}
	}
	// A field that fails keeps what the PR had.
	pr := PR{State: "MERGED", Head: oid, Base: "main"}
	pr.setRefs("", "nope", "", "")
	if pr.Head != oid || pr.Base != "main" {
		t.Errorf("kept fields lost: %+v", pr)
	}
}

// The hosts parsed MergedAt with RFC3339 (GitHub, mods) and RFC3339Nano
// (Azure); setRefs uses RFC3339Nano. Both layouts accept the same times.
func TestSetRefsTimeLayouts(t *testing.T) {
	for _, s := range []string{
		"2026-10-14T12:00:00Z", "2026-10-14T12:00:00.5Z", "2026-10-14T12:00:00.1234567+02:00",
		"2026-10-14T12:00:00.123456789123Z", "2026-10-14 12:00:00Z", "2026-10-14T12:00:00",
		"2026-10-14T12:00:00,5Z", "2026-10-14T12:00:00.Z", "2026-10-14T24:00:00Z", "",
	} {
		a, errA := time.Parse(time.RFC3339, s)
		b, errB := time.Parse(time.RFC3339Nano, s)
		if (errA == nil) != (errB == nil) || !a.Equal(b) {
			t.Errorf("%q: RFC3339 %v, %v; RFC3339Nano %v, %v", s, a, errA, b, errB)
		}
	}
}

// prsHost answers PR by the URL, else the branch, it is asked about.
type prsHost struct {
	NoHost
	byURL, byBranch map[string]PR
	asked           []Ref
}

func (h *prsHost) PR(_ string, ref Ref) (PR, error) {
	h.asked = append(h.asked, ref)
	if pr, ok := h.byURL[ref.URL]; ok && ref.URL != "" {
		return pr, nil
	}
	if pr, ok := h.byBranch[ref.Branch]; ok && ref.URL == "" {
		return pr, nil
	}
	return PR{}, ErrNoPR
}

// T728: a report's PR line counts only for a PR of the thread's branch.
func TestThreadPR(t *testing.T) {
	own := PR{Number: 777, URL: "https://github.com/o/r/pull/777", Branch: "tm/x/t-1"}
	release := PR{Number: 776, URL: "https://github.com/o/r/pull/776", Branch: "formula-0.51.0", State: "MERGED"}
	h := &prsHost{byURL: map[string]PR{own.URL: own, release.URL: release}, byBranch: map[string]PR{own.Branch: own}}
	for _, c := range []struct {
		url       string
		want, oth int
		asks      int
	}{{own.URL, 777, 0, 1}, {release.URL, 777, 776, 2}, {"https://github.com/o/r/pull/9", 777, 0, 2}, {"", 777, 0, 1}} {
		h.asked = nil
		pr, other, err := ThreadPR(h, "repo", c.url, own.Branch)
		if err != nil || pr.Number != c.want || other.Number != c.oth || len(h.asked) != c.asks {
			t.Errorf("%q: %d %d %v, asked %+v", c.url, pr.Number, other.Number, err, h.asked)
		}
	}
	h.byBranch = nil
	if _, other, err := ThreadPR(h, "repo", release.URL, own.Branch); !errors.Is(err, ErrNoPR) || other.Number != 776 {
		t.Errorf("no PR of the branch: %v %d", err, other.Number)
	}
}
