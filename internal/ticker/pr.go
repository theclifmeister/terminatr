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

// State is the ticker's state file as read once, for the callers that
// want several things of it (PRs, Timing, Checkouts, HostFails) without
// parsing it for each. A missing or unreadable file is the zero State,
// which has none of anything: the ticker hasn't polled yet.
type State struct{ st state }

// Load reads the ticker's state file at path.
func Load(path string) State {
	var s State
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if json.Unmarshal(b, &s.st) != nil {
		return State{}
	}
	return s
}

// Summaries is PRs as Summary lines, for project.Context.
func Summaries(path, slug string) map[string]string {
	return Load(path).Summaries(slug)
}

// Summaries is PRs(slug) as Summary lines.
func (s State) Summaries(slug string) map[string]string {
	out := map[string]string{}
	for id, pr := range s.PRs(slug) {
		out[id] = pr.Summary()
	}
	return out
}

// PRs reads the PR fields the ticker keeps in its state file at path for
// the threads of project slug, by thread id. A missing or unreadable
// file gives none: the ticker hasn't polled yet.
func PRs(path, slug string) map[string]PR {
	return Load(path).PRs(slug)
}

// PRs is the PR fields kept for the threads of project slug, by thread
// id.
func (s State) PRs(slug string) map[string]PR {
	prs, _ := s.Project(slug)
	return prs
}

// Timing is when the ticker last did its slow work for a project.
type Timing struct {
	PRPolled  time.Time // gh last asked about any PR
	Synced    time.Time // repos last fetched
	GHFailing bool      // PR polls are failing
	GHFails   int       // how many in a row
	// ThreadPolled is when each thread's PR was last checked, by id.
	ThreadPolled map[string]time.Time
}

// HostFails reads, from the ticker's state file at path, the projects
// whose PR polls fail: how many failed in a row, by slug (Timing's
// GHFailing for each). One read for all projects, as the sidebars poll.
func HostFails(path string) map[string]int {
	return Load(path).HostFails()
}

// HostFails is how many PR polls failed in a row, by project slug, for
// the projects whose polls fail.
func (s State) HostFails() map[string]int {
	out := map[string]int{}
	for slug, pm := range s.st.Projects {
		if pm != nil && pm.GHFails > 0 {
			out[slug] = pm.GHFails
		}
	}
	return out
}

// ReadTiming reads project slug's Timing from the ticker's state file at path;
// zero when there is none.
func ReadTiming(path, slug string) Timing {
	_, t := Load(path).Project(slug)
	return t
}

// ReadProject is PRs and ReadTiming from one read of the state file.
func ReadProject(path, slug string) (map[string]PR, Timing) {
	return Load(path).Project(slug)
}

// Project is project slug's PRs and Timing.
func (s State) Project(slug string) (map[string]PR, Timing) {
	prs, t := map[string]PR{}, Timing{ThreadPolled: map[string]time.Time{}}
	st := s.st
	if pm := st.Projects[slug]; pm != nil {
		t.PRPolled, t.Synced, t.GHFailing, t.GHFails = pm.PRPolled, pm.Synced, pm.GHFails > 0, pm.GHFails
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
