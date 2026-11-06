// Package history keeps a project's daily numbers (docs/SPEC.md §5.1,
// History): done per day and the facts, from tm's own records only, the
// task list, its archive and the journal, so a plain folder and a repo
// with several remotes answer alike. Git only adds each repo's releases
// (internal/releases), read locally, and nothing else.
//
// The journal is read once: JOURNAL.md is followed from its last offset
// and each month archive is read again only when its size or time
// changed, which is when the ticker moved lines to it. So a read costs
// little however long the project has run. The task list is read on
// every read, which is cheap; consoles read when the done count changes.
package history

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/releases"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// indexes are the indexes this process keeps, by project folder: the
// server's, or one read's in a tm with no server to ask.
var (
	indexesMu sync.Mutex
	indexes   = map[string]*index{}
)

// Read answers project p's numbers and releases, its days in this
// machine's time zone.
func Read(p *project.Project) (proto.History, error) {
	return ReadIn(p, time.Local)
}

// ReadIn is Read with days in loc.
func ReadIn(p *project.Project, loc *time.Location) (proto.History, error) {
	x := indexOf(p)
	h, err := x.read(p, loc)
	if err != nil {
		return h, err
	}
	h.Releases = x.releases(p, h.FirstRecord, loc)
	h.Pulse = PulseOf(h, len(p.Repos) > 1, time.Now().In(loc))
	return h, nil
}

// ReleasesIn are the releases Read answers with, without its numbers.
// Numbers that can't be read are no reason to fail what asks: no releases.
func ReleasesIn(p *project.Project, loc *time.Location) []proto.HistoryRepo {
	x := indexOf(p)
	h, err := x.read(p, loc)
	if err != nil {
		return nil
	}
	return x.releases(p, h.FirstRecord, loc)
}

// Records is Read without the releases: tm's records alone, no git run.
// A paired server gets these.
func Records(p *project.Project) (proto.History, error) {
	return indexOf(p).read(p, time.Local)
}

func indexOf(p *project.Project) *index {
	indexesMu.Lock()
	x := indexes[p.Dir]
	if x == nil {
		x = &index{archives: map[string]*archive{}}
		indexes[p.Dir] = x
	}
	indexesMu.Unlock()
	return x
}

// releases are the releases of p's repos that have any (releases.Count),
// each repo's on its own. No repo, or release_tags = "none", runs no git;
// settings that can't be read are no reason to fail the numbers.
func (x *index) releases(p *project.Project, first string, loc *time.Location) []proto.HistoryRepo {
	var out []proto.HistoryRepo
	for _, r := range x.repoTags(p, first, loc) {
		out = append(out, repoReleases(r.name, r.releases))
	}
	return out
}

// taggedRepo is one repo's name and the releases it has.
type taggedRepo struct {
	name     string
	releases []releases.Release
}

// repoTags are the repos of p with a release, in its list's order.
func (x *index) repoTags(p *project.Project, first string, loc *time.Location) []taggedRepo {
	if len(p.Repos) == 0 || first == "" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	safety, err := cfg.Safety(p.Slug)
	if err != nil || config.ReleaseTagsAreOff(safety.ReleaseTags) {
		return nil
	}
	names := tasks.RepoLabels(p.Repos)
	var out []taggedRepo
	for i, repo := range p.Repos {
		rs := releases.Count(x.tags.Tags(repo), safety.ReleaseTags, first, loc)
		if len(rs) > 0 {
			out = append(out, taggedRepo{names[i], rs})
		}
	}
	return out
}

// repoReleases is a repo's releases, the newest proto.HistoryReleases of
// them, and how many it has of each part when any names a part.
func repoReleases(name string, rs []releases.Release) proto.HistoryRepo {
	h := proto.HistoryRepo{Repo: name, Count: len(rs)}
	parts := map[string]int{}
	for _, r := range rs {
		parts[r.Part]++
	}
	if len(parts) > 1 || parts[""] == 0 {
		for _, part := range slices.Sorted(maps.Keys(parts)) {
			h.Parts = append(h.Parts, proto.HistoryPart{Part: textsafe.Line(part, maxTagWords), Count: parts[part]})
		}
	}
	rs = rs[max(len(rs)-proto.HistoryReleases, 0):]
	h.Releases = make([]proto.HistoryRelease, len(rs))
	for i, r := range rs {
		h.Releases[i] = proto.HistoryRelease{Tag: textsafe.Line(r.Tag, maxTagWords), Part: textsafe.Line(r.Part, maxTagWords),
			Version: textsafe.Line(r.Version, maxTagWords), At: r.At.UTC().Format(time.RFC3339)}
	}
	return h
}

