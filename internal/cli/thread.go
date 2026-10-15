package cli

// Threads (docs/SPEC.md §7, §9, M6): tm thread …, tm task delegate, and
// the thread's own tm report, tm status and tm done. Files are written
// here; sessions are the server's.

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

const threadUsage = `usage: tm thread <command> [--project <slug>] [--json]

  start [--task T12] [--agent A] [--model M] [--repo PATH] [--base B] [--approved-by-user] [--over-cap] "title"
  adopt <session> [--task T12] [--title "…"]
                                 make a running agent session outside the projects a thread
  list [--all]                   open threads; --all adds resolved and archived ones
  show <id>                      <id> is a thread id (t-0003) or a task id (T12):
                                 the task's open thread; an archived thread is read
                                 from threads/archive/
  read <id> [--lines N]          the thread's screen as text
  prompt <id> "text" | --next N  queue a prompt (sent when idle; refused while blocked)
  approve <id> [--choice N]      answer a permission prompt with "allow once"
  answer <id> [--choice N[,N…]] [--option LABEL]… [--text T] [--question K]
                                 relay the user's answer to a question menu
  ack <id>                       acknowledge the latest report
  stop <id> | restart <id>
  merge <id>                     merge the thread's PR (merge = coordinator): checks green on
                                 its head, mergeable, regular merge commit; else a refusal says why
  resolve <id> [--discard]       --discard also removes a worktree with uncommitted changes
                                 and its branch; refused when the branch has unsaved commits

Exit codes: 0 done or already true, 1 refused, 2 usage, 3 I/O.`

// call makes one server call; a refusal becomes exit 1.
func (e *Env) call(method string, params, result any) error {
	c, _, err := connect(true)
	if err != nil {
		return err
	}
	defer c.Close()
	err = c.Call(method, params, result)
	var perr *proto.Error
	if errors.As(err, &perr) {
		switch perr.Code {
		case proto.ErrRefused, proto.ErrUnknownSession:
			return &tasks.Error{Code: perr.Code, Msg: perr.Message}
		case proto.ErrBadParams:
			return usagef("%s", perr.Message)
		}
	}
	return err
}

// sessionOf returns the live session running a thread, if any.
func (e *Env) sessionOf(r *thread.Record) (proto.SessionInfo, bool) {
	if r.Session == "" {
		return proto.SessionInfo{}, false
	}
	var res proto.SessionListResult
	if err := e.call(proto.MethodSessionList, nil, &res); err != nil {
		return proto.SessionInfo{}, false
	}
	for _, s := range res.Sessions {
		if s.ID == r.Session {
			return s, true
		}
	}
	return proto.SessionInfo{}, false
}

// coordinatorOnly refuses threads, and agents of another project.
func (e *Env) coordinatorOnly(p *project.Project, what string) error {
	if e.Caller.Kind == caller.Thread {
		return &tasks.Error{Code: "coordinator-only", Msg: what + " is the coordinator's"}
	}
	if e.Caller.IsAgent() && e.Caller.Project != "" && e.Caller.Project != p.Slug {
		return &tasks.Error{Code: "other-project", Msg: fmt.Sprintf("agents of project %s can't change project %s", e.Caller.Project, p.Slug)}
	}
	return nil
}

