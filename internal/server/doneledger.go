package server

// Done as a ledger (docs/SPEC.md §5.1; design board-library, B4 and B12),
// what task.done and $.tasks.done share: the done tasks of one repo or all,
// Done's headings with the release lines among them, and what a filter
// offers. Each repo's versions are its own, so its releases are counted on
// their own and never summed; a task's count follows the filter.

import (
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/history"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// doneRepo is one entry of a filter: the repo's name in the answer, how
// many done tasks it has and how many releases.
type doneRepo struct {
	Repo     string
	Done     int
	Releases int
}

// doneLedger is the done tasks a read asks about.
type doneLedger struct {
	// all are the tasks read, done those the filter lets through; repo
	// the filter, "" for all.
	all, done []*tasks.Task
	repo      string
	name      func(*tasks.Task) string
	// several says the project has several repos, so rows and lines name
	// theirs while no filter says which.
	several bool
	// released are the project's releases, once read.
	released []proto.HistoryRepo
	read     bool
}

// releases are p's releases: read once, as git runs for them.
func (l *doneLedger) releases(p *project.Project, loc *time.Location) []proto.HistoryRepo {
	if !l.read {
		l.released, l.read = history.ReleasesIn(p, loc), true
	}
	return l.released
}

// readDoneLedger reads p's done tasks, the archive's too with archive,
// narrowed to repo unless that is "".
func readDoneLedger(p *project.Project, archive bool, repo string) (*doneLedger, error) {
	all, err := p.Tasks().Done(archive)
	if err != nil {
		return nil, err
	}
	led := &doneLedger{all: all, done: all, repo: repo, name: tasks.RepoNamer(p.Repos), several: len(p.Repos) > 1}
	if repo != "" {
		led.done = slices.DeleteFunc(slices.Clone(all), func(t *tasks.Task) bool { return led.name(t) != repo })
	}
	return led, nil
}

// naming says whether rows and lines name their repo: a view of one repo
// needs no names.
func (l *doneLedger) naming() bool { return l.several && l.repo == "" }

// rowRepo is what a row says of its repo, "" when it needn't.
func (l *doneLedger) rowRepo(t *tasks.Task) string {
	if !l.naming() {
		return ""
	}
	return l.name(t)
}

// groups are Done's headings as of now, in now's time zone, opening the one
// that holds task reveal, with the release lines among them. Git is read
// for the tags only, and not at all with no repo; git failing gives no
// lines.
func (l *doneLedger) groups(p *project.Project, now time.Time, reveal int) []tasks.DoneGroup {
	out := tasks.GroupDone(l.done, now, reveal)
	tasks.PlaceReleases(out, l.done, l.lines(l.releases(p, now.Location())), now)
	return out
}

// lines are the release lines of repos the filter lets through, the newest
// proto.DoneReleaseLines.
func (l *doneLedger) lines(repos []proto.HistoryRepo) []tasks.ReleaseLine {
	var out []tasks.ReleaseLine
	for _, r := range repos {
		if l.repo != "" && r.Repo != l.repo {
			continue
		}
		for _, x := range r.Releases {
			at, err := time.Parse(time.RFC3339, x.At)
			if err != nil {
				continue
			}
			line := tasks.ReleaseLine{Part: x.Part, Version: x.Version, Tag: x.Tag, At: at}
			if l.naming() {
				line.Repo = r.Repo
			}
			out = append(out, line)
		}
	}
	slices.SortStableFunc(out, func(a, b tasks.ReleaseLine) int { return b.At.Compare(a.At) })
	return out[:min(len(out), proto.DoneReleaseLines)]
}

// repoCounts is what a filter offers, with several repos: the project's in
// its order, then a repo tasks name that it no longer has, then none, while
// a task has it. With one repo or none, nothing: there is nothing to narrow.
func (l *doneLedger) repoCounts(p *project.Project, loc *time.Location) []doneRepo {
	if !l.several {
		return nil
	}
	done := map[string]int{}
	for _, t := range l.all {
		done[l.name(t)]++
	}
	released := map[string]int{}
	for _, r := range l.releases(p, loc) {
		released[r.Repo] = r.Count
	}
	var out []doneRepo
	listed := map[string]bool{tasks.NoRepo: true}
	for _, name := range tasks.RepoLabels(p.Repos) {
		out = append(out, doneRepo{Repo: name, Done: done[name], Releases: released[name]})
		listed[name] = true
	}
	var others []string
	for name := range done {
		if !listed[name] {
			others = append(others, name)
		}
	}
	slices.Sort(others)
	for _, name := range others {
		out = append(out, doneRepo{Repo: name, Done: done[name]})
	}
	if n := done[tasks.NoRepo]; n > 0 {
		out = append(out, doneRepo{Repo: tasks.NoRepo, Done: n})
	}
	return out
}