// maxTagWords cuts a tag's name for a console.
const maxTagWords = 200

// archive is what one month archive said, and which version of it.
type archive struct {
	size  int64
	mod   time.Time
	lines records
}

// records are the lines of the journal the numbers use.
type records struct {
	// status are the lines that move a task, in the file's order.
	status []project.StatusLine
	// first is the time of the oldest line of any kind.
	first time.Time
}

func (r *records) add(line string) {
	ts, _, _ := strings.Cut(line, " ")
	at, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return // the heading, or a line a hand wrote
	}
	if r.first.IsZero() || at.Before(r.first) {
		r.first = at
	}
	if s, ok := project.ParseStatusLine(line); ok {
		r.status = append(r.status, s)
	}
}

// index is one project's journal in memory.
type index struct {
	mu       sync.Mutex
	archives map[string]*archive
	// The journal: the file last read, how far, and what it said.
	journal fs.FileInfo
	offset  int64
	current records
	// tags are the repos' tags, cached by their refs.
	tags releases.Cache
}

func (x *index) read(p *project.Project, loc *time.Location) (proto.History, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	all, sources, first, err := x.load(p)
	if err != nil {
		return proto.History{}, err
	}
	return numbers(all, runsOf(sources), first, loc), nil
}

// load reads the task list and whatever the journal added: all the tasks,
// the journal's lines oldest file first, and its oldest time. The lines
// are the index's, good while x.mu is held.
func (x *index) load(p *project.Project) ([]*tasks.Task, []*records, time.Time, error) {
	// The journal before the archives: the ticker writes a line to its
	// archive first, so a move meanwhile shows a line twice, which the
	// numbers don't mind, and never not at all.
	if err := x.follow(p.Path("JOURNAL.md")); err != nil {
		return nil, nil, time.Time{}, err
	}
	names, err := x.readArchives(p.Path("journal"))
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	s := p.Tasks()
	board, err := s.Load()
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	archived, err := s.LoadArchive()
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	first := x.current.first
	sources := make([]*records, 0, len(names)+1)
	for _, name := range names {
		r := &x.archives[name].lines
		sources = append(sources, r)
		if !r.first.IsZero() && (first.IsZero() || r.first.Before(first)) {
			first = r.first
		}
	}
	sources = append(sources, &x.current)
	return append(board.Tasks, archived.Tasks...), sources, first, nil
}

