package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

const askUsage = `usage: tm ask <command> [--project <slug>] [--json]

  add "question" [--task T12] [--header H] --option "Label[: description]"... [--multi] [--recommend LABEL]
  add --file F|-                one question, or an array of them, as JSON:
                                {"task","header","question","options":[{"label","description"}],
                                 "multi_select","recommended"}
  list                          the open questions, oldest first
  done Q3...                    answered, or no longer asked
  open                          have the coordinator put them to the user in its question dialog
  answer Q3 --choice N|LABEL... [--other "…"]
                                the user's: answer a question (N counts its options from 1; --choice
                                again for a multi-select one; --other in your own words)

Questions take 2 to 4 options (the dialog adds Other) and a header of at most 12 characters (default: the task).
Exit codes: 0 done, 1 refused, 2 usage, 3 I/O.`

// runAsk is the coordinator's question store (docs/SPEC.md §7.5,
// Questions): the questions it waits on the user for, which the band in
// its pane, or a dashboard key, has it open in its question dialog.
func runAsk(e *Env, args []string) error {
	if done, err := helpOr(e, args, askUsage); done {
		return err
	}
	sub := args[0]
	f := newFlags()
	slug, asJSON := f.String("project"), f.Bool("json")
	task, header, file, recommend := f.String("task"), f.String("header"), f.String("file"), f.String("recommend")
	options, multi := f.List("option"), f.Bool("multi")
	choices, other := f.List("choice"), f.String("other")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	if sub == "answer" {
		return askAnswer(e, *slug, pos, *choices, *other, *asJSON)
	}
	if f.anySet("choice", "other") {
		return usagef("--choice and --other go with answer\n%s", askUsage)
	}
	if e.Caller.Kind == caller.Thread {
		return &project.Error{Code: "coordinator-only", Msg: "questions to the user are the coordinator's; a thread uses tm status --needs-you"}
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	if sub != "list" && sub != "ls" {
		// Reading another project is allowed; changing it, or prompting
		// its coordinator (questions.open, as the human), is not.
		if err := e.coordinatorOnly(p, "tm ask "+sub); err != nil {
			return err
		}
	}
	switch sub {
	case "add":
		var qs []project.Question
		if *file != "" {
			if len(pos) > 0 || f.anySet("task", "header", "option", "multi", "recommend") {
				return usagef("add takes --file or a question with flags, not both")
			}
			if qs, err = e.readQuestions(*file); err != nil {
				return err
			}
		} else {
			if len(pos) != 1 {
				return usagef("%s", askUsage)
			}
			q := project.Question{Task: *task, Header: *header, Question: pos[0], MultiSelect: *multi, Recommended: *recommend}
			for _, o := range *options {
				label, desc, _ := strings.Cut(o, ": ")
				q.Options = append(q.Options, project.QuestionOption{Label: label, Description: desc})
			}
			qs = []project.Question{q}
		}
		// Check them all first: a bad one in an array saves none.
		for i := range qs {
			if err := qs[i].Check(); err != nil {
				return err
			}
		}
		var added []project.Question
		for _, q := range qs {
			got, err := p.AddQuestion(q)
			if err != nil {
				return err
			}
			added = append(added, got)
			if err := p.Journal(e.Caller, "ask.add", got.ID, textsafe.Line(got.Question, 80)); err != nil {
				return err
			}
		}
		if *asJSON {
			return e.printJSON(added)
		}
		for _, q := range added {
			fmt.Fprintf(e.Stdout, "asked %s\n", q.ID)
		}
		return nil
	case "list", "ls":
		if len(pos) != 0 {
			return usagef("%s", askUsage)
		}
		qs, err := p.Questions()
		if err != nil {
			return err
		}
		if *asJSON {
			if qs == nil {
				qs = []project.Question{}
			}
			return e.printJSON(qs)
		}
		for _, q := range qs {
			fmt.Fprintln(e.Stdout, shown(project.QuestionLine(q)))
		}
		return nil
	case "done":
		if len(pos) == 0 {
			return usagef("%s", askUsage)
		}
		var failed error
		for _, ref := range pos {
			q, err := p.DoneQuestion(ref)
			if err != nil {
				fmt.Fprintf(e.Stderr, "tm ask: %v\n", err)
				failed = &exitError{ExitRefused}
				continue
			}
			if err := p.Journal(e.Caller, "ask.done", q.ID, ""); err != nil {
				return err
			}
			fmt.Fprintf(e.Stdout, "done %s\n", q.ID)
		}
		return failed
	case "open":
		if len(pos) != 0 {
			return usagef("%s", askUsage)
		}
		var res proto.QuestionsOpenResult
		if err := e.call(proto.MethodQuestionsOpen, proto.QuestionsOpenParams{Project: p.Slug}, &res); err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(res)
		}
		fmt.Fprintf(e.Stdout, "%s: asked the coordinator (%s) to open them (%s)\n", project.QuestionsWaiting(res.Open), res.Session, res.Via)
		return nil
	}
	return usagef("unknown subcommand %q\n%s", sub, askUsage)
}

// readQuestions reads one question, or an array of them, as JSON from
// path ("-" for stdin).
func (e *Env) readQuestions(path string) ([]project.Question, error) {
	text, err := e.readArg(path)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	var qs []project.Question
	if strings.HasPrefix(text, "[") {
		err = json.Unmarshal([]byte(text), &qs)
	} else {
		var q project.Question
		err = json.Unmarshal([]byte(text), &q)
		qs = []project.Question{q}
	}
	if err != nil {
		return nil, usagef("--file: %v", err)
	}
	if len(qs) == 0 {
		return nil, usagef("--file holds no question")
	}
	return qs, nil
}

// askAnswer is tm ask answer: the user answers an open question from
// their own terminal (or the Obsidian plugin, which runs it), as in the
// coordinator's question dialog. It drops an answer item, the user's
// word, for the coordinator to act on (docs/SPEC.md §7.5, Questions).
func askAnswer(e *Env, slug string, pos, choices []string, other string, asJSON bool) error {
	if len(pos) != 1 {
		return usagef("%s", askUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "only the user answers a question (the coordinator's question dialog, or tm ask answer in their own terminal); ask them in chat"}
	}
	p, err := e.openProject(slug)
	if err != nil {
		return err
	}
	q, a, asked, err := p.AnswerQuestion(e.Caller, pos[0], choices, other)
	if err != nil {
		return err
	}
	if asJSON {
		return e.printJSON(map[string]any{"question": q.ID, "answer": a, "asked": asked})
	}
	if !asked {
		fmt.Fprintf(e.Stdout, "%s is answered already; it waits on the coordinator\n", q.ID)
		return nil
	}
	fmt.Fprintf(e.Stdout, "told the coordinator your answer to %s: %s\n", q.ID, shown(a.Words()))
	return nil
}
