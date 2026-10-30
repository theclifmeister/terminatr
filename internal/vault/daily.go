package vault

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// The daily notes (T719, docs/SPEC.md §5.1, Vault): _tm/daily/<date>.md
// at the vault's root, one per local day across every project, the
// digest of that day's journal lines: what shipped, started and was
// decided, what needed the user at the day's end, the links, the guard's
// denials and a line of counts. JOURNAL.md stays the raw log. A journal
// line's detail is often an agent's words, so a note shows tm's own
// words and ids only, and the titles from the board.

// DailyDir is where the daily notes are, from the vault's root. It is
// tm's own folder, never the Daily notes plugin's: a human's template
// embeds tm's day with ![[_tm/daily/{{date}}]].
const DailyDir = "_tm/daily"

// DailyNote is the path of the day date's note ("2026-10-24") from the
// vault's root.
func DailyNote(date string) string { return DailyDir + "/" + date + ".md" }

// TickerDays are the days the ticker renders: today, and yesterday, which
// settles with its first render after midnight.
const TickerDays = 2

// MaxDays is the most days tm vault render --days goes back.
const MaxDays = 3660

// entry is one journal line: "<time> <who> <action> <ref> <detail>".
type entry struct {
	at                       time.Time
	who, action, ref, detail string
}

func parseEntry(l string) (entry, bool) {
	f := strings.SplitN(l, " ", 5)
	if len(f) < 3 {
		return entry{}, false
	}
	at, err := time.Parse(time.RFC3339, f[0])
	if err != nil {
		return entry{}, false
	}
	e := entry{at: at, who: f[1], action: f[2]}
	if len(f) > 3 {
		e.ref = f[3]
	}
	if len(f) > 4 {
		e.detail = f[4]
	}
	return e, true
}

// maxJournal caps what one journal file gives, unpacked.
const maxJournal = 256 << 20

