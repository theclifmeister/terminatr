package cli

import (
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

const accessUsage = `usage: tm access show [<project>] [--thread <id>] [--guides] [--json]
       tm access explain [<project>] [--thread <id>] [--json] -- <command line>
       tm access preset list [--json]
       tm access preset show <name> [--json]
       tm access add <kind> <value> [<project> | --all] [--without-asking]
       tm access add guide <title> --text <text> | --file <file> [<project> | --all] [--replace]
       tm access rm <kind> <value> [<project> | --all]
       tm access edit <kind> <old value> <new value> [<project> | --all] [--without-asking | --asks-first] [--text <text> | --file <file>]
       tm access drop|undrop <value> [<project>]
       tm access set local-ports|escapes|yolo on|off [<project> | --all] [--yes]
       tm access set protection open|guarded|sandboxed|strict [<project> | --all] [--yes]
       tm access preset add <name> [<project> | --all]

show lists a thread's access as rules: where each acts, the rule, and
where it comes from (tm, all projects, the project, tm's default, or the
agent's own settings file, shown only). Without --thread, what a new
thread of the project gets; with it, what that thread launched with,
RESTART TO APPLY marking what config.toml changed since; --guides
prints each guide's text after the table, as the brief gets it. A
thread may show only its own access (its --thread is the default).

explain says whether a thread of the project runs the command inside or
outside the sandbox, whether it asks first, and the guard's verdict.

add and rm change a sandbox list of the project's table (the one here
when none is named) or, with --all, all projects': kinds write (a write
folder), host, guide (a text tm adds after a thread's brief: its title,
and its text from --text or --file, - for stdin; --replace changes the
text of one the table has; rm and drop name it by its title), socket (a
unix socket sandboxed commands may
connect to), excluded (an excluded command, asking first;
--without-asking runs it without asking), approved (a command a thread
may ask tm to run outside its sandbox: tm run) and allow (a permission
rule for a tool that neither runs commands nor writes files). edit
changes an entry where it is, checked as add checks a new one: its
value (the same value to change only its options), an excluded
command's --without-asking or --asks-first (else it keeps its own), a
guide's title and, with --text or --file, its text. Editing all
projects' entry carries each project's drop of it along. A project's
lists add to all projects'; drop names one of all projects' entries its
threads don't take, undrop takes it back. set protection picks the
level agents run under, each adding to the one before: open (the
agent's own prompts, no thread sandbox, the guard's floor only),
guarded (the guard's rules), sandboxed (the thread sandbox, escapes ask
you) or strict (no escapes); open asks first. set turns local ports or
yolo on or off, and escapes off (strict) or on (sandboxed); yolo on asks
first (--yes without a terminal), as it turns the sandbox off. Each is journaled, and threads
get it from their next start or restart. These are the human's.

A preset is a named group of rules tm ships. preset show lists the rules
it adds; preset add adds those the project's threads (or, with --all,
every project's) don't have yet to its table in config.toml, as plain
rules: each then stands on its own, to change or remove in the Access
tab. Presets are the human's.`

func init() {
	commands["access"] = accessCmd
}

// accessCmd implements `tm access …` (T574 §5): show and explain read,
// the others write config.toml (accessedit.go, accesspreset.go).
func accessCmd(e *Env, args []string) error {
	if len(args) == 0 {
		return srvUsage("access", accessUsage)
	}
	switch args[0] {
	case "show":
		return accessShow(e, args[1:])
	case "explain":
		return accessExplain(e, args[1:])
	case "preset":
		return accessPreset(e, args[1:])
	case "add", "rm":
		return accessAdd(e, args[0], args[1:])
	case "edit":
		return accessEdit(e, args[1:])
	case "drop", "undrop":
		return accessDrop(e, args[0], args[1:])
	case "set":
		return accessSet(e, args[1:])
	}
	return srvUsage("access", accessUsage)
}

// accessTarget is the project and thread an access command names:
// pos's project, else the one here; a thread caller's own thread when
// it names none.
func (e *Env) accessTarget(cmd string, pos []string, id string) (slug, thread string, err error) {
	if len(pos) > 1 {
		return "", "", srvUsage(cmd, accessUsage)
	}
	flag := ""
	if len(pos) == 1 {
		flag = pos[0]
	}
	slug, err = project.Resolve(flag, e.Getenv, e.Cwd)
	if err != nil {
		return "", "", srvFail(cmd, err)
	}
	if slug == "" {
		return "", "", srvUsage(cmd, "no project here; name one (tm project list)")
	}
	if err := e.ownProject(slug); err != nil {
		return "", "", srvFail(cmd, err)
	}
	if id == "" && e.Caller.Kind == caller.Thread {
		id = e.Caller.Thread
	}
	return slug, id, nil
}

func accessShow(e *Env, args []string) error {
	f := newFlags()
	id, asJSON, guides := f.String("thread"), f.Bool("json"), f.Bool("guides")
	pos, err := f.Parse(args)
	if err != nil {
		return srvFail("access show", err)
	}
	slug, tid, err := e.accessTarget("access show", pos, *id)
	if err != nil {
		return err
	}
	c, _, err := connect(false)
	if err != nil {
		return srvFail("access show", err)
	}
	defer c.Close()
	var res proto.AccessShowResult
	if err := c.Call(proto.MethodAccessShow, proto.AccessShowParams{Project: slug, Thread: tid}, &res); err != nil {
		return srvFail("access show", err)
	}
	if *asJSON {
		return e.printJSON(res)
	}
	printAccess(e, res)
	if *guides {
		printGuides(e, res)
	}
	return nil
}

// printGuides prints the texts of the guides a set gives, as the brief
// gets them (tm 0.38's names, worded at launch, by name only).
func printGuides(e *Env, res proto.AccessShowResult) {
	var gs []config.Guide
	for _, r := range res.Rules {
		switch {
		case r.Kind != proto.AccessGuide || !r.Given() || r.Change == proto.AccessRemoved:
		case r.Body == "":
			fmt.Fprintf(e.Stdout, "\n%s: tm 0.38's guidance, worded at launch (the server writes it out when it starts)\n", oneLineText(r.Value))
		default:
			gs = append(gs, config.Guide{Title: r.Value, Body: textsafe.Text(r.Body)})
		}
	}
	if len(gs) > 0 {
		fmt.Fprint(e.Stdout, "\n"+config.GuideText(gs))
	}
}

// printAccess prints a set as the effective access view's table.
func printAccess(e *Env, res proto.AccessShowResult) {
	fmt.Fprintln(e.Stdout, accessTitle(res))
	if res.SandboxError != "" {
		fmt.Fprintf(e.Stdout, "sandbox rules left out: %s\n", oneLineText(res.SandboxError))
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHERE\tRULE\tFROM\t")
	for _, r := range res.Rules {
		from := r.Source
		if r.Note != "" {
			from += " · " + r.Note
		}
		from = oneLineText(from)
		where := r.Where
		if r.Change == proto.AccessRemoved || !r.Given() {
			where += " ─"
		}
		fmt.Fprintf(tw, "%s\t%s %s\t%s\t%s\n", where, r.Kind, oneLineText(r.Value), from, oneLineText(strings.Join(r.Remarks(), "; ")))
	}
	tw.Flush()
	switch {
	case res.Thread != "" && !res.Recorded:
		fmt.Fprintf(e.Stdout, "%s has no launch record (it launched before tm kept one, or hasn't launched): this is what it would get now\n", res.Thread)
	case res.Changes == 1:
		fmt.Fprintf(e.Stdout, "1 change since launch: tm thread restart %s applies it\n", res.Thread)
	case res.Changes > 1:
		fmt.Fprintf(e.Stdout, "%d changes since launch: tm thread restart %s applies them\n", res.Changes, res.Thread)
	}
}

// accessTitle is the view's first line: whose access, the agent and
// platform, and when it launched.
func accessTitle(res proto.AccessShowResult) string {
	parts := []string{"Effective access", res.Project}
	if res.Thread != "" {
		parts = append(parts, res.Thread)
	} else {
		parts = append(parts, "a new thread")
	}
	var on []string
	if res.Agent != "" {
		on = append(on, res.Agent)
	}
	on = append(on, config.OSName(res.OS))
	t := strings.Join(parts, " · ") + " (" + strings.Join(on, ", ") + ")"
	if res.Recorded && !res.Launched.IsZero() {
		t += " · launched " + res.Launched.Local().Format("2006-01-02 15:04")
	}
	return t
}

// oneLineText is s on one line, for a table cell, without what a
// terminal would act on: a rule's text may come from a file a thread
// writes (its worktree's agent settings).
func oneLineText(s string) string {
	return strings.Join(strings.Fields(textsafe.Clean(s, false)), " ")
}

func accessExplain(e *Env, args []string) error {
	i := slices.Index(args, "--")
	if i < 0 || i == len(args)-1 {
		return srvUsage("access explain", accessUsage)
	}
	f := newFlags()
	id, asJSON := f.String("thread"), f.Bool("json")
	pos, err := f.Parse(args[:i])
	if err != nil {
		return srvFail("access explain", err)
	}
	slug, tid, err := e.accessTarget("access explain", pos, *id)
	if err != nil {
		return err
	}
	c, _, err := connect(false)
	if err != nil {
		return srvFail("access explain", err)
	}
	defer c.Close()
	var res proto.AccessExplainResult
	p := proto.AccessExplainParams{Project: slug, Thread: tid, Command: strings.Join(args[i+1:], " ")}
	if err := c.Call(proto.MethodAccessExplain, p, &res); err != nil {
		return srvFail("access explain", err)
	}
	if *asJSON {
		return e.printJSON(res)
	}
	printAccessExplain(e, res)
	return nil
}

// printAccessExplain prints an explanation: the sandbox, the prompt and the
// guard, each with why and where its rule comes from.
func printAccessExplain(e *Env, res proto.AccessExplainResult) {
	fmt.Fprintln(e.Stdout, oneLineText(res.Command))
	from := func(s string) string {
		if s == "" {
			return ""
		}
		return "(" + s + ")"
	}
	sandbox := "runs inside the sandbox"
	if res.Sandbox == proto.AccessOutside {
		sandbox = "runs OUTSIDE the sandbox"
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "  sandbox\t%s\t%s\t%s\n", sandbox, oneLineText(res.SandboxWhy), from(res.SandboxFrom))
	fmt.Fprintf(tw, "  prompt\t%s\t%s\t%s\n", res.Prompt, oneLineText(res.PromptWhy), from(res.PromptFrom))
	fmt.Fprintf(tw, "  guard\t%s\t%s\t\n", res.Guard, oneLineText(res.GuardWhy))
	tw.Flush()
	if res.Approved != "" {
		fmt.Fprintf(e.Stdout, "  %s\n", oneLineText(res.Approved))
	}
	if res.Agents != "" {
		fmt.Fprintf(e.Stdout, "  %s\n", oneLineText(res.Agents))
	}
}
