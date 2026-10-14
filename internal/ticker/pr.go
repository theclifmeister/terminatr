package ticker

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
)

// oidRE is a full SHA-1 commit id, as the hosts keep PR.Merge.
var oidRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// PR is the fixed set of fields the ticker keeps of a pull request
// (codehost.PR, docs/SPEC.md §7.5).
type PR = codehost.PR

// Summaries is PRs as Summary lines, for project.Context.
func Summaries(path, slug string) map[string]string {
	out := map[string]string{}
	for id, pr := range PRs(path, slug) {
		out[id] = pr.Summary()
	}
	return out
}

// PRs reads the PR fields the ticker keeps in its state file at path for
// the threads of project slug, by thread id. A missing or unreadable
// file gives none: the ticker hasn't polled yet.
func PRs(path, slug string) map[string]PR {
	prs, _ := ReadProject(path, slug)
	return prs
}

// Timing is when the ticker last did its slow work for a project.
type Timing struct {
	PRPolled  time.Time // gh last asked about any PR
	Synced    time.Time // repos last fetched
	GHFailing bool      // PR polls are failing
	// ThreadPolled is when each thread's PR was last checked, by id.
	ThreadPolled map[string]time.Time
}

// HostFails reads, from the ticker's state file at path, the projects
// whose PR polls fail: how many failed in a row, by slug (Timing's
// GHFailing for each). One read for all projects, as the sidebars poll.
func HostFails(path string) map[string]int {
	out := map[string]int{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var st state
	if json.Unmarshal(b, &st) != nil {
		return out
	}
	for slug, pm := range st.Projects {
		if pm != nil && pm.GHFails > 0 {
			out[slug] = pm.GHFails
		}
	}
	return out
}

// ReadTiming reads project slug's Timing from the ticker's state file at path;
// zero when there is none.
func ReadTiming(path, slug string) Timing {
	_, t := ReadProject(path, slug)
	return t
}

// ReadProject is PRs and ReadTiming from one read of the state file.
func ReadProject(path, slug string) (map[string]PR, Timing) {
	prs, t := map[string]PR{}, Timing{ThreadPolled: map[string]time.Time{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return prs, t
	}
	var st state
	if json.Unmarshal(b, &st) != nil {
		return prs, t
	}
	if pm := st.Projects[slug]; pm != nil {
		t.PRPolled, t.Synced, t.GHFailing = pm.PRPolled, pm.Synced, pm.GHFails > 0
	}
	for k, m := range st.Threads {
		id, ok := strings.CutPrefix(k, slug+"/")
		if !ok || m == nil || strings.Contains(id, "/") {
			continue
		}
		if m.PR.Number > 0 {
			prs[id] = m.PR
		}
		if !m.PRPolled.IsZero() {
			t.ThreadPolled[id] = m.PRPolled
		}
	}
	return prs, t
}
