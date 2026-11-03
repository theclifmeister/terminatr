package cli

import (
	"fmt"
	"slices"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
)

// tm program recommend (T982): the director recommends an answer to an
// open question of the program's Needs you, a member coordinator's or
// the program's own, with a one-line reason. It never answers: the user
// accepts it with one key or tap, or picks another.

const programRecommendUsage = `usage: tm program recommend <question> --option N|LABEL… --why "…"   (the director's)
       tm program recommend <question> --text "…" --why "…"   (an answer in words)
       tm program recommend clear <question>   (take it back)
       tm program recommend list [--json]
       tm program recommend accept <question>   (the user's: answers with it, as theirs)
 [--program <slug>] on each, for the human.

<question> is an open question of the program's Needs you: acme-api/Q3 (a
member coordinator's), mini/acme-ios/Q3 (a paired member's) or Q2 (the
program's own, merged). The user sees "director recommends: <answer> — <why>"
on the row and accepts it with one key or tap, or picks another. A
recommendation is never an answer, and is dropped once its question is
answered, closed or asked again in other words. Say why in one line, with
the facts and where they come from (a task, a decision, a report); never
press.`

func programRecommend(e *Env, cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return usagef("%s", programRecommendUsage)
	}
	f := newFlags()
	slug, asJSON := f.String("program"), f.Bool("json")
	choices, text, why := f.List("option"), f.String("text"), f.String("why")
	sub := args[0]
	rest := args[1:]
	if !slices.Contains([]string{"clear", "list", "ls", "accept"}, sub) {
		sub, rest = "", args
	}
	pos, err := f.Parse(rest)
	if err != nil {
		return err
	}
	if sub != "" && f.anySet("option", "text", "why") {
		return usagef("%s", programRecommendUsage)
	}
	g, err := e.ownProgram(cfg, *slug)
	if err != nil {
		return err
	}
	switch sub {
	case "":
		if len(pos) != 1 {
			return usagef("%s", programRecommendUsage)
		}
		key, r, err := g.Recommend(e.Caller, cfg, program.RecommendRequest{Question: pos[0], Choices: *choices, Text: *text, Why: *why})
		if err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(map[string]any{"question": key, "recommendation": r})
		}
		fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("recommended on %s: %s — %s (the user accepts it or picks another)", key, r.Words(), r.Why)))
		return nil
	case "clear":
		if len(pos) != 1 {
			return usagef("%s", programRecommendUsage)
		}
		key, err := g.Unrecommend(e.Caller, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(e.Stdout, shown("cleared "+key))
		return nil
	case "list", "ls":
		if len(pos) != 0 {
			return usagef("%s", programRecommendUsage)
		}
		_, _ = g.Prune(cfg)
		recs, err := g.Recommendations()
		if err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(recs)
		}
		keys := make([]string, 0, len(recs))
		for k := range recs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s: %s — %s", k, recs[k].Words(), recs[k].Why)))
		}
		return nil
	case "accept":
		if len(pos) != 1 {
			return usagef("%s", programRecommendUsage)
		}
		res, err := g.Accept(e.Caller, cfg, pos[0])
		if err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(res)
		}
		line := fmt.Sprintf("answered %s: %s", res.Key, res.Answer)
		if res.Result != "" {
			line += " (" + res.Result + ")"
		}
		fmt.Fprintln(e.Stdout, shown(line))
		for _, fn := range res.Fanned {
			l := fmt.Sprintf("  %s/%s %s", fn.Project, fn.Question, fn.Result)
			if fn.Error != "" {
				l += ": " + fn.Error
			}
			fmt.Fprintln(e.Stdout, shown(l))
		}
		return nil
	}
	return usagef("%s", programRecommendUsage)
}
