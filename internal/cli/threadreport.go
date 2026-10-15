package cli

// The thread's own commands: tm report, tm status and tm done.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// ownThread is the thread a report, status or done call is about: the
// caller's own, or --thread for the human (by hand, or in tests).
func (e *Env) ownThread(p *project.Project, flag string) (*thread.Record, error) {
	switch e.Caller.Kind {
	case caller.Thread:
		if flag != "" && flag != e.Caller.Thread {
			return nil, &tasks.Error{Code: "coordinator-only", Msg: "a thread reports only for itself"}
		}
		if e.Caller.Project != "" && e.Caller.Project != p.Slug {
			return nil, &tasks.Error{Code: "other-project", Msg: "a thread reports only to its own project"}
		}
		flag = e.Caller.Thread
	case caller.Coordinator:
		return nil, &tasks.Error{Code: "thread-only", Msg: "threads report; the coordinator reads reports with tm thread show"}
	default:
		if flag == "" {
			return nil, usagef("outside a thread, pass --thread <id>")
		}
	}
	r, err := thread.Load(p, flag)
	if err != nil {
		return nil, err
	}
	if r.State == thread.Resolved {
		return nil, &tasks.Error{Code: "resolved", Msg: fmt.Sprintf("thread %s is resolved", r.ID)}
	}
	return r, nil
}

const reportUsage = `usage: tm report [--file F] [--attach F]...   (the report on stdin, or from --file)
       tm report --show
Format: optional "PR: <url>" first line, "## Report", required "## Next"
(one action per line, at most 100 characters), optional "## Remember".`

func runReport(e *Env, args []string) error {
	f := newFlags()
	slug, id := f.String("project"), f.String("thread")
	file, attach, show := f.String("file"), f.List("attach"), f.Bool("show")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*show && f.anySet("file", "attach")) {
		return usagef("%s", reportUsage)
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	if *show {
		tid := *id
		if e.Caller.Kind == caller.Thread {
			tid = e.Caller.Thread
		}
		if tid == "" {
			return usagef("outside a thread, pass --thread <id>")
		}
		if _, err := thread.Load(p, tid); err != nil {
			return err
		}
		data, err := os.ReadFile(thread.Path(p, tid, "REPORT.md"))
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(e.Stdout, "%s has no report yet\n", tid)
			return nil
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(e.Stdout, shownText(string(data)))
		return err
	}
	r, err := e.ownThread(p, *id)
	if err != nil {
		return err
	}
	src := "-"
	if *file != "" {
		src = *file
	}
	text, err := e.readArg(src)
	if err != nil {
		return err
	}
	var files []string
	for _, a := range *attach {
		files = append(files, e.abs(a))
	}
	n, err := thread.StoreReportFrom(p, r.ID, text, files, func(a string) (*os.File, error) { return e.openArg(a) }, time.Now())
	if err != nil {
		return err
	}
	// One item per report: a newer one supersedes the thread's older
	// unhandled report items.
	if err := p.DoneItems("report", r.ID); err != nil {
		return err
	}
	summary := fmt.Sprintf("%s handed in report %d", thread.Label(p, r.ID), n)
	if _, err := p.AddItem("report", r.ID, summary, false); err != nil {
		return err
	}
	if err := p.Journal(e.Caller, "thread.report", r.ID, fmt.Sprintf("report %d", n)); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "stored report %d for %s\n", n, r.ID)
	return nil
}

const statusUsage = `usage: tm status --percent N --activity "…" [--needs-you "question"]
       tm status --unknown [--activity "…"] [--needs-you "question"]
       tm status --needs-you "question"`

// runStatus is a thread's self-report (docs/SPEC.md §7.3).
func runStatus(e *Env, args []string) error {
	f := newFlags()
	slug, id := f.String("project"), f.String("thread")
	percent, activity, needsYou, unknown := f.String("percent"), f.String("activity"), f.String("needs-you"), f.Bool("unknown")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || !f.anySet("percent", "activity", "needs-you", "unknown") || (f.IsSet("percent") && *unknown) {
		return usagef("%s", statusUsage)
	}
	pct := -1
	if f.IsSet("percent") {
		if pct, err = strconv.Atoi(strings.TrimSuffix(*percent, "%")); err != nil || pct < 0 || pct > 100 {
			return usagef("--percent takes 0-100")
		}
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	r, err := e.ownThread(p, *id)
	if err != nil {
		return err
	}
	wasWaiting := false
	st, err := thread.UpdateStatus(p, r.ID, func(st *thread.Status) error {
		wasWaiting = st.NeedsYou != ""
		if f.anySet("percent", "unknown") {
			st.SelfPercent = pct
		}
		st.SelfAt = time.Now().UTC()
		st.Activity = oneLine(*activity, 100)
		st.NeedsYou = oneLine(*needsYou, 300)
		if st.NeedsYou == "" && strings.EqualFold(st.Activity, "waiting for you") {
			st.NeedsYou = st.Activity
		}
		return nil
	})
	if err != nil {
		return err
	}
	if st.NeedsYou != "" && !wasWaiting {
		// The question itself is the thread's text: data, kept out of
		// the item's summary.
		if _, err := p.AddItem("needs-you", r.ID, thread.Label(p, r.ID)+" is waiting for the user (tm thread show "+r.ID+")", true); err != nil {
			return err
		}
	}
	fmt.Fprintf(e.Stdout, "%s: %d%% %s\n", r.ID, st.Percent, orDash(st.PercentSource))
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// runDone is a thread's "finished" (docs/SPEC.md §7.3): it needs a report
// stored since the last prompt. The task's status stays the coordinator's.
func runDone(e *Env, args []string) error {
	f := newFlags()
	slug, id := f.String("project"), f.String("thread")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usagef("usage: tm done [\"summary\"]")
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	r, err := e.ownThread(p, *id)
	if err != nil {
		return err
	}
	if r.Reports == 0 || r.ReportAt.Before(r.LastPrompt) {
		return &tasks.Error{Code: "no-report", Msg: "hand in your report with tm report first (none since your last prompt)"}
	}
	if r.Done && !r.DoneAt.Before(r.ReportAt) {
		fmt.Fprintf(e.Stdout, "%s unchanged (already done)\n", r.ID)
		return nil
	}
	now := time.Now().UTC()
	if _, err := thread.Update(p, r.ID, func(x *thread.Record) error { x.Done, x.DoneAt = true, now; return nil }); err != nil {
		return err
	}
	if _, err := thread.UpdateStatus(p, r.ID, func(st *thread.Status) error { st.Done = true; return nil }); err != nil {
		return err
	}
	detail := ""
	if len(pos) == 1 {
		detail = oneLine(pos[0], 80)
	}
	if err := p.Journal(e.Caller, "thread.done", r.ID, detail); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "%s done\n", r.ID)
	return nil
}