// readJournal is p's journal, oldest first: the archives
// (journal/<yyyy-mm>.md.gz) and JOURNAL.md. A line the archive repeats
// (a crash between archiving and rewriting JOURNAL.md) counts once.
func readJournal(p *project.Project) ([]entry, error) {
	var paths []string
	ents, err := os.ReadDir(p.Path("journal"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range ents {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".md.gz") {
			paths = append(paths, p.Path("journal", e.Name()))
		}
	}
	sort.Strings(paths)
	paths = append(paths, p.Path("JOURNAL.md"))
	var out []entry
	seen := map[string]bool{}
	for _, path := range paths {
		f, err := fsx.OpenNoLink(path, os.O_RDONLY, 0)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var r io.Reader = f
		if strings.HasSuffix(path, ".gz") {
			zr, err := gzip.NewReader(f)
			if err != nil {
				f.Close()
				return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
			}
			r = zr
		}
		sc := bufio.NewScanner(io.LimitReader(r, maxJournal))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			l := sc.Text()
			if e, ok := parseEntry(l); ok && !seen[l] {
				seen[l] = true
				out = append(out, e)
			}
		}
		err = sc.Err()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out, nil
}

// The ids a note takes from a journal line; anything else in it is
// someone's words.
var (
	questionRE = regexp.MustCompile(`^Q[1-9][0-9]{0,8}$`)
	memoryRE   = regexp.MustCompile(`^memory/[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.md$`)
	linkIDRE   = regexp.MustCompile(`^[MRX]-[0-9A-Za-z][0-9A-Za-z-]{0,39}$`)
	mergedRE   = regexp.MustCompile(`^PR #([1-9][0-9]{0,8}) merged`)
	ruleRE     = regexp.MustCompile(`^[a-z][a-z-]{0,39}$`)
	statusRE   = regexp.MustCompile(`^[a-z]+`)
)

// first is the first word of s.
func first(s string) string {
	w, _, _ := strings.Cut(s, " ")
	return w
}

// shipped is a task done, or a PR merged, on the day.
type shipped struct {
	task, thread string
	pr           int
}

// linkEvent is a link or peer line as fixed words: verb, the message,
// request or file id, the other project, or the paired server (peer).
type linkEvent struct{ verb, id, other, peer string }

// projectDay is what a project's journal says of one day.
type projectDay struct {
	lines    int
	shipped  []shipped
	started  [][2]string // task, thread
	added    []string
	answered []string
	// answeredFor are the tasks the questions answered name.
	answeredFor []string
	memory      []string
	// memoryGone are the memory notes removed, which links can't reach.
	memoryGone []string
	approved   [][2]string // task, status
	links      []linkEvent
	linkMore   map[string]int
	guard      map[string]int
	guardBy    []string
	rules      []string
	counts     map[string]int
	// What needed the user at the day's end.
	review   [][2]string // task, status
	asks     []string
	held     [][2]string // message, other
	requests [][2]string // request or file, other
}

// replay is a project's state as its journal builds it.
type replay struct {
	status  map[string]string // task → status
	thread  map[string]string // task → thread
	taskOf  map[string]string // thread → task
	pr      map[string]int    // thread → PR merged
	asks    map[string]bool   // open questions
	qTask   map[string]string // question → the task it names
	held    map[string]string // held message → other
	waiting map[string]string // request or file offered here → other
}

func newReplay() *replay {
	return &replay{
		status: map[string]string{}, thread: map[string]string{}, taskOf: map[string]string{},
		pr: map[string]int{}, asks: map[string]bool{}, qTask: map[string]string{}, held: map[string]string{}, waiting: map[string]string{},
	}
}

// open is what needs the user now, for a day's end.
func (r *replay) open(d *projectDay) {
	for _, t := range slices.Sorted(mapKeys(r.status)) {
		if st := r.status[t]; st == string(tasks.Review) || st == string(tasks.Blocked) {
			d.review = append(d.review, [2]string{t, st})
		}
	}
	sortRefs(d.review)
	d.asks = slices.Sorted(mapKeys(r.asks))
	sort.Slice(d.asks, func(i, j int) bool { return refNum(d.asks[i]) < refNum(d.asks[j]) })
	for _, id := range slices.Sorted(mapKeys(r.held)) {
		d.held = append(d.held, [2]string{id, r.held[id]})
	}
	for _, id := range slices.Sorted(mapKeys(r.waiting)) {
		d.requests = append(d.requests, [2]string{id, r.waiting[id]})
	}
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// refNum is the number in an id such as T12 or Q3.
func refNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(id, "TQ"))
	return n
}

func sortRefs(s [][2]string) {
	sort.SliceStable(s, func(i, j int) bool { return refNum(s[i][0]) < refNum(s[j][0]) })
}

func appendOnce(s []string, v string) []string {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}

// linkVerbs are the link and peer actions a note lists, as words; the
// rest of link.* are counted (linkMore).
var linkVerbs = map[string]string{
	"link.send": "to", "link.receive": "from", "link.held": "held for",
	"link.approve": "approved for", "link.drop": "dropped, for",
	"link.request": "asked of", "link.requested": "asked by",
	"link.accept": "accepted from", "link.decline": "declined from",
	"link.accepted": "accepted by", "link.declined": "declined by",
	"link.file-offer": "offered to", "link.file-offered": "offered by",
	"link.file-received": "received from",
	"link.pause":         "paused with", "link.resume": "resumed with",
}