func runThread(e *Env, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		if len(args) == 0 {
			return usagef("%s", threadUsage)
		}
		fmt.Fprintln(e.Stdout, threadUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	f := newFlags()
	slug, asJSON := f.String("project"), f.Bool("json")
	var run func(p *project.Project, pos []string) error
	oneID := func(pos []string, usage string) (string, error) {
		if len(pos) != 1 {
			return "", usagef("usage: tm thread %s", usage)
		}
		return pos[0], nil
	}
	switch sub {
	case "start":
		o := startOpts{task: f.String("task"), agent: f.String("agent"), model: f.String("model"), repo: f.String("repo"),
			base: f.String("base"), approved: f.Bool("approved-by-user"), overCap: f.Bool("over-cap")}
		run = func(p *project.Project, pos []string) error {
			if len(pos) > 1 {
				return usagef("usage: tm thread start [--task T12] [flags] \"title\" (quote the title)")
			}
			if len(pos) == 1 {
				o.title = pos[0]
			}
			return e.threadStart(p, o, *asJSON)
		}
	case "adopt":
		o := adoptOpts{task: f.String("task"), title: f.String("title")}
		// Accepted and ignored: the user's go-ahead to adopt is T on the
		// dashboard, which no flag stands in for (threadAdopt).
		f.Bool("approved-by-user")
		run = func(p *project.Project, pos []string) error {
			if len(pos) != 1 {
				return usagef("usage: tm thread adopt <session> [--task T12] [--title \"…\"]")
			}
			return e.threadAdopt(p, pos[0], o, *asJSON)
		}
	case "list", "ls":
		all := f.Bool("all")
		run = func(p *project.Project, pos []string) error {
			if len(pos) != 0 {
				return usagef("usage: tm thread list [--all]")
			}
			return e.threadList(p, *all, *asJSON)
		}
	case "show":
		run = func(p *project.Project, pos []string) error {
			id, err := oneID(pos, "show <id>")
			if err != nil {
				return err
			}
			return e.threadShow(p, id, *asJSON)
		}
	case "read":
		lines := f.String("lines")
		run = func(p *project.Project, pos []string) error {
			id, err := oneID(pos, "read <id> [--lines N]")
			if err != nil {
				return err
			}
			n := 40
			if f.IsSet("lines") {
				if n, err = strconv.Atoi(*lines); err != nil || n < 1 {
					return usagef("--lines takes a positive number")
				}
			}
			return e.threadRead(p, id, n)
		}
	case "prompt":
		next := f.String("next")
		run = func(p *project.Project, pos []string) error {
			if len(pos) < 1 || (len(pos) == 1) == !f.IsSet("next") || len(pos) > 2 {
				return usagef("usage: tm thread prompt <id> \"text\" | tm thread prompt <id> --next N")
			}
			text := ""
			if len(pos) == 2 {
				text = pos[1]
			}
			return e.threadPrompt(p, pos[0], text, *next)
		}
	case "approve":
		choice := f.String("choice")
		run = func(p *project.Project, pos []string) error {
			id, err := oneID(pos, "approve <id> [--choice N]")
			if err != nil {
				return err
			}
			n := 1
			if f.IsSet("choice") {
				if n, err = strconv.Atoi(*choice); err != nil || n < 1 || n > 9 {
					return usagef("--choice takes a number 1-9")
				}
			}
			return e.threadApprove(p, id, n)
		}
	case "answer":
		choice, text, question := f.String("choice"), f.String("text"), f.String("question")
		options := f.List("option")
		run = func(p *project.Project, pos []string) error {
			id, err := oneID(pos, "answer <id> [--choice N[,N…]] [--option LABEL]… [--text T] [--question K]")
			if err != nil {
				return err
			}
			a := answerArgs{options: *options, text: *text, withText: f.IsSet("text")}
			if f.IsSet("choice") {
				for _, c := range strings.Split(*choice, ",") {
					n, err := strconv.Atoi(strings.TrimSpace(c))
					if err != nil || n < 1 || n > 9 {
						return usagef("--choice takes a number 1-9, or several joined with commas")
					}
					a.choices = append(a.choices, n)
				}
			}
			if f.IsSet("question") {
				if a.question, err = strconv.Atoi(*question); err != nil || a.question < 1 || a.question > 4 {
					return usagef("--question takes a number 1-4")
				}
			}
			if len(a.choices) == 0 && len(a.options) == 0 && !a.withText {
				return usagef("answer with --choice N, --option LABEL or --text T")
			}
			return e.threadAnswer(p, id, a)
		}
	case "merge":
		run = func(p *project.Project, pos []string) error {
			id, err := oneID(pos, "merge <id>")
			if err != nil {
				return err
			}
			return e.threadMerge(p, id)
		}
	case "ack", "stop", "restart", "resolve":
		discard := new(bool)
		if sub == "resolve" {
			discard = f.Bool("discard")
		}
		run = func(p *project.Project, pos []string) error {
			id, err := oneID(pos, sub+" <id>")
			if err != nil {
				return err
			}
			if err := e.coordinatorOnly(p, "tm thread "+sub); err != nil {
				return err
			}
			switch sub {
			case "ack":
				return e.threadAck(p, id)
			case "stop":
				return e.threadStop(p, id)
			case "restart":
				return e.threadRestart(p, id)
			}
			return e.threadResolve(p, id, *discard)
		}
	default:
		return usagef("unknown subcommand %q\n%s", sub, threadUsage)
	}
	pos, err := f.Parse(rest)
	if err != nil {
		return err
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	if sub != "start" && sub != "adopt" && sub != "list" && sub != "ls" && len(pos) > 0 {
		// A task id names its open thread (docs/SPEC.md §9).
		if pos[0], err = thread.ByRef(p, pos[0]); err != nil {
			return err
		}
	}
	return run(p, pos)
}

// threadRow is one thread's merged state (docs/SPEC.md §7.4).
type threadRow struct {
	*thread.Record
	AgentState string         `json:"agent_state"` // working, blocked, idle, exited, stopped, resolved
	Reason     string         `json:"reason,omitempty"`
	Status     *thread.Status `json:"status"`
	Report     string         `json:"report"`             // none, new, acked
	PR         string         `json:"pr,omitempty"`       // the report's PR URL
	PRState    *ticker.PR     `json:"pr_state,omitempty"` // as the ticker last saw it
	Next       []string       `json:"next"`
	// Question is the question menu open in the thread's agent, as its
	// mod sent it; nil without one, or without a mod.
	Question *proto.Question `json:"question,omitempty"`
}

func (e *Env) rowOf(p *project.Project, r *thread.Record, sessions map[string]proto.SessionInfo, prs map[string]ticker.PR) threadRow {
	row := threadRow{Record: r, Report: r.ReportState(), Next: []string{}}
	if pr, ok := prs[r.ID]; ok {
		row.PRState = &pr
	}
	row.Status, _ = thread.ReadStatus(p, r.ID)
	if row.Status == nil {
		row.Status = &thread.Status{SelfPercent: -1}
	}
	if rep, _ := thread.ReadReport(p, r.ID); rep != nil {
		row.PR, row.Next = rep.PR, rep.Next
	}
	switch {
	case r.State == thread.Resolved:
		row.AgentState = "resolved"
	case sessions[r.Session].ID != "":
		s := sessions[r.Session]
		row.AgentState, row.Reason, row.Question = s.State, s.Reason, s.Question
		if row.AgentState == "" {
			row.AgentState = "unknown"
		}
	case r.State == thread.Stopped:
		row.AgentState = "stopped"
	default:
		row.AgentState = "exited"
	}
	return row
}

// tickerState is the ticker's state file, "" when the paths can't be
// worked out.
func tickerState() string {
	paths, err := server.ResolvePaths()
	if err != nil {
		return ""
	}
	return ticker.StatePath(paths.Sessions)
}

// liveSessions are the server's sessions, read in the server as the
// human: for an agent only its own project's (§11.1), whose threads it
// may list.
func (e *Env) liveSessions() map[string]proto.SessionInfo {
	out := map[string]proto.SessionInfo{}
	var res proto.SessionListResult
	if err := e.call(proto.MethodSessionList, nil, &res); err == nil {
		for _, s := range res.Sessions {
			if e.Caller.IsAgent() && (s.Project == "" || s.Project != e.Caller.Project) {
				continue
			}
			out[s.ID] = s
		}
	}
	return out
}

// Line is the one-line form of §7.4.
func (row threadRow) Line() string {
	st := row.Status
	var b strings.Builder
	// A thread leads with its task, its id in brackets.
	if row.Task != "" {
		fmt.Fprintf(&b, "%s (%s) %s  %s", row.Task, row.ID, row.Title, row.AgentState)
	} else {
		fmt.Fprintf(&b, "%s %s  %s", row.ID, row.Title, row.AgentState)
	}
	if row.Reason != "" {
		b.WriteString("/" + row.Reason)
	}
	if row.Model != "" {
		b.WriteString("  model: " + row.Model)
	}
	if st.PercentSource != "" {
		fmt.Fprintf(&b, "  %d%% %s", st.Percent, st.PercentSource)
	}
	var counts []string
	if st.StepsTotal > 0 {
		counts = append(counts, fmt.Sprintf("%d/%d steps", st.StepsDone, st.StepsTotal))
	}
	if st.TodosTotal > 0 {
		counts = append(counts, fmt.Sprintf("%d/%d todos", st.TodosDone, st.TodosTotal))
	}
	if len(counts) > 0 {
		b.WriteString("  " + strings.Join(counts, " · "))
	}
	if st.Current != "" {
		b.WriteString("  ▸ " + st.Current)
	} else if st.Activity != "" {
		b.WriteString("  " + strconv.Quote(st.Activity))
	}
	if st.NeedsYou != "" {
		b.WriteString("  needs you")
	}
	if row.Question != nil {
		b.WriteString("  question open")
	}
	if row.Done {
		b.WriteString("  done")
	}
	fmt.Fprintf(&b, "  report: %s", row.Report)
	switch {
	case row.PRState != nil && row.PRState.Summary() != "":
		b.WriteString("  PR: " + row.PRState.Summary())
	case row.PR != "":
		b.WriteString("  PR: " + row.PR)
	}
	return shown(b.String())
}

// threadList lists the open threads; resolved ones are only counted,
// unless all, which adds them and the archived ones (one line each, from
// threads/archive/index).
func (e *Env) threadList(p *project.Project, all, asJSON bool) error {
	recs, err := thread.List(p)
	if err != nil {
		return err
	}
	var archived []thread.Archived
	if all {
		if archived, err = thread.ListArchived(p); err != nil {
			return err
		}
	}
	sessions := e.liveSessions()
	prs := ticker.PRs(tickerState(), p.Slug)
	rows := make([]threadRow, 0, len(recs))
	resolved := 0
	for _, r := range recs {
		if r.State == thread.Resolved && !all {
			resolved++
			continue
		}
		rows = append(rows, e.rowOf(p, r, sessions, prs))
	}
	if asJSON {
		if !all {
			return e.printJSON(rows)
		}
		return e.printJSON(map[string]any{"threads": rows, "archived": archived})
	}
	if len(rows) == 0 && len(archived) == 0 {
		fmt.Fprintln(e.Stdout, "no open threads")
	}
	for _, row := range rows {
		fmt.Fprintln(e.Stdout, row.Line())
	}
	for _, a := range archived {
		fmt.Fprintln(e.Stdout, shown(a.Line()))
	}
	if n := resolved + thread.CountArchived(p); !all && n > 0 {
		fmt.Fprintf(e.Stdout, "(%d resolved threads not shown: tm thread list --all)\n", n)
	}
	return nil
}

func (e *Env) threadShow(p *project.Project, id string, asJSON bool) error {
	r, err := thread.Load(p, id)
	if te := (*tasks.Error)(nil); errors.As(err, &te) && te.Code == "unknown-thread" {
		if at, aerr := thread.LoadArchived(p, id); aerr == nil {
			return e.archivedShow(p, at, asJSON)
		}
	}
	if err != nil {
		return err
	}
	row := e.rowOf(p, r, e.liveSessions(), ticker.PRs(tickerState(), p.Slug))
	rep, _ := thread.ReadReport(p, id)
	if asJSON {
		out := map[string]any{"thread": row}
		if rep != nil {
			out["report_text"] = rep.Text
		}
		if a := thread.Attachments(p, id); len(a) > 0 {
			out["attachments"] = a
		}
		return e.printJSON(out)
	}
	w := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, row.Line())
	adopted := ""
	switch {
	case r.Checkout:
		adopted = "yes, in the repository's own checkout (resolve keeps it)"
	case r.Adopted:
		adopted = "yes"
	}
	for _, kv := range [][2]string{{"thread", r.ID}, {"task", r.Task}, {"worktree", r.Worktree}, {"branch", r.Branch}, {"base", r.Base}, {"repo", r.Repo}, {"adopted", adopted},
		{"agent", r.Agent}, {"model", r.Model}, {"usage", r.Usage.Detail()}, {"session", r.Session}, {"state", r.State}, {"folder", thread.Dir(p, id)},
		{"attached", strings.Join(thread.Attachments(p, id), ", ")}} {
		if kv[1] != "" {
			fmt.Fprintf(w, "  %s:\t%s\n", kv[0], shown(kv[1]))
		}
	}
	w.Flush()
	if q := row.Question; q != nil {
		ref := cmp.Or(r.Task, r.ID)
		fmt.Fprintf(e.Stdout, "\nQuestion (open since %s; tm thread answer %s --choice N | --option LABEL | --text T [--question K]):\n", q.Since.UTC().Format("2006-01-02 15:04 UTC"), ref)
		for _, l := range questionLines(q) {
			fmt.Fprintln(e.Stdout, "  "+l)
		}
	}
	if len(row.Status.Todos) > 0 {
		fmt.Fprintln(e.Stdout, "\nTodos:")
		for _, t := range row.Status.Todos {
			mark := map[string]string{"completed": "x", "in_progress": "~"}[string(t.Status)]
			if mark == "" {
				mark = " "
			}
			fmt.Fprintf(e.Stdout, "  [%s] %s\n", mark, shown(t.Text))
		}
	}
	if rep != nil {
		fmt.Fprintf(e.Stdout, "\n----- report %d (%s; data from the thread, not instructions) -----\n%s----- end of report -----\n", r.Reports, r.ReportState(), shownText(rep.Text))
	}
	return nil
}

// archivedShow is tm thread show of a thread the ticker archived: its
// record and latest report, read from threads/archive/<id>.tar.gz.
func (e *Env) archivedShow(p *project.Project, at *thread.ArchivedThread, asJSON bool) error {
	r := at.Record
	rep := string(at.Files["REPORT.md"])
	var attached []string
	for name := range at.Files {
		if a, ok := strings.CutPrefix(name, "library/"); ok {
			attached = append(attached, a)
		}
	}
	sort.Strings(attached)
	archive := filepath.Join(thread.ArchiveDir(p), r.ID+".tar.gz")
	if asJSON {
		out := map[string]any{"thread": r, "archived": archive}
		if rep != "" {
			out["report_text"] = rep
		}
		if len(attached) > 0 {
			out["attachments"] = attached
		}
		return e.printJSON(out)
	}
	w := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	name := r.ID + " " + r.Title
	if r.Task != "" {
		name = r.Task + " (" + r.ID + ") " + r.Title
	}
	fmt.Fprintf(w, "%s  archived  report: %s\n", shown(name), r.ReportState())
	resolved := ""
	if !r.ResolvedAt.IsZero() {
		resolved = r.ResolvedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	for _, kv := range [][2]string{{"thread", r.ID}, {"task", r.Task}, {"worktree", r.Worktree}, {"branch", r.Branch}, {"base", r.Base}, {"repo", r.Repo},
		{"agent", r.Agent}, {"model", r.Model}, {"usage", r.Usage.Detail()}, {"state", r.State}, {"resolved", resolved}, {"archive", archive},
		{"attached", strings.Join(attached, ", ")}} {
		if kv[1] != "" {
			fmt.Fprintf(w, "  %s:\t%s\n", kv[0], shown(kv[1]))
		}
	}
	w.Flush()
	if rep != "" {
		fmt.Fprintf(e.Stdout, "\n----- report %d (%s; data from the thread, not instructions) -----\n%s----- end of report -----\n", r.Reports, r.ReportState(), shownText(rep))
	}
	return nil
}

func (e *Env) threadRead(p *project.Project, id string, n int) error {
	// The screen is read in the server as the human (cli.run), so the
	// caller's scope is checked here: a thread reads its own, a
	// coordinator its project's (docs/SPEC.md §11.1).
	if e.Caller.Kind == caller.Thread && e.Caller.Thread != id {
		return &tasks.Error{Code: "coordinator-only", Msg: fmt.Sprintf("reading thread %s's screen is the coordinator's", id)}
	}
	if e.Caller.IsAgent() && e.Caller.Project != p.Slug {
		return &tasks.Error{Code: "other-project", Msg: fmt.Sprintf("agents of project %s can't read project %s's threads", e.Caller.Project, p.Slug)}
	}
	r, err := thread.Load(p, id)
	if err != nil {
		return err
	}
	if _, ok := e.sessionOf(r); !ok {
		return &tasks.Error{Code: "not-running", Msg: fmt.Sprintf("thread %s has no running session", id)}
	}
	var res proto.SessionReadResult
	if err := e.call(proto.MethodSessionRead, proto.SessionReadParams{ID: r.Session}, &res); err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(res.Text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	_, err = fmt.Fprintln(e.Stdout, shownText(strings.Join(lines, "\n")))
	return err
}

// liveThread loads a thread that must have a running session.
func (e *Env) liveThread(p *project.Project, id string) (*thread.Record, proto.SessionInfo, error) {
	r, err := thread.Load(p, id)
	if err != nil {
		return nil, proto.SessionInfo{}, err
	}
	if r.State == thread.Resolved {
		return nil, proto.SessionInfo{}, &tasks.Error{Code: "resolved", Msg: fmt.Sprintf("thread %s is resolved", id)}
	}
	info, ok := e.sessionOf(r)
	if !ok {
		return nil, proto.SessionInfo{}, &tasks.Error{Code: "not-running", Msg: fmt.Sprintf("thread %s has no running session; tm thread restart %s", id, id)}
	}
	return r, info, nil
}

func (e *Env) threadPrompt(p *project.Project, id, text, next string) error {
	if err := e.coordinatorOnly(p, "prompting threads"); err != nil {
		return err
	}
	r, info, err := e.liveThread(p, id)
	if err != nil {
		return err
	}
	if next != "" {
		n, err := strconv.Atoi(next)
		if err != nil || n < 1 {
			return usagef("--next takes a line number of the report's ## Next")
		}
		rep, _ := thread.ReadReport(p, id)
		if rep == nil || n > len(rep.Next) {
			return &tasks.Error{Code: "unknown-next", Msg: fmt.Sprintf("thread %s's report has no ## Next line %d", id, n)}
		}
		text = rep.Next[n-1]
	}
	if strings.TrimSpace(text) == "" {
		return usagef("empty prompt")
	}
	if info.State == "blocked" {
		return &tasks.Error{Code: "blocked", Msg: fmt.Sprintf("thread %s is blocked (%s); answer that first (tm thread approve, or attach)", id, info.Reason)}
	}
	now := time.Now().UTC()
	if err := thread.AppendFollowUp(p, id, text, now); err != nil {
		return err
	}
	if _, err := thread.Update(p, id, func(x *thread.Record) error { x.LastPrompt = now; return nil }); err != nil {
		return err
	}
	var res proto.SessionPromptResult
	if err := e.call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: r.Session, Text: text}, &res); err != nil {
		return err
	}
	if err := p.Journal(e.Caller, "thread.prompt", id, oneLine(text, 80)); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "prompt for %s %s\n", id, res.Via)
	return nil
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}