// follow reads what was added to the journal since the last read. The
// ticker replaces the file when it moves old lines out, and so does a
// hand: another file, or a shorter one, is read from its start.
func (x *index) follow(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		x.journal, x.offset, x.current = nil, 0, records{}
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if x.journal == nil || !os.SameFile(x.journal, fi) || fi.Size() < x.offset {
		x.offset, x.current = 0, records{}
	}
	x.journal = fi
	if fi.Size() == x.offset {
		return nil
	}
	if _, err := f.Seek(x.offset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	// A line still being written waits for the next read.
	end := strings.LastIndexByte(string(data), '\n') + 1
	for line := range strings.SplitSeq(string(data[:end]), "\n") {
		if line != "" {
			x.current.add(line)
		}
	}
	x.offset += int64(end)
	return nil
}

// readArchives reads the month archives that are new or changed, drops
// those gone, and returns the names in month order. An archive that
// doesn't read whole, one being appended to or damaged, gives what it
// holds and is read again next time.
func (x *index) readArchives(dir string) ([]string, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.md.gz"))
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	for name := range x.archives {
		if !slices.Contains(names, name) {
			delete(x.archives, name)
		}
	}
	kept := names[:0]
	for _, name := range names {
		fi, err := os.Stat(name)
		if err != nil {
			delete(x.archives, name)
			continue
		}
		kept = append(kept, name)
		a := x.archives[name]
		if a != nil && a.size == fi.Size() && a.mod.Equal(fi.ModTime()) {
			continue
		}
		a = &archive{size: fi.Size(), mod: fi.ModTime()}
		if err := project.ReadJournalArchive(name, a.lines.add); err != nil {
			a.size = -1
		}
		x.archives[name] = a
	}
	return kept, nil
}

// run is one task's latest way to done, from the journal: its first
// start and when it became done. A task that left done starts over, so
// a task reopened a month later times its last run.
type run struct{ start, done time.Time }

func runsOf(sources []*records) map[int]*run {
	runs := map[int]*run{}
	for _, src := range sources {
		for _, l := range src.status {
			r := runs[l.Task]
			if r == nil {
				r = &run{}
				runs[l.Task] = r
			}
			switch {
			case l.Status == string(tasks.Done):
				if r.done.IsZero() {
					r.done = l.At
				}
				continue
			case !r.done.IsZero():
				*r = run{}
			}
			if l.Status == string(tasks.Started) && r.start.IsZero() {
				r.start = l.At
			}
		}
	}
	return runs
}

// numbers are the numbers of all a project's tasks, with their runs and
// the journal's oldest time.
func numbers(all []*tasks.Task, runs map[int]*run, first time.Time, loc *time.Location) proto.History {
	out := proto.History{Days: []proto.HistoryDay{}}
	if !first.IsZero() {
		out.FirstRecord = first.In(loc).Format(time.DateOnly)
	}
	done := doneTimes(runs)
	perDay := map[string]int{}
	var took []int64
	seen := map[int]bool{}
	for _, t := range all {
		if seen[t.ID] {
			continue // moved to the archive between the two reads
		}
		seen[t.ID] = true
		if len(t.Created) >= len(time.DateOnly) && (out.FirstRecord == "" || t.Created[:len(time.DateOnly)] < out.FirstRecord) {
			out.FirstRecord = t.Created[:len(time.DateOnly)]
		}
		switch t.Status {
		case tasks.Done:
			out.Done++
		case tasks.Dropped:
			out.Dropped++
			continue
		default:
			out.Open++
			continue
		}
		at := tasks.DoneValue(t, done)
		if day := dayOf(at, loc); day != "" {
			perDay[day]++
		}
		if r := runs[t.ID]; r != nil && !r.start.IsZero() {
			end, err := time.Parse(time.RFC3339, at)
			if err != nil {
				end = r.done // only a day on the task, a time in the journal
			}
			if !end.IsZero() && !end.Before(r.start) {
				took = append(took, int64(end.Sub(r.start)/time.Second))
			}
		}
	}
	for day, n := range perDay {
		out.Days = append(out.Days, proto.HistoryDay{Day: day, Done: n})
	}
	slices.SortFunc(out.Days, func(a, b proto.HistoryDay) int { return cmp.Compare(a.Day, b.Day) })
	for i := range out.Days {
		if out.BestDay == nil || out.Days[i].Done > out.BestDay.Done {
			out.BestDay = &out.Days[i]
		}
	}
	if out.BestDay != nil {
		best := *out.BestDay
		out.BestDay = &best
	}
	out.Timed = len(took)
	if out.Timed >= proto.TypicalFrom {
		slices.Sort(took)
		out.TypicalSeconds = median(took)
		out.Typical = Words(out.TypicalSeconds)
	}
	return out
}

// doneTimes are the runs' finish times, as tasks.DoneValue takes them.
func doneTimes(runs map[int]*run) map[int]string {
	done := map[int]string{}
	for id, r := range runs {
		if !r.done.IsZero() {
			done[id] = r.done.UTC().Format(time.RFC3339)
		}
	}
	return done
}

// dayOf is the day a done task's time or bare day falls on in loc, ""
// when it is neither.
func dayOf(at string, loc *time.Location) string {
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.In(loc).Format(time.DateOnly)
	}
	if _, err := time.Parse(time.DateOnly, at); err == nil {
		return at
	}
	return ""
}

// median of sorted, which isn't empty; of two middle values, their mean.
func median(sorted []int64) int64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// Words says a time in minutes, hours or days (design board-library,
// B16), rounded: minutes up to 90 of them, hours up to 48.
func Words(seconds int64) string {
	unit := func(n float64, name string) string {
		v := int64(math.Round(n))
		if v == 1 {
			return "1 " + name
		}
		return fmt.Sprintf("%d %ss", v, name)
	}
	switch s := float64(seconds); {
	case s < 90*60:
		return unit(max(s/60, 1), "minute")
	case s < 48*3600:
		return unit(s/3600, "hour")
	default:
		return unit(s/86400, "day")
	}
}
