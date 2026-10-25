package vault

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// The derived notes (T718, docs/SPEC.md §5.1, Vault): a note per task
// and per thread, which tm renders from the files that stay the source
// (TASKS.md and tasks/ARCHIVE.md, thread.toml, STATUS.md, the reports)
// whenever they change, so that Obsidian has a node, a backlink target
// and a row in a Base for each. Only tm writes them: a human's edit is
// overwritten on the next change.

// Note is a derived note: its path in the project's folder, with forward
// slashes, and its text. Guard is for a path someone else may have a
// file at (designs/): tm writes it only over a file it wrote.
type Note struct {
	Rel   string
	Data  []byte
	Guard bool
}

// taskFront is a task note's properties.
type taskFront struct {
	ID         string   `toml:"id" yaml:"id"`
	Title      string   `toml:"title" yaml:"title"`
	Project    string   `toml:"project" yaml:"project"`
	Status     string   `toml:"status" yaml:"status"`
	Group      string   `toml:"group" yaml:"group"`
	Thread     string   `toml:"thread,omitempty" yaml:"thread,omitempty"`
	PR         string   `toml:"pr,omitempty" yaml:"pr,omitempty"`
	Design     string   `toml:"design,omitempty" yaml:"design,omitempty"`
	After      []string `toml:"after,omitempty" yaml:"after,omitempty"`
	Owner      string   `toml:"owner,omitempty" yaml:"owner,omitempty"`
	Steps      int      `toml:"steps" yaml:"steps"`
	StepsDone  int      `toml:"steps_done" yaml:"steps_done"`
	Created    string   `toml:"created,omitempty" yaml:"created,omitempty"`
	Updated    string   `toml:"updated,omitempty" yaml:"updated,omitempty"`
	Archived   bool     `toml:"archived,omitempty" yaml:"archived,omitempty"`
	Tags       []string `toml:"tags" yaml:"tags"`
	TM         string   `toml:"tm" yaml:"tm"`
	CSSClasses []string `toml:"cssclasses" yaml:"cssclasses"`
}

// threadFront is a thread note's properties.
type threadFront struct {
	ID         string    `toml:"id" yaml:"id"`
	Title      string    `toml:"title" yaml:"title"`
	Project    string    `toml:"project" yaml:"project"`
	Task       string    `toml:"task,omitempty" yaml:"task,omitempty"`
	State      string    `toml:"state" yaml:"state"`
	Agent      string    `toml:"agent" yaml:"agent"`
	Model      string    `toml:"model,omitempty" yaml:"model,omitempty"`
	Branch     string    `toml:"branch,omitempty" yaml:"branch,omitempty"`
	PR         string    `toml:"pr,omitempty" yaml:"pr,omitempty"`
	Created    time.Time `toml:"created,omitzero" yaml:"created,omitempty"`
	Resolved   time.Time `toml:"resolved,omitzero" yaml:"resolved,omitempty"`
	Reports    int       `toml:"reports" yaml:"reports"`
	Turns      int       `toml:"turns" yaml:"turns"`
	CostUSD    float64   `toml:"cost_usd,omitempty" yaml:"cost_usd,omitempty"`
	NeedsYou   string    `toml:"needs_you,omitempty" yaml:"needs_you,omitempty"`
	Tags       []string  `toml:"tags" yaml:"tags"`
	TM         string    `toml:"tm" yaml:"tm"`
	CSSClasses []string  `toml:"cssclasses" yaml:"cssclasses"`
}

// Notes renders every derived note of p: tasks/T<n>.md for each task
// on the board, tasks/archive/T<n>.md for each archived one,
// threads/<id>/<id>.md for each thread whose folder is there, and
// designs/<name>.md and .canvas for each design (designNotes).
func Notes(p *project.Project) ([]Note, error) {
	s := p.Tasks()
	board, err := s.Load()
	if err != nil {
		return nil, err
	}
	archive, err := s.LoadArchive()
	if err != nil {
		return nil, err
	}
	recs, err := thread.List(p)
	if err != nil {
		return nil, err
	}
	byID := map[string]*thread.Record{}
	prs := map[string]string{}
	for _, r := range recs {
		byID[r.ID] = r
		if all := thread.ReportPRs(p, r.ID); len(all) > 0 {
			prs[r.ID] = all[len(all)-1]
		}
	}
	var notes []Note
	for _, b := range []*tasks.Board{board, archive} {
		for _, t := range b.Tasks {
			n, err := taskNote(p.Slug, t, byID[t.Thread], prs[t.Thread], b == archive)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", t.Ref(), err)
			}
			notes = append(notes, n)
		}
	}
	for _, r := range recs {
		n, err := threadNote(p, r, prs[r.ID])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.ID, err)
		}
		notes = append(notes, n)
	}
	ds, err := designNotes(p, board, archive)
	return append(notes, ds...), err
}

