package project

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Caps on `tm context` sections (§7.6). Each section says what it left
// out, so the output stays a fixed size whatever the files hold.
const (
	capInstructions = 60
	capContext      = 120
	capMemory       = 60
	capTasksGroup   = 40
	capDone         = 3
	capInbox        = 20
	capJournal      = 10
	// Journal lines are clipped to this width (runes): the full text is
	// in JOURNAL.md.
	capJournalLine = 160
)

// Section is one part of `tm context`.
type Section struct {
	Title string   `json:"title"`
	Lines []string `json:"lines"`
	// Omitted says what the cap left out, e.g. "12 more lines in CONTEXT.md".
	Omitted string `json:"omitted,omitempty"`
}

// Ticked is what the ticker last saw (state/ticker.json, ticker.Seen):
// each thread's PR state by thread id (ticker.PR's Summary), and a note
// by repo path on a checkout that is behind origin
// (worktree.Checkout's Note). Nil maps hold none.
type Ticked struct {
	PRs       map[string]string
	Checkouts map[string]string
	// Queues are the project's sessions whose queued prompts are held
	// while the agent is idle (session.list, proto.SessionInfo.QueueNote).
	Queues []HeldQueue
	// Questions are the question menus open in the project's sessions,
	// as their mods sent them (session.list, proto.SessionInfo.Question).
	Questions []OpenQuestion
	// Working counts every project's threads that count toward the cap
	// across projects, by slug (thread.WorkingAll); nil when no server
	// answered.
	Working map[string]int
	// Links are the project's links to other projects' coordinators, a
	// line each (internal/link, which reads this package): none hides
	// the section.
	Links []string
}

// OpenQuestion is one session's open question menu, its Lines as tm
// thread show prints them.
type OpenQuestion struct {
	Session, Role, Thread, Task string
	Since                       time.Time
	Lines                       []string
}

// HeldQueue is one session's held prompt queue.
type HeldQueue struct {
	Session, Role, Thread, Task string
	Queued                      int
	Why                         string
	Since                       time.Time
}