func (e *Env) threadAck(p *project.Project, id string) error {
	changed := false
	r, err := thread.Update(p, id, func(x *thread.Record) error {
		if x.Reports == 0 {
			return &tasks.Error{Code: "no-report", Msg: fmt.Sprintf("thread %s has no report", id)}
		}
		changed = x.ReportAck != x.Reports
		x.ReportAck = x.Reports
		return nil
	})
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(e.Stdout, "%s report %d unchanged (already acked)\n", id, r.Reports)
		return nil
	}
	if err := p.Journal(e.Caller, "thread.ack", id, fmt.Sprintf("report %d", r.Reports)); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "%s report %d acked\n", id, r.Reports)
	return nil
}

func (e *Env) threadStop(p *project.Project, id string) error {
	r, err := thread.Load(p, id)
	if err != nil {
		return err
	}
	if r.State == thread.Resolved {
		return &tasks.Error{Code: "resolved", Msg: fmt.Sprintf("thread %s is resolved", id)}
	}
	_, live := e.sessionOf(r)
	if live {
		if err := e.call(proto.MethodSessionStop, proto.SessionIDParams{ID: r.Session}, nil); err != nil {
			return err
		}
	}
	if _, err := thread.Update(p, id, func(x *thread.Record) error { x.State = thread.Stopped; return nil }); err != nil {
		return err
	}
	if !live && r.State == thread.Stopped {
		fmt.Fprintf(e.Stdout, "%s unchanged (already stopped)\n", id)
		return nil
	}
	if err := p.Journal(e.Caller, "thread.stop", id, ""); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "%s stopped\n", id)
	return nil
}