// apply takes e into r and, when d isn't nil, into the day d.
func (r *replay) apply(e entry, d *projectDay) {
	if d != nil {
		d.lines++
	}
	task := ""
	if tasks.ValidRef(e.ref) {
		task = e.ref
	}
	thr := ""
	if thread.ValidID(e.ref) {
		thr = e.ref
	}
	other, id := "", first(e.detail)
	if config.ValidLinkKey(e.ref) {
		other = e.ref
	}
	if !linkIDRE.MatchString(id) {
		id = ""
	}
	counted := ""
	switch a := e.action; {
	case a == "task.add" && task != "":
		r.status[task] = string(tasks.Open)
		if d != nil {
			d.added = appendOnce(d.added, task)
		}
	case a == "task.status" && task != "":
		st := statusRE.FindString(e.detail)
		r.status[task] = st
		if d == nil {
			break
		}
		if st == string(tasks.Done) {
			d.ship(shipped{task: task, thread: r.thread[task], pr: r.pr[r.thread[task]]})
		} else if strings.HasSuffix(e.detail, "(approved by the user)") && st != "" {
			d.approved = append(d.approved, [2]string{task, st})
		}
	case a == "task.done" && task != "":
		r.status[task] = string(tasks.Done)
		if d != nil {
			d.ship(shipped{task: task, thread: r.thread[task], pr: r.pr[r.thread[task]]})
		}
	case a == "task.archive" && task != "":
		delete(r.status, task)
	case a == "task.thread" && task != "" && thread.ValidID(first(e.detail)):
		r.thread[task], r.taskOf[first(e.detail)] = first(e.detail), task
	case a == "thread.start" && thr != "":
		t := first(e.detail)
		if tasks.ValidRef(t) {
			r.thread[t], r.taskOf[thr] = thr, t
		} else {
			t = ""
		}
		if d != nil {
			d.started = append(d.started, [2]string{t, thr})
		}
	case a == "thread.merge" && thr != "":
		m := mergedRE.FindStringSubmatch(e.detail)
		if m == nil {
			break
		}
		r.pr[thr], _ = strconv.Atoi(m[1])
		if d != nil {
			d.ship(shipped{task: r.taskOf[thr], thread: thr, pr: r.pr[thr]})
		}
	case a == "ask.add" && questionRE.MatchString(e.ref):
		r.asks[e.ref] = true
		if t := strings.TrimSuffix(first(e.detail), ":"); tasks.ValidRef(t) {
			r.qTask[e.ref] = t
		}
	case a == "ask.done" && questionRE.MatchString(e.ref):
		delete(r.asks, e.ref)
		if d != nil {
			d.answered = appendOnce(d.answered, e.ref)
			if t := r.qTask[e.ref]; t != "" {
				d.answeredFor = appendOnce(d.answeredFor, t)
			}
		}
	case (a == "memory.edit" || a == "memory.new") && memoryRE.MatchString(e.ref):
		if d != nil {
			d.memory = appendOnce(d.memory, e.ref)
			d.memoryGone = slices.DeleteFunc(d.memoryGone, func(m string) bool { return m == e.ref })
		}
	case a == "memory.rm" && memoryRE.MatchString(e.ref):
		if d != nil {
			d.memoryGone = appendOnce(d.memoryGone, e.ref)
			d.memory = slices.DeleteFunc(d.memory, func(m string) bool { return m == e.ref })
		}
	case a == "guard.deny":
		if d == nil {
			break
		}
		by := "coordinator"
		switch {
		case thr != "":
			by = thr
		case e.who != "coordinator":
			by = "other"
		}
		if d.guard[by] == 0 {
			d.guardBy = append(d.guardBy, by)
		}
		d.guard[by]++
		if rule := first(e.detail); ruleRE.MatchString(rule) {
			d.rules = appendOnce(d.rules, rule)
		}
	case strings.HasPrefix(a, "link.") && other != "":
		switch a {
		case "link.held":
			if id != "" {
				r.held[id] = other
			}
		case "link.approve", "link.drop":
			delete(r.held, id)
		case "link.requested", "link.file-offered":
			if id != "" {
				r.waiting[id] = other
			}
		case "link.accept", "link.decline", "link.file-accept", "link.file-decline", "link.file-received", "link.file-failed", "link.request-expired":
			delete(r.waiting, id)
		}
		if d == nil {
			break
		}
		if verb, ok := linkVerbs[a]; ok && (id != "" || a == "link.pause" || a == "link.resume") {
			d.links = append(d.links, linkEvent{verb: verb, id: id, other: other})
		} else {
			d.linkMore[strings.TrimPrefix(a, "link.")]++
		}
	case strings.HasPrefix(a, "peer.share-") && config.ValidLinkKey(e.ref) && !strings.Contains(e.ref, "/"):
		if d != nil {
			d.links = append(d.links, linkEvent{verb: "share " + strings.TrimPrefix(a, "peer.share-"), peer: e.ref})
		}
	case strings.HasPrefix(a, "task.steps."):
		counted = "step change"
	case a == "run.start":
		counted = "run"
	case a == "run.end":
	case strings.HasPrefix(a, "thread.prompt"):
		counted = "prompt"
	case a == "repo.fast-forward":
		counted = "fast-forward"
	case a == "thread.report":
		counted = "report"
	default:
		counted = "other"
	}
	if d != nil && counted != "" {
		d.counts[counted]++
	}
}