// TaskNote is the path of task ref's note, live or archived.
func TaskNote(ref string, archived bool) string {
	if archived {
		return "tasks/archive/" + ref + ".md"
	}
	return "tasks/" + ref + ".md"
}

// ThreadNote is the path of thread id's note.
func ThreadNote(id string) string { return "threads/" + id + "/" + id + ".md" }

// derived reports whether rel is where Notes puts a note: a task's, live
// or archived, or a thread's.
func derived(rel string) bool {
	dir, name := path.Split(rel)
	ref := strings.TrimSuffix(name, ".md")
	switch {
	case !strings.HasSuffix(name, ".md"):
		return false
	case dir == "tasks/" || dir == "tasks/archive/":
		return tasks.ValidRef(ref)
	}
	return thread.ValidID(ref) && dir == "threads/"+ref+"/"
}

func taskNote(slug string, t *tasks.Task, r *thread.Record, pr string, archived bool) (Note, error) {
	f := taskFront{
		ID: t.Ref(), Title: t.Title, Status: string(t.Status), Group: string(tasks.GroupOf(t.Status)),
		Project: vaultlink.Link(slug, vaultlink.Project, slug, ""),
		Owner:   t.Owner, Steps: len(t.Steps), StepsDone: t.StepsDone(),
		Created: t.Created, Updated: t.Updated, Archived: archived,
		Tags: []string{"task"}, TM: "owned", CSSClasses: []string{"tm-owned"},
	}
	if t.Status == tasks.Review || t.Status == tasks.Blocked {
		f.Tags = append(f.Tags, "needs-you")
	}
	if t.Thread != "" {
		f.Thread, f.PR = vaultlink.Link(slug, vaultlink.Thread, t.Thread, ""), pr
	}
	if t.Design != "" {
		f.Design = vaultlink.Link(slug, vaultlink.Design, t.Design, "")
	}
	for _, a := range t.After {
		f.After = append(f.After, vaultlink.Link(slug, vaultlink.Task, "T"+strconv.Itoa(a), ""))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n", t.Ref(), heading(t.Title))
	if t.Notes != "" {
		b.WriteString("\n" + body(slug, t.Notes) + "\n")
	}
	if len(t.Steps) > 0 {
		b.WriteString("\n## Steps\n\n")
		for _, s := range t.Steps {
			mark := " "
			if s.Done {
				mark = "x"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", mark, body(slug, s.Text))
		}
	}
	if r != nil && r.Reports > 0 {
		fmt.Fprintf(&b, "\n## Thread\n\n![[%s#Report]]\n", vaultlink.Target(slug, vaultlink.Thread, r.ID))
	}
	data, err := mdfile.Join(&f, []byte(b.String()))
	return Note{Rel: TaskNote(t.Ref(), archived), Data: data}, err
}

func threadNote(p *project.Project, r *thread.Record, pr string) (Note, error) {
	slug := p.Slug
	f := threadFront{
		ID: r.ID, Title: r.Title, Project: vaultlink.Link(slug, vaultlink.Project, slug, ""),
		State: r.State, Agent: r.Agent, Model: r.Model, Branch: r.Branch, PR: pr,
		Created: r.Created.UTC(), Resolved: r.ResolvedAt.UTC(), Reports: r.Reports,
		Turns: r.Usage.Turns, CostUSD: r.Usage.CostUSD,
		Tags: []string{"thread"}, TM: "owned", CSSClasses: []string{"tm-owned"},
	}
	if r.Task != "" {
		f.Task = vaultlink.Link(slug, vaultlink.Task, r.Task, "")
	}
	if st, err := thread.ReadStatus(p, r.ID); err == nil && st.NeedsYou != "" && r.State != thread.Resolved {
		f.NeedsYou = st.NeedsYou
		f.Tags = append(f.Tags, "needs-you")
	}
	file := func(name, alias string) string {
		return vaultlink.Link(slug, vaultlink.ThreadFile, r.ID+"/"+name, alias)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n", r.ID, heading(r.Title))
	var files []string
	for _, name := range []string{"brief", "task", "STATUS"} {
		if _, err := os.Stat(thread.Path(p, r.ID, name+".md")); err == nil {
			files = append(files, "- "+file(name, name)+"\n")
		}
	}
	if len(files) > 0 {
		b.WriteString("\n## Files\n\n" + strings.Join(files, ""))
	}
	if r.Reports > 0 {
		b.WriteString("\n## Reports\n\n")
		for n := 1; n < r.Reports; n++ {
			if _, err := os.Stat(thread.Path(p, r.ID, "reports", strconv.Itoa(n)+".md")); err == nil {
				fmt.Fprintf(&b, "- %s\n", file("reports/"+strconv.Itoa(n), "report "+strconv.Itoa(n)))
			}
		}
		fmt.Fprintf(&b, "- %s (latest)\n", file("REPORT", "report "+strconv.Itoa(r.Reports)))
	}
	if lib := thread.Attachments(p, r.ID); len(lib) > 0 {
		b.WriteString("\n## Library\n\n")
		for _, name := range lib {
			fmt.Fprintf(&b, "- %s\n", file("library/"+name, textsafe.Line(name, 200)))
		}
	}
	if rep, err := thread.ReadReport(p, r.ID); err == nil && rep != nil {
		// The thread's words, through the vault guard (a report stored
		// before it may hold HTML), quoted in a callout.
		fmt.Fprintf(&b, "\n## Report\n\n> [!quote] Report %d from %s · the thread's words\n", r.Reports, r.ID)
		for _, l := range strings.Split(strings.TrimRight(textsafe.Vault(textsafe.Text(rep.Text)), "\n"), "\n") {
			b.WriteString(strings.TrimRight("> "+l, " ") + "\n")
		}
	}
	data, err := mdfile.Join(&f, []byte(b.String()))
	return Note{Rel: ThreadNote(r.ID), Data: data}, err
}

// heading is a one-line title as a heading shows it, guarded.
func heading(title string) string {
	return textsafe.Vault(textsafe.Line(title, 0))
}

// body is someone's Markdown (a task's notes, a step) as a derived note
// shows it: guarded (textsafe.Vault), its ids linked (vaultlink.Text).
func body(slug, s string) string {
	return vaultlink.Text(slug, textsafe.Vault(textsafe.Text(s)))
}

// Stamp is a cheap mark of what p's derived notes come from: the size
// and time of TASKS.md, the archive, designs.json, questions.json, each
// file in designs/ and each thread's record, STATUS, latest report and
// folders of reports and library. Notes changes only
// when it does, so a caller renders on change by comparing stamps.
func Stamp(p *project.Project) string {
	var b strings.Builder
	stamp := func(path string) {
		if fi, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "%d.%d;", fi.Size(), fi.ModTime().UnixNano())
		} else {
			b.WriteString("-;")
		}
	}
	stamp(p.Path("TASKS.md"))
	stamp(p.Path("tasks", "ARCHIVE.md"))
	stamp(p.Path("designs.json"))
	stamp(p.Path("questions.json"))
	files, _ := os.ReadDir(p.Path("designs"))
	for _, e := range files {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b.WriteString(e.Name() + ":")
		stamp(p.Path("designs", e.Name()))
	}
	ents, _ := os.ReadDir(p.Path("threads"))
	for _, e := range ents {
		if !e.IsDir() || !thread.ValidID(e.Name()) {
			continue
		}
		b.WriteString(e.Name() + ":")
		for _, name := range []string{"thread.toml", "STATUS.md", "REPORT.md", "reports", "library", "brief.md", "task.md"} {
			stamp(thread.Path(p, e.Name(), name))
		}
	}
	return b.String()
}