// Context builds `tm context`. It only reads files, and the same files
// always give the same output (§7.6).
//
// threads builds the Threads section from the PR state by thread id; it is
// given because internal/thread, which reads the thread records, imports
// this package (thread.Context passes thread.ContextSection). Nil leaves
// the section out.
func (p *Project) Context(seen Ticked, threads func(prs map[string]string) (Section, error)) ([]Section, error) {
	prs := seen.PRs
	var out []Section

	head := []string{
		"Project: " + p.Slug,
		"Folder: " + p.Dir,
		"Goal: " + orNone(p.Meta.Goal),
	}
	if len(p.Repos) == 0 {
		head = append(head, "Repos: (none)")
	}
	for _, r := range p.Repos {
		line := "Repo: " + r
		if n := seen.Checkouts[r]; n != "" {
			line += " · " + n
		}
		head = append(head, line)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	safety, err := cfg.Safety(p.Slug)
	if err != nil {
		return nil, err
	}
	closeRule := safety.AutoClose
	if closeRule == config.CloseDays {
		closeRule = fmt.Sprintf("%d days after done or merged", safety.AutoCloseDays)
	}
	head = append(head, fmt.Sprintf("Safety: start_threads=%s · yolo=%t · coordinator_approves=%t · parallel_threads=%s · auto_close=%s · complete_tasks=%s (config.toml; the human's)",
		safety.StartThreads, safety.Yolo, safety.CoordinatorApproves, fmt.Sprint(safety.ParallelThreads)+globalCap(safety.GlobalParallelThreads, seen.Working), closeRule, safety.CompleteTasks))
	if safety.Paused {
		head = append(head, "Paused by the user: no nudges, no PR follow-up, and tm thread start refuses (project-paused) until they resume it")
	}
	for _, q := range seen.Queues {
		who := q.Role
		switch {
		case q.Task != "":
			who += " " + q.Task + " (" + q.Thread + ")"
		case q.Thread != "":
			who += " " + q.Thread
		}
		head = append(head, fmt.Sprintf("Prompt queue: %s (%s) has %d prompt(s) held since %s (%s): nothing is pasted, and the coordinator gets no nudges, until it clears; after its bound the server drops a held prompt, or writes its own fixed-word ones to the agent's socket, unconfirmed (JOURNAL.md prompt.sent, prompt.dropped)",
			q.Session, who, q.Queued, q.Since.UTC().Format("2006-01-02 15:04 UTC"), q.Why))
	}
	for _, q := range seen.Questions {
		who, ref := q.Role, q.Session
		switch {
		case q.Task != "":
			who, ref = who+" "+q.Task+" ("+q.Thread+")", q.Task
		case q.Thread != "":
			who, ref = who+" "+q.Thread, q.Thread
		}
		head = append(head, fmt.Sprintf("Question: %s (%s) waits on a question menu since %s; relay the user's answer with tm thread answer %s --choice N | --option LABEL | --text T [--question K]:",
			q.Session, who, q.Since.UTC().Format("2006-01-02 15:04 UTC"), ref))
		for _, l := range q.Lines {
			head = append(head, "  "+l)
		}
	}
	head = append(head, agentModelLines(cfg, safety)...)
	out = append(out, Section{Title: "Project", Lines: head})
	out = append(out, capLines("Standing instructions (PROJECT.md)", splitLines(p.Instructions), capInstructions, "PROJECT.md"))

	// The memory index as tm keeps it: a note added or removed by hand,
	// or a list an older tm left, is brought in line first.
	if _, err := p.SyncIndex(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, f := range []struct {
		title, file string
		cap         int
	}{{"Context (CONTEXT.md)", "CONTEXT.md", capContext}, {"Memory index (MEMORY.md)", "MEMORY.md", capMemory}} {
		data, err := os.ReadFile(p.Path(f.file))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		out = append(out, capLines(f.title, splitLines(string(data)), f.cap, f.file))
	}
	// The coordinator's stored questions (tm ask); only when some are open.
	qs, err := p.Questions()
	if err != nil {
		return nil, err
	}
	if len(qs) > 0 {
		sec := Section{Title: "Open questions (tm ask)"}
		for _, q := range qs {
			sec.Lines = append(sec.Lines, QuestionLine(q))
		}
		out = append(out, sec)
	}
	// Context files over their size budget (upkeep.go); only when some are.
	over, err := p.Oversized()
	if err != nil {
		return nil, err
	}
	if len(over) > 0 {
		up := Section{Title: "Upkeep"}
		for _, o := range over {
			up.Lines = append(up.Lines, o.String())
		}
		out = append(out, up)
	}

	ts, err := p.taskSection()
	if err != nil {
		return nil, err
	}
	out = append(out, ts)

	if threads != nil {
		th, err := threads(prs)
		if err != nil {
			return nil, err
		}
		out = append(out, th)
	}

	items, err := p.Inbox()
	if err != nil {
		return nil, err
	}
	inbox := Section{Title: "Inbox"}
	for i, it := range items {
		if i == capInbox {
			inbox.Omitted = fmt.Sprintf("%d more items (tm inbox list)", len(items)-capInbox)
			break
		}
		flag := ""
		if it.NeedsUser {
			flag = " [needs user]"
		}
		inbox.Lines = append(inbox.Lines, fmt.Sprintf("%s  %s%s: %s", it.ID, it.Kind, flag, it.Summary))
	}
	out = append(out, inbox)
	if len(seen.Links) > 0 {
		out = append(out, Section{Title: "Links (tm link)", Lines: seen.Links})
	}

	lines, total, err := p.JournalTail(capJournal)
	if err != nil {
		return nil, err
	}
	for i, l := range lines {
		lines[i] = textsafe.Clip(l, capJournalLine)
	}
	j := Section{Title: fmt.Sprintf("Journal (last %d)", capJournal), Lines: lines}
	if total > len(lines) {
		j.Omitted = fmt.Sprintf("%d earlier lines in JOURNAL.md", total-len(lines))
	}
	out = append(out, j)
	return out, nil
}

// agentModelLines name the agents the coordinator may start threads
// with and their models (docs/SPEC.md §8.2, §11.2): only the installed
// agents; the thread agent tm thread start runs without --agent (the
// user's setting, or the one installed agent); and each agent's
// models in the project's scope, as the agent itself described them,
// its older versions on one line, or why they are unknown.
func agentModelLines(cfg *config.Config, safety config.Safety) []string {
	reg := models.Registry()
	if reg == nil {
		return nil
	}
	installed := models.Installed(reg, nil)
	name, auto, err := models.Resolve(reg, installed, safety.ThreadAgent, "thread agent")
	var out []string
	switch {
	case len(installed) == 0:
		return []string{"Agents: none installed, so no thread can start (tm doctor; the human installs one)"}
	case err != nil:
		var me *models.Error
		errors.As(err, &me)
		msg := err.Error()
		if me != nil {
			msg = me.Msg
		}
		out = append(out, "Thread agent: "+msg)
	case auto:
		out = append(out, "Thread agent: "+name+" (the only agent installed): tm thread start runs it")
	default:
		out = append(out, "Thread agent: "+name+" (config.toml; the human's): tm thread start runs it")
	}
	if others := slices.DeleteFunc(slices.Clone(installed), func(n string) bool { return n == name }); len(others) > 0 && err == nil {
		out[0] += "; --agent may name another installed agent: " + strings.Join(others, ", ")
	}
	var known []string
	for _, c := range models.Catalogs(reg, cfg, installed) {
		known = append(known, c.Names()...)
		if !c.Known {
			out = append(out, fmt.Sprintf("Models of %s: unknown (%s); its threads run %s's own default, and --model is refused", c.Agent, c.Reason, c.Agent))
			continue
		}
		out = append(out, fmt.Sprintf("Models of %s (tm thread start --model; without it, %s):", c.Agent, launchDefault(c, safety.Models)))
		var older []string
		shown := 0
		for _, m := range c.InScope(safety.Models) {
			shown++
			if m.Older {
				older = append(older, m.Name)
				continue
			}
			line := "  " + m.Name
			if a := m.About(); a != "" {
				line += ": " + a
			}
			if m.Yours {
				line += " (added by the user; " + c.Agent + " doesn't list it)"
			}
			if m.Default {
				line += " (" + c.Agent + "'s own default)"
			}
			out = append(out, line)
		}
		if len(older) > 0 {
			out = append(out, "  also (older versions): "+strings.Join(older, ", "))
		}
		if shown == 0 {
			out = append(out, "  (none in this project's models: start threads without --model)")
		}
		for _, m := range c.Refused {
			out = append(out, fmt.Sprintf("  refused for the user's account: %s (%s); --model %s is refused", m.Name, m.Refusal.Reason, m.Name))
		}
	}
	if len(safety.Models) > 0 {
		out = append(out, "The user limits this project's models to: "+strings.Join(safety.Models, ", ")+" (config.toml; the human's). Any other --model is refused.")
		for _, n := range safety.Models {
			if !slices.Contains(known, n) {
				out = append(out, "  stale: "+n+" is allowed but no installed agent offers it now; --model "+n+" is refused")
			}
		}
	}
	return out
}

// launchDefault says what a thread without --model runs.
func launchDefault(c models.Catalog, scope []string) string {
	if d := c.LaunchModel(scope); d != "" {
		return d + ", the user's default"
	}
	return c.Agent + "'s own default"
}

func (p *Project) taskSection() (Section, error) {
	b, err := p.Tasks().Load()
	if err != nil {
		return Section{}, err
	}
	b.Sort()
	s := Section{Title: "Tasks"}
	var omitted []string
	for _, g := range tasks.Groups {
		var ts []*tasks.Task
		for _, t := range b.Tasks {
			if tasks.GroupOf(t.Status) == g {
				ts = append(ts, t)
			}
		}
		if len(ts) == 0 {
			continue
		}
		limit := capTasksGroup
		if g == tasks.DoneG {
			// The most recent done tasks matter; show the highest ids.
			limit = capDone
			if len(ts) > limit {
				ts = ts[len(ts)-limit:]
			}
		}
		s.Lines = append(s.Lines, fmt.Sprintf("%s (%d)", g, countGroup(b, g)))
		for _, l := range tasks.Lines(ts[:min(len(ts), limit)]) {
			s.Lines = append(s.Lines, "  "+l)
		}
		if n := countGroup(b, g) - min(len(ts), limit); n > 0 {
			omitted = append(omitted, fmt.Sprintf("%d %s", n, strings.ToLower(string(g))))
		}
	}
	if len(s.Lines) == 0 {
		s.Lines = []string{"(no tasks)"}
	}
	if len(omitted) > 0 {
		s.Omitted = strings.Join(omitted, ", ") + " tasks not shown (tm task list)"
	}
	return s, nil
}

func countGroup(b *tasks.Board, g tasks.Group) int {
	n := 0
	for _, t := range b.Tasks {
		if tasks.GroupOf(t.Status) == g {
			n++
		}
	}
	return n
}

// RenderContext prints sections as plain text: each line without what a
// terminal would act on (textsafe.Clean), whoever wrote it.
func RenderContext(sections []Section) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## %s\n", s.Title)
		for _, l := range s.Lines {
			b.WriteString(textsafe.Clean(l, false) + "\n")
		}
		if len(s.Lines) == 0 {
			b.WriteString("(empty)\n")
		}
		if s.Omitted != "" {
			fmt.Fprintf(&b, "[… %s]\n", s.Omitted)
		}
	}
	return b.String()
}