// ship adds s to the day once: a task done after its thread's merge, or
// a merge after its task, is one entry.
func (d *projectDay) ship(s shipped) {
	for i, o := range d.shipped {
		if s.task != "" && o.task == s.task || s.thread != "" && o.thread == s.thread && (o.task == "" || s.task == "" || o.task == s.task) {
			if o.task == "" {
				o.task = s.task
			}
			if o.thread == "" {
				o.thread = s.thread
			}
			if o.pr == 0 {
				o.pr = s.pr
			}
			d.shipped[i] = o
			return
		}
	}
	d.shipped = append(d.shipped, s)
}

// projectDays replays journal and returns what it says of each of days
// (local dates, oldest first, in loc), and what needs the user now.
func projectDays(journal []entry, days []string, loc *time.Location) (map[string]*projectDay, *projectDay) {
	r := newReplay()
	out := map[string]*projectDay{}
	want := map[string]bool{}
	for _, d := range days {
		want[d] = true
	}
	var cur *projectDay
	curDate := ""
	settle := func() {
		if cur != nil {
			r.open(cur)
		}
	}
	for _, e := range journal {
		date := e.at.In(loc).Format(time.DateOnly)
		if date != curDate {
			settle()
			curDate, cur = date, nil
			if want[date] {
				cur = &projectDay{linkMore: map[string]int{}, guard: map[string]int{}, counts: map[string]int{}}
				out[date] = cur
			}
		}
		r.apply(e, cur)
	}
	settle()
	now := &projectDay{}
	r.open(now)
	return out, now
}

// dayFront is a daily note's properties.
type dayFront struct {
	Date       string   `toml:"date" yaml:"date"`
	Projects   []string `toml:"projects" yaml:"projects"`
	Shipped    int      `toml:"shipped" yaml:"shipped"`
	Started    int      `toml:"started" yaml:"started"`
	NeedsYou   int      `toml:"needs_you" yaml:"needs_you"`
	Tags       []string `toml:"tags" yaml:"tags"`
	TM         string   `toml:"tm" yaml:"tm"`
	CSSClasses []string `toml:"cssclasses" yaml:"cssclasses"`
}

// dayProject is a project as a daily note shows it: its slug, its
// board's titles and its journal.
type dayProject struct {
	slug    string
	titles  map[string]string // task → title
	journal []entry
}

func loadDayProject(p *project.Project) (*dayProject, error) {
	j, err := readJournal(p)
	if err != nil {
		return nil, err
	}
	dp := &dayProject{slug: p.Slug, titles: map[string]string{}, journal: j}
	s := p.Tasks()
	board, err := s.Load()
	if err != nil {
		return nil, err
	}
	archive, err := s.LoadArchive()
	if err != nil {
		return nil, err
	}
	for _, b := range []*tasks.Board{board, archive} {
		for _, t := range b.Tasks {
			dp.titles[t.Ref()] = t.Title
		}
	}
	return dp, nil
}

// dayDates are the n local dates up to now's, oldest first.
func dayDates(now time.Time, n int) []string {
	var out []string
	y, m, d := now.Date()
	for i := n - 1; i >= 0; i-- {
		out = append(out, time.Date(y, m, d-i, 12, 0, 0, 0, now.Location()).Format(time.DateOnly))
	}
	return out
}

