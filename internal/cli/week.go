package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/history"
)

const weekUsage = `usage: tm week [--weeks N] [--project <slug>] [--json]

A stretch's numbers, this calendar week so far, or with --weeks N this one
and the N-1 before it: tasks done per day, the designs touched, the tasks
dropped and still open, and each repo's releases with its own range, never
added up, in this machine's time zone. What a project hasn't got is left
out. A stretch where nothing finished says so and gives the last finish
date. It names no task or thread; the week-in-review task template
(tm task add "week 45" --template week-in-review) has a page written
from it.`

// barWidth is the longest day's bar.
const barWidth = 24

// runWeek is tm week (docs/SPEC.md §5.1, Week). Like tm history, forwarded
// it runs in the server, and with no server it reads the records itself.
func runWeek(e *Env, args []string) error {
	if len(args) > 0 { // bare, it answers for the project it is run in
		if done, err := helpOr(e, args, weekUsage); done {
			return err
		}
	}
	f := newFlags()
	slug, weeksArg, asJSON := f.String("project"), f.String("weeks"), f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return usagef("%v\n%s", err, weekUsage)
	}
	if len(pos) != 0 {
		return usagef("%s", weekUsage)
	}
	weeks := 1
	if f.IsSet("weeks") {
		weeks, err = strconv.Atoi(*weeksArg)
		if err != nil || weeks < 1 || weeks > history.MaxWeeks {
			return usagef("--weeks takes a number from 1 to %d\n%s", history.MaxWeeks, weekUsage)
		}
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	w, err := history.ReadWeek(p, weeks)
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(w)
	}
	printWeek(e, w)
	return nil
}

func printWeek(e *Env, w history.Week) {
	from, _ := time.Parse(time.DateOnly, w.From)
	to, _ := time.Parse(time.DateOnly, w.To)
	heading := fmt.Sprintf("WEEK %d", w.ToWeek)
	if w.Weeks > 1 {
		heading = fmt.Sprintf("WEEKS %d–%d", w.FromWeek, w.ToWeek)
	}
	heading += "  " + spanWords(from, to)
	if w.Done > 0 {
		heading += fmt.Sprintf(", so far %d done", w.Done)
	}
	fmt.Fprintln(e.Stdout, heading)
	fmt.Fprintln(e.Stdout)
	if w.Done > 0 {
		printDays(e, w, from, to)
	} else {
		fmt.Fprintln(e.Stdout, nothingFinished(w.LastFinish, to))
	}
	if len(w.Designs) > 0 {
		fmt.Fprintln(e.Stdout, "\nDesigns")
		for _, d := range w.Designs {
			name := d.Title
			if name == "" {
				name = d.Design
			}
			fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("  %s · %d done, %d open", name, d.Done, d.Open)))
		}
	}
	if len(w.Releases) > 0 {
		fmt.Fprintln(e.Stdout, "\nReleases")
		for _, r := range w.Releases {
			fmt.Fprintln(e.Stdout, shown("  "+releaseRange(r)))
		}
	}
	fmt.Fprintln(e.Stdout)
	if w.Dropped > 0 {
		fmt.Fprintf(e.Stdout, "Dropped %s\n", taskCount(w.Dropped))
	}
	fmt.Fprintf(e.Stdout, "Still open %s\n", taskCount(w.StillOpen))
	if w.Done == 0 && w.Weeks == 1 {
		fmt.Fprintln(e.Stdout, "\ntm week --weeks 4 covers the last four weeks")
	}
}

// printDays draws the days with work, by weekday in one week, with their
// dates in more, a bar and the count.
func printDays(e *Env, w history.Week, from, to time.Time) {
	layout := "Mon"
	if w.Weeks > 1 {
		layout = "Mon 2 Jan"
		if from.Year() != to.Year() {
			layout = "Mon 2 Jan 2006"
		}
	}
	most, width := 0, 0
	labels := make([]string, len(w.Days))
	for i, d := range w.Days {
		most = max(most, d.Done)
		day, _ := time.Parse(time.DateOnly, d.Day)
		labels[i] = day.Format(layout)
		width = max(width, len(labels[i]))
	}
	for i, d := range w.Days {
		bar := strings.Repeat("█", max(d.Done*barWidth/most, 1))
		fmt.Fprintf(e.Stdout, "%-*s %s %d\n", width, labels[i], bar, d.Done)
	}
}

// nothingFinished says a stretch finished nothing, and when a task last
// did, the year only if it isn't to's.
func nothingFinished(last string, to time.Time) string {
	day, err := time.Parse(time.DateOnly, last)
	if err != nil {
		return "Nothing finished yet."
	}
	layout := "2 Jan"
	if day.Year() != to.Year() {
		layout = "2 Jan 2006"
	}
	return "Nothing finished. The last task finished on " + day.Format(layout) + "."
}

// spanWords says from to to with what they don't share: "Mon 2 – Fri 6
// Nov", "Mon 26 Oct – Fri 6 Nov", "Mon 28 Dec 2026 – Fri 8 Jan 2027".
func spanWords(from, to time.Time) string {
	switch {
	case from.Equal(to):
		return to.Format("Mon 2 Jan")
	case from.Year() != to.Year():
		return from.Format("Mon 2 Jan 2006") + " – " + to.Format("Mon 2 Jan 2006")
	case from.Month() != to.Month():
		return from.Format("Mon 2 Jan") + " – " + to.Format("Mon 2 Jan")
	}
	return from.Format("Mon 2") + " – " + to.Format("Mon 2 Jan")
}

// releaseRange is a repo's line: "terminatr 14 0.73.1 → 0.82.1", "mono
// api 1 1.2".
func releaseRange(r history.WeekRelease) string {
	s := strings.TrimSpace(r.Repo + " " + r.Part)
	s += fmt.Sprintf(" %d %s", r.Count, r.First)
	if r.Last != r.First {
		s += " → " + r.Last
	}
	return s
}

func taskCount(n int) string {
	if n == 1 {
		return "1 task"
	}
	return fmt.Sprintf("%d tasks", n)
}