func capLines(title string, lines []string, n int, file string) Section {
	s := Section{Title: title, Lines: lines}
	if len(lines) > n {
		s.Lines = lines[:n]
		s.Omitted = fmt.Sprintf("%d more lines in %s", len(lines)-n, file)
	}
	return s
}

// splitLines trims surrounding blank lines and splits; "" gives nil.
func splitLines(s string) []string {
	s = strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func orNone(s string) string {
	if s == "" {
		return "(none set)"
	}
	return s
}

// CodeHosts is every repo of every project, with the code host its PRs
// live on; what tm doctor's code-host checks run for.
func CodeHosts() []codehost.RepoHost {
	list, err := List()
	if err != nil {
		return nil
	}
	var out []codehost.RepoHost
	seen := map[string]bool{}
	for _, s := range list {
		if s.Error != "" {
			continue
		}
		p, err := Open(s.Slug)
		if err != nil {
			continue
		}
		for _, r := range p.Repos {
			if k := s.Slug + "\x00" + r; !seen[k] {
				seen[k] = true
				out = append(out, codehost.RepoHost{Repo: r, Target: codehost.Detect(r, p.CodeHost())})
			}
		}
	}
	return out
}

// globalCap is the Safety line's global_parallel_threads, after
// parallel_threads: the cap across projects and, with a server, how many
// threads work in which projects; "" without such a cap.
func globalCap(n int, working map[string]int) string {
	if n <= 0 {
		return ""
	}
	out := fmt.Sprintf(" · global_parallel_threads=%d", n)
	if working == nil {
		return out
	}
	slugs := slices.Sorted(maps.Keys(working))
	total, parts := 0, []string{}
	for _, s := range slugs {
		total += working[s]
		parts = append(parts, fmt.Sprintf("%s %d", s, working[s]))
	}
	out += fmt.Sprintf(" (%d working", total)
	if len(parts) > 0 {
		out += ": " + strings.Join(parts, ", ")
	}
	return out + ")"
}