// dailyNotes renders the daily notes of the n days up to now's, in
// now's zone, from ps; a day no project's journal has a line of gets
// none. Their Rel is from the vault's root.
func dailyNotes(ps []*dayProject, now time.Time, n int) ([]Note, error) {
	dates := dayDates(now, n)
	byProject := make([]map[string]*projectDay, len(ps))
	nowOpen := make([]*projectDay, len(ps))
	for i, p := range ps {
		byProject[i], nowOpen[i] = projectDays(p.journal, dates, now.Location())
	}
	var notes []Note
	for _, date := range dates {
		var b strings.Builder
		f := dayFront{Date: date, Tags: []string{"day"}, TM: "owned", CSSClasses: []string{"tm-owned"}}
		still := false
		for i, p := range ps {
			d := byProject[i][date]
			if d == nil || d.lines == 0 {
				continue
			}
			f.Projects = append(f.Projects, vaultlink.Link(p.slug, vaultlink.Project, p.slug, ""))
			f.Shipped += len(d.shipped)
			f.Started += len(d.started)
			f.NeedsYou += len(d.review) + len(d.asks) + len(d.held) + len(d.requests)
			if p.writeDay(&b, d, nowOpen[i]) {
				still = true
			}
		}
		if len(f.Projects) == 0 {
			continue
		}
		if still {
			f.Tags = append(f.Tags, "needs-you")
		}
		at, _ := time.ParseInLocation(time.DateOnly, date, now.Location())
		body := fmt.Sprintf("# %s\n\nWritten by tm from the projects' journals (local days, %s); tm rewrites it, so your own notes go elsewhere.\n%s",
			at.Format("Monday 2 January 2006"), zoneName(at), b.String())
		data, err := mdfile.Join(&f, []byte(body))
		if err != nil {
			return nil, err
		}
		notes = append(notes, Note{Rel: DailyNote(date), Data: data})
	}
	return notes, nil
}

func zoneName(t time.Time) string {
	name, _ := t.Zone()
	if t.Location() == time.UTC || name == "" {
		return "UTC"
	}
	return name
}

// task is a task's link and title, as a daily note shows it.
func (p *dayProject) task(ref string, title bool) string {
	alias := ref
	l := vaultlink.Link(p.slug, vaultlink.Task, ref, alias)
	if t := p.titles[ref]; title && t != "" {
		l += " " + heading(t)
	}
	return l
}

func (p *dayProject) thread(id string) string {
	return vaultlink.Link(p.slug, vaultlink.Thread, id, "")
}

func (p *dayProject) other(key string) string {
	return vaultlink.Link(p.slug, vaultlink.Project, key, "")
}

func (p *dayProject) message(id, other string) string {
	if strings.HasPrefix(id, "M-") {
		return vaultlink.Link(p.slug, vaultlink.Message, other+"/"+id, id)
	}
	return id
}