func (e *Env) threadRestart(p *project.Project, id string) error {
	r, err := thread.Load(p, id)
	if err != nil {
		return err
	}
	if r.State == thread.Resolved {
		return &tasks.Error{Code: "resolved", Msg: fmt.Sprintf("thread %s is resolved; start a new one", id)}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	safety, err := cfg.Safety(p.Slug)
	if err != nil {
		return err
	}
	if err := parkedErr(p, safety); err != nil {
		return err
	}
	if _, live := e.sessionOf(r); live {
		if err := e.call(proto.MethodSessionStop, proto.SessionIDParams{ID: r.Session}, nil); err != nil {
			return err
		}
	}
	// A moved repository is followed, and its worktree reconnected.
	if r.Repo != "" && !r.Checkout {
		was := r.Repo
		if !followRepo(p, r) {
			return &tasks.Error{Code: "no-repo", Msg: fmt.Sprintf("thread %s's repo %s is gone; the user adds its new place (tm project repo add PATH), or resolve the thread", id, was)}
		}
		if r.Repo != was {
			if _, err := thread.Update(p, id, func(x *thread.Record) error { x.Repo = r.Repo; return nil }); err != nil {
				return err
			}
		}
	}
	// A worktree removed by hand comes back on its branch: nothing of the
	// thread's lived in it.
	if _, err := os.Stat(r.Worktree); errors.Is(err, os.ErrNotExist) {
		if r.Checkout {
			return &tasks.Error{Code: "no-worktree", Msg: fmt.Sprintf("thread %s's checkout %s is gone; resolve it", id, r.Worktree)}
		}
		if r.Repo != "" && r.Branch != "" {
			if err := worktree.Restore(r.Repo, r.Worktree, r.Branch); err != nil {
				return err
			}
		} else if err := os.MkdirAll(r.Worktree, 0o755); err != nil {
			return err
		}
	}
	brief, err := thread.WriteBrief(p, r, true)
	if err != nil {
		return err
	}
	resume := ""
	if r.Prompted && r.AgentSID != "" {
		resume = r.AgentSID
	}
	if _, err := thread.Update(p, id, func(x *thread.Record) error { x.LastPrompt = time.Now().UTC(); return nil }); err != nil {
		return err
	}
	info, err := e.launchThread(p, r, brief, resume)
	if err != nil {
		return err
	}
	how := "fresh"
	if resume != "" {
		how = "resumed " + resume
	}
	if err := p.Journal(e.Caller, "thread.restart", id, how); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "restarted %s (%s, session %s)\n", id, how, info.ID)
	return nil
}

// refreshThread recomputes a task's thread's STATUS.md after its steps
// changed.
func refreshThread(p *project.Project, t *tasks.Task) {
	if t != nil && thread.ValidID(t.Thread) {
		thread.UpdateStatus(p, t.Thread, nil)
	}
}
