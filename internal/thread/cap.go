package thread

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The parallel threads cap (docs/SPEC.md §9, §11.2): tm thread start
// refuses a new thread while Working counts the project's cap or more,
// or WorkingAll the cap across projects (global_parallel_threads) or
// more.

// DoneSince is when the thread called tm done, if it did so after its
// last prompt; zero otherwise (a new prompt gives it work again).
func (r *Record) DoneSince() time.Time {
	if !r.Done || r.DoneAt.Before(r.LastPrompt) {
		return time.Time{}
	}
	return r.DoneAt
}

// AgentState is the live agent state of a thread's session in sessions
// ("working", "blocked", "idle", "unknown", …), and whether it runs.
func AgentState(r *Record, sessions []proto.SessionInfo) (state string, live bool) {
	if r.Session == "" {
		return "", false
	}
	for _, s := range sessions {
		if s.ID == r.Session && s.Thread == r.ID {
			if s.State == "" {
				return "unknown", true
			}
			return s.State, true
		}
	}
	return "", false
}

// IsWorking reports whether a thread counts toward the cap: it is open
// (not stopped or resolved), its session runs, its agent isn't idle
// (working, blocked and unknown all count), and it hasn't called tm done
// since its last prompt.
func IsWorking(r *Record, sessions []proto.SessionInfo) bool {
	if r.State != Running || !r.DoneSince().IsZero() {
		return false
	}
	st, live := AgentState(r, sessions)
	return live && st != "idle" && st != "exited" && st != "stopped"
}

// Working counts the threads of recs that count toward the cap.
func Working(recs []*Record, sessions []proto.SessionInfo) int {
	n := 0
	for _, r := range recs {
		if IsWorking(r, sessions) {
			n++
		}
	}
	return n
}

// WorkingAll counts, by project slug, the threads of every project that
// count toward the cap across projects: only projects with a thread
// session in sessions can have one, so only theirs are read. A project
// that can't be read counts none.
func WorkingAll(sessions []proto.SessionInfo) map[string]int {
	out := map[string]int{}
	for _, s := range sessions {
		if s.Thread == "" || s.Project == "" {
			continue
		}
		if _, ok := out[s.Project]; ok {
			continue
		}
		out[s.Project] = 0
		p, err := project.Open(s.Project)
		if err != nil {
			continue
		}
		if recs, err := List(p); err == nil {
			out[s.Project] = Working(recs, sessions)
		}
	}
	maps.DeleteFunc(out, func(_ string, n int) bool { return n == 0 })
	return out
}

// Total is the sum of WorkingAll's counts.
func Total(by map[string]int) int {
	n := 0
	for _, c := range by {
		n += c
	}
	return n
}

// ByProjectWords says WorkingAll's counts, largest first, e.g. "3 in
// web, 1 in api"; "none" when there are none.
func ByProjectWords(by map[string]int) string {
	slugs := slices.Sorted(maps.Keys(by))
	slices.SortStableFunc(slugs, func(a, b string) int { return by[b] - by[a] })
	var parts []string
	for _, s := range slugs {
		parts = append(parts, fmt.Sprintf("%d in %s", by[s], s))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