// writeDay writes p's section of a day, d, to b; now is what needs the
// user now. It reports whether anything the day's Needs you lists still
// does.
func (p *dayProject) writeDay(b *strings.Builder, d, now *projectDay) (still bool) {
	fmt.Fprintf(b, "\n## %s\n", vaultlink.Link(p.slug, vaultlink.Project, p.slug, ""))
	section := func(name string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(b, "\n### %s\n\n", name)
		for _, it := range items {
			b.WriteString("- " + it + "\n")
		}
	}

	var items []string
	for _, s := range d.shipped {
		var parts []string
		if s.task != "" {
			parts = append(parts, p.task(s.task, true))
		}
		if s.pr > 0 {
			parts = append(parts, "#"+strconv.Itoa(s.pr))
		}
		if s.thread != "" {
			parts = append(parts, p.thread(s.thread))
		}
		items = append(items, strings.Join(parts, " · "))
	}
	section("Shipped", items)

	items = nil
	for _, s := range d.started {
		if s[0] != "" {
			items = append(items, p.task(s[0], true)+" → "+p.thread(s[1]))
		} else {
			items = append(items, p.thread(s[1]))
		}
	}
	if len(d.added) > 0 {
		refs := make([]string, len(d.added))
		for i, t := range d.added {
			refs[i] = p.task(t, false)
		}
		items = append(items, plural(len(d.added), "task")+" added: "+strings.Join(refs, ", "))
	}
	section("Started", items)

	items = nil
	if len(d.answered) > 0 {
		line := plural(len(d.answered), "question") + " answered: " + strings.Join(d.answered, ", ")
		if len(d.answeredFor) > 0 {
			refs := make([]string, len(d.answeredFor))
			for i, t := range d.answeredFor {
				refs[i] = p.task(t, false)
			}
			line += " (" + strings.Join(refs, ", ") + ")"
		}
		items = append(items, line)
	}
	for _, a := range d.approved {
		items = append(items, p.task(a[0], true)+" → "+a[1]+", the user's word")
	}
	if len(d.memory) > 0 {
		notes := make([]string, len(d.memory))
		for i, m := range d.memory {
			name := strings.TrimSuffix(strings.TrimPrefix(m, "memory/"), ".md")
			notes[i] = vaultlink.Link(p.slug, vaultlink.Memory, m, name)
		}
		items = append(items, "memory: "+strings.Join(notes, ", "))
	}
	if len(d.memoryGone) > 0 {
		names := make([]string, len(d.memoryGone))
		for i, m := range d.memoryGone {
			names[i] = strings.TrimSuffix(strings.TrimPrefix(m, "memory/"), ".md")
		}
		items = append(items, "memory removed: "+strings.Join(names, ", "))
	}
	section("Decided", items)

	items = nil
	open := func(found bool) string {
		if found {
			still = true
			return ", still"
		}
		return ""
	}
	for _, t := range d.review {
		items = append(items, p.task(t[0], true)+" · in "+t[1]+open(slices.Contains(now.review, t)))
	}
	if len(d.asks) > 0 {
		left := 0
		for _, q := range d.asks {
			if slices.Contains(now.asks, q) {
				left++
			}
		}
		line := plural(len(d.asks), "question") + " open: " + strings.Join(d.asks, ", ")
		if left > 0 {
			still = true
			line += fmt.Sprintf(" (%d still)", left)
		}
		items = append(items, line)
	}
	for _, h := range d.held {
		items = append(items, p.message(h[0], h[1])+" to "+p.other(h[1])+" held for the user"+open(slices.Contains(now.held, h)))
	}
	for _, r := range d.requests {
		items = append(items, r[0]+" from "+p.other(r[1])+" waits for the user"+open(slices.Contains(now.requests, r)))
	}
	section("Needs you", items)

	items = nil
	for _, l := range d.links {
		switch {
		case l.peer != "":
			items = append(items, l.verb+" with the paired server "+l.peer)
			continue
		case l.id == "":
			items = append(items, l.verb+" "+p.other(l.other))
			continue
		}
		items = append(items, p.message(l.id, l.other)+" "+l.verb+" "+p.other(l.other))
	}
	if len(d.linkMore) > 0 {
		var more []string
		for _, k := range slices.Sorted(mapKeys(d.linkMore)) {
			more = append(more, fmt.Sprintf("%d %s", d.linkMore[k], k))
		}
		items = append(items, strings.Join(more, " · "))
	}
	section("Links", items)

	if len(d.guardBy) > 0 {
		total := 0
		var by []string
		for _, w := range d.guardBy {
			total += d.guard[w]
			s := w
			if thread.ValidID(w) {
				s = p.thread(w)
			}
			if d.guard[w] > 1 {
				s += " ×" + strconv.Itoa(d.guard[w])
			}
			by = append(by, s)
		}
		line := plural(total, "denial") + ": " + strings.Join(by, ", ")
		if len(d.rules) > 0 {
			line += " (" + strings.Join(d.rules, ", ") + ")"
		}
		section("Guard", []string{line})
	}

	var counts []string
	for _, k := range []string{"step change", "run", "prompt", "report", "fast-forward", "other"} {
		if n := d.counts[k]; n > 0 {
			if k == "other" {
				counts = append(counts, fmt.Sprintf("%d other", n))
			} else {
				counts = append(counts, plural(n, k))
			}
		}
	}
	if len(counts) > 0 {
		section("Counts", []string{strings.Join(counts, " · ")})
	}
	return still
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// vaultRoot is the vault's folder: the projects folder.
func vaultRoot() (string, error) { return home.ProjectsDir() }

// RenderDaily writes the daily notes of the days days up to now's, in
// now's zone, from every project's journal, through the ledger: a note
// is written only when it changed, and none is ever removed. It returns
// how many it wrote.
func RenderDaily(now time.Time, days int) (int, error) {
	if days < 1 || days > MaxDays {
		return 0, fmt.Errorf("days: %d is not 1 to %d", days, MaxDays)
	}
	ps, err := dayProjects()
	if err != nil && len(ps) == 0 {
		return 0, err
	}
	notes, nerr := dailyNotes(ps, now, days)
	if nerr != nil {
		return 0, nerr
	}
	root, rerr := vaultRoot()
	if rerr != nil {
		return 0, rerr
	}
	written := 0
	lerr := withLedger(func(files map[string]string) error {
		var errs []error
		for _, n := range notes {
			w, err := put(files, n.Rel, filepath.Join(root, filepath.FromSlash(n.Rel)), n.Data)
			if err != nil {
				errs = append(errs, err)
			} else if w {
				written++
			}
		}
		return errors.Join(errs...)
	})
	return written, errors.Join(err, lerr)
}

// dayProjects are the projects whose journals the daily notes read; a
// project that can't be read is left out, with its error.
func dayProjects() ([]*dayProject, error) {
	list, err := project.List()
	if err != nil {
		return nil, err
	}
	var out []*dayProject
	var errs []error
	for _, s := range list {
		p, err := project.Open(s.Slug)
		if err == nil {
			var dp *dayProject
			if dp, err = loadDayProject(p); err == nil {
				out = append(out, dp)
				continue
			}
		}
		errs = append(errs, fmt.Errorf("%s: %w", s.Slug, err))
	}
	return out, errors.Join(errs...)
}

// DailyStamp is a cheap mark of what the ticker's daily notes come from:
// now's date, and the size and time of each project's journal and its
// archives' folder. RenderDaily changes only when it does.
func DailyStamp(now time.Time) string {
	var b strings.Builder
	b.WriteString(now.Format(time.DateOnly) + ";")
	root, err := vaultRoot()
	if err != nil {
		return b.String()
	}
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if !e.IsDir() || !project.ValidSlug(e.Name()) {
			continue
		}
		b.WriteString(e.Name() + ":")
		for _, name := range append([]string{"JOURNAL.md", "journal"}, tasks.Rels()...) {
			if fi, err := os.Stat(filepath.Join(root, e.Name(), filepath.FromSlash(name))); err == nil {
				fmt.Fprintf(&b, "%d.%d;", fi.Size(), fi.ModTime().UnixNano())
			} else {
				b.WriteString("-;")
			}
		}
	}
	return b.String()
}

// withLedger runs fn on the hash ledger's files under its lock and saves
// them, whatever fn returns.
func withLedger(fn func(files map[string]string) error) error {
	file, err := ledgerPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	unlock, err := mdfile.Lock(file)
	if err != nil {
		return err
	}
	defer unlock()
	l := readLedger(file)
	if l.Off {
		if !On() {
			return nil // tm vault off ran while the notes rendered
		}
		l.Off = false
	}
	ferr := fn(l.Files)
	b, err := json.MarshalIndent(&l, "", "  ")
	if err == nil {
		err = fsx.WriteAtomic(file, append(b, '\n'), 0o600)
	}
	return errors.Join(ferr, err)
}

// put writes data to path, the note the ledger keeps as key, unless the
// ledger has its hash and the file is there; it reports whether it wrote.
func put(files map[string]string, key, path string, data []byte) (bool, error) {
	h := hash(data)
	if files[key] == h {
		if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() {
			return false, nil
		}
	}
	if err := keepEdit(key, path, files[key]); err != nil {
		return false, err
	}
	if err := writeNote(path, data, true); err != nil {
		return false, err
	}
	files[key] = h
	return true, nil
}
