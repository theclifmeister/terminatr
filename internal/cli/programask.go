package cli

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
)

// tm program ask (the programs design §7.1, §7.2): the program's own
// questions, each merged from member coordinators' questions that ask
// the same thing. The director merges and withdraws them; only the user
// answers them, once for every member question they cover.

const programAskUsage = `usage: tm program ask merge Q3@acme-api Q7@acme-web… [--text "…"] [--header H] [--map acme-web/Q7=2,1]
       tm program ask list [--json]
       tm program ask done Q2   (no longer asked: the member questions are their own again)
       tm program ask answer Q2 --choice N|LABEL… [--other "…"]   (the user's; as tm ask answer <program>/Q2)
 [--program <slug>] on each, for the human.

A merge makes one program question out of member coordinators' open questions
that ask the same thing: it takes the first one's options, and each other
one's must match them by label, or --map says which of its options each of
the first's is (1-based, in the first's order). tm program needs flags alike
questions (similar: Q7@acme-web); nothing merges them by itself. The user's
answer goes to every question it covers as their own answer there.`

func programAsk(e *Env, cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return usagef("%s", programAskUsage)
	}
	sub := args[0]
	f := newFlags()
	slug, asJSON := f.String("program"), f.Bool("json")
	text, header, maps := f.String("text"), f.String("header"), f.List("map")
	choices, other := f.List("choice"), f.String("other")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	if sub != "merge" && f.anySet("text", "header", "map") || sub != "answer" && f.anySet("choice", "other") {
		return usagef("%s", programAskUsage)
	}
	g, err := e.ownProgram(cfg, *slug)
	if err != nil {
		return err
	}
	switch sub {
	case "merge":
		req := program.MergeRequest{Questions: pos, Text: *text, Header: *header, Map: map[string]string{}}
		for _, m := range *maps {
			k, v, ok := strings.Cut(m, "=")
			if !ok {
				return usagef("--map takes <project>/Q<n>=2,1,…")
			}
			req.Map[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		q, err := g.Merge(e.Caller, cfg, req)
		if err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(q)
		}
		fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("merged into %s/%s: %s", g.Slug, q.ID, project.QuestionLine(q))))
		return nil
	case "list", "ls":
		if len(pos) != 0 {
			return usagef("%s", programAskUsage)
		}
		qs, err := g.Questions()
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
			return usagef("%s", programAskUsage)
		}
		var failed error
		for _, ref := range pos {
			q, err := g.Withdraw(e.Caller, cfg, ref)
			if err != nil {
				fmt.Fprintf(e.Stderr, "tm program ask: %v\n", err)
				failed = &exitError{ExitRefused}
				continue
			}
			fmt.Fprintf(e.Stdout, "done %s/%s\n", g.Slug, q.ID)
		}
		return failed
	case "answer":
		if len(pos) != 1 {
			return usagef("%s", programAskUsage)
		}
		return programAnswer(e, cfg, g, pos[0], *choices, *other, *asJSON)
	}
	return usagef("unknown subcommand %q\n%s", sub, programAskUsage)
}

// programAnswer is the user's answer to program question ref of g, from
// tm program ask answer or tm ask answer <program>/Q2.
func programAnswer(e *Env, cfg *config.Config, g *program.Program, ref string, choices []string, other string, asJSON bool) error {
	res, err := g.Answer(e.Caller, cfg, ref, choices, other)
	if err != nil && len(res.Fanned) == 0 {
		return err
	}
	if asJSON {
		if perr := e.printJSON(res); perr != nil {
			return perr
		}
		return err
	}
	fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("answered %s/%s: %s", g.Slug, res.Question, res.Answer.Words())))
	for _, f := range res.Fanned {
		line := fmt.Sprintf("  %s/%s %s", f.Project, f.Question, f.Result)
		if f.Error != "" {
			line += ": " + f.Error
		}
		fmt.Fprintln(e.Stdout, shown(line))
	}
	return err
}

// askProgramAnswer is tm ask answer <program>/Q2: the user answers a
// program's question as tm program ask answer does.
func askProgramAnswer(e *Env, ref string, choices []string, other string, asJSON bool) (bool, error) {
	slug, id, ok := strings.Cut(ref, "/")
	if !ok || !config.ValidProgramSlug(slug) {
		return false, nil
	}
	cfg, _ := config.Load()
	if _, isProgram := cfg.Program(slug); !isProgram {
		return false, nil
	}
	if !features.Programs(cfg) {
		return true, programsOff()
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return true, err
	}
	return true, programAnswer(e, cfg, g, id, choices, other, asJSON)
}
