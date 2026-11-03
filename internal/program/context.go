package program

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The director's context (the programs design §4.1): tm program context,
// and what its session-start hook carries. The rolled-up Needs you,
// slots and links come with the tasks that build them (B6, B11, B14).

// Caps on the program's own parts.
const (
	capChildren   = 30
	capChildTitle = 60
	// StartupBudget bounds the director's start-up block, in bytes, as
	// project.StartupBudget bounds a coordinator's: under the agents'
	// caps on hook output.
	StartupBudget = project.StartupBudget
)

// Context is tm program context: the program's goal, members and
// authority; PROGRAM.md's standing instructions, CONTEXT.md, the memory
// index, open questions and upkeep (the parts a project has too); the
// umbrella tasks; one line per child project; the inbox and the
// journal. cfg is config.Load's (nil reads it).
func (p *Program) Context(cfg *config.Config) ([]project.Section, error) {
	if cfg == nil {
		cfg, _ = config.Load()
	}
	t, _ := cfg.Program(p.Slug)
	head := []string{
		"Program: " + p.Slug,
		"Folder: " + p.Dir,
		"Goal: " + orNone(p.Meta.Goal),
		"Members: " + orNone(strings.Join(p.Members, ", ")),
		fmt.Sprintf("Settings: authority=%s · parallel_threads=%s · dependency_met=%s (config.toml; the human's)",
			cmpOr(t.Authority, config.AuthorityPropose), capText(t.ParallelThreads), cmpOr(t.DependencyMet, config.DependencyMerged)),
	}
	if p.LeftOut {
		head = append(head, "Left out: a project is called "+p.Slug+" too, so nothing routes to this program until one is renamed")
	}
	out := []project.Section{{Title: "Program", Lines: head, Part: project.PartProject}}
	state, err := p.StateSections("Standing instructions (PROGRAM.md)", p.Instructions, File)
	if err != nil {
		return nil, err
	}
	out = append(out, state...)
	um, err := project.TaskSection(p.Tasks(), "Umbrella tasks (TASKS.md)", "tm program task list")
	if err != nil {
		return nil, err
	}
	out = append(out, um)
	out = append(out, p.childrenSection(cfg))
	inbox, err := p.InboxSection()
	if err != nil {
		return nil, err
	}
	out = append(out, inbox)
	j, err := p.JournalSection()
	if err != nil {
		return nil, err
	}
	return append(out, j), nil
}

// childrenSection is one line per member: a local project's goal and
// its board's counts by group; a paired server's member by name only
// (its state comes over the link, B16).
func (p *Program) childrenSection(cfg *config.Config) project.Section {
	s := project.Section{Title: "Children", Part: project.PartThreads}
	for i, m := range p.Members {
		if i == capChildren {
			s.Omitted = fmt.Sprintf("%d more members (tm program show %s)", len(p.Members)-capChildren, p.Slug)
			break
		}
		s.Lines = append(s.Lines, childLine(m, cfg))
	}
	if len(s.Lines) == 0 {
		s.Lines = []string{"(no members yet: the user adds them in Settings, or in config.toml's [programs." + p.Slug + "] projects)"}
	}
	return s
}

// childLine is member m's line in the Children part.
func childLine(m string, cfg *config.Config) string {
	alias, slug, ok := config.SplitMember(m)
	switch {
	case !ok:
		return m + ": not a project's name"
	case alias != "":
		return m + ": on the paired server " + alias
	}
	c, err := project.OpenWith(slug, cfg)
	if err != nil {
		return m + ": not here (" + textsafe.Clip(err.Error(), capChildTitle) + ")"
	}
	line := m + ": " + textsafe.Clip(orNone(c.Meta.Goal), capChildTitle)
	if s, err := cfg.Safety(slug); err == nil && s.Paused {
		line += " · paused"
	}
	b, err := c.Tasks().Load()
	if err != nil {
		return line + " · tasks: unreadable"
	}
	var parts []string
	for _, g := range tasks.Groups {
		if g == tasks.DoneG || g == tasks.DroppedG {
			continue
		}
		n := 0
		for _, t := range b.Tasks {
			if tasks.GroupOf(t.Status) == g {
				n++
			}
		}
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(string(g))))
		}
	}
	if len(parts) == 0 {
		return line + " · no open tasks"
	}
	return line + " · tasks: " + strings.Join(parts, ", ")
}

// Startup is the director's start-up block: head (where its rules are),
// then the program's context, cut at StartupBudget with a line saying
// where the rest is.
func Startup(head string, secs []project.Section) string {
	head = strings.TrimRight(head, "\n")
	body := project.RenderContext(secs)
	tail := "That is the program's state at this conversation's start; `tm program context` prints it as it is now.\n"
	room := StartupBudget - len(head) - len(tail) - 2
	if len(body) > room {
		cut := strings.LastIndex(body[:max(room-40, 0)], "\n")
		body = body[:cut+1] + "[… cut: `tm program context` prints the rest]\n"
	}
	if head == "" {
		return body + "\n" + tail
	}
	return head + "\n\n" + body + "\n" + tail
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none set)"
	}
	return s
}

func cmpOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

func capText(n int) string {
	if n <= 0 {
		return "none (each project's own cap)"
	}
	return fmt.Sprint(n)
}
