package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The start-up block (docs/SPEC.md §7.8): what a coordinator whose rules
// are in its system prompt gets back on SessionStart. It is an index of
// `tm context`, under the agent's cap on hook output: each part has a
// budget in bytes, a part that needs less gives what it leaves to the
// next, and a part that shows less than `tm context` ends with the
// `tm context --part <name>` that prints it in full.

// The parts of `tm context` (Section.Part), each one `tm context
// --part <name>`.
const (
	PartProject      = "project"
	PartInstructions = "instructions"
	PartNeeds        = "needs"
	PartContext      = "context"
	PartUpkeep       = "upkeep"
	PartMemory       = "memory"
	PartTasks        = "tasks"
	PartThreads      = "threads"
	PartInbox        = "inbox"
	PartLinks        = "links"
	PartJournal      = "journal"
)

// StartupBudget bounds the whole start-up block, in bytes: under
// Claude's 10,000 characters for hook output (a byte count is never
// below a character count), with room for the header Claude adds.
const StartupBudget = 9000

// StartupCut is where the block is clipped at run time, should a part
// ever pass its budget: past StartupBudget, still under the cap. A clip
// is journaled (context.startup.cut) and tm doctor warns.
const StartupCut = 9500

// ActionStartupCut is the journal action of a start-up block clipped at
// StartupCut (tm doctor warns of a recent one).
const ActionStartupCut = "context.startup.cut"

// StartupPart is one part of the start-up block and its budget in
// bytes. The parts' budgets and StartupHead sum to StartupBudget.
type StartupPart struct {
	Name   string
	Budget int
}

// StartupHead is the budget of the block's own lines: the header (where
// the rules are, what changed since the session started) and the last
// line (StartupTail).
const StartupHead = 500

// StartupParts are the start-up block's parts in order. PROJECT.md's
// standing instructions are not one: they are in the system prompt.
// CONTEXT.md's budget (3 KB) fits whole in its part.
var StartupParts = []StartupPart{
	{PartProject, 900},
	{PartNeeds, 700},
	{PartContext, 3100},
	{PartUpkeep, 450},
	{PartMemory, 850},
	{PartTasks, 950},
	{PartThreads, 850},
	{PartInbox, 300},
	{PartLinks, 100},
	{PartJournal, 300},
}

// ContextParts are the names `tm context --part` takes, in `tm context`'s order.
var ContextParts = []string{PartProject, PartInstructions, PartNeeds, PartContext, PartUpkeep, PartMemory, PartTasks, PartThreads, PartInbox, PartLinks, PartJournal}

// StartupTail is the block's last line.
const StartupTail = "That is the project's state at this conversation's start, as an index: a part that shows less ends with the `tm context --part <name>` that prints it in full; `tm context` prints all of it as it is now.\n"

// PartSections are the sections of part name as `tm context --part`
// prints them: in full, as `tm context` does. The needs part is open
// questions after CONTEXT.md's "## Needs you".
func PartSections(secs []Section, name string) []Section {
	var out []Section
	if name == PartNeeds {
		for _, s := range secs {
			if s.Part != PartContext {
				continue
			}
			if needs, _, ok := needsYou(s.Lines); ok {
				out = append(out, Section{Title: "Needs you (CONTEXT.md)", Lines: needs, Part: PartNeeds})
			}
		}
	}
	for _, s := range secs {
		if s.Part == name {
			out = append(out, s)
		}
	}
	return out
}

// Startup is the start-up block: head (the header's lines), then each
// of StartupParts within its budget and what earlier parts left, then
// StartupTail. It stays within StartupBudget unless head passes
// StartupHead by more than the parts leave.
func Startup(head string, secs []Section) string {
	var b strings.Builder
	head = strings.TrimRight(head, "\n")
	if head != "" {
		b.WriteString(head + "\n")
	}
	spill := StartupHead - b.Len() - len(StartupTail) - 1
	for _, part := range StartupParts {
		avail := part.Budget + spill
		text := renderPart(PartSections(secs, part.Name), part.Name, avail-1) // and the blank line before it
		if text != "" {
			b.WriteString("\n" + text)
			avail -= len(text) + 1
		}
		spill = avail
	}
	b.WriteString("\n" + StartupTail)
	return b.String()
}

// renderPart is the part's sections in the index within avail bytes:
// the sections' Index (else their Lines), whole when they fit; else as
// many lines as fit. Either way, a part that shows less than `tm
// context` ends with its pointer. "" for a part with no sections.
func renderPart(ss []Section, name string, avail int) string {
	if len(ss) == 0 {
		return ""
	}
	type line struct {
		text string
		body bool
	}
	var lines []line
	short := false
	for _, s := range ss {
		title, ls := s.Title, s.Lines
		if s.Index != nil {
			ls, short = s.Index, short || !s.IndexWhole
			title = cmpOr(s.IndexTitle, title)
		}
		lines = append(lines, line{"## " + textsafe.Clean(title, false) + "\n", false})
		if s.Omitted != "" {
			short = true
		}
		for _, l := range ls {
			lines = append(lines, line{textsafe.Clean(l, false) + "\n", true})
		}
		if len(ls) == 0 {
			lines = append(lines, line{"(empty)\n", true})
		}
	}
	pointer := func(n int) string {
		if n == 0 {
			return "[… in full: tm context --part " + name + "]\n"
		}
		return fmt.Sprintf("[… %d more lines: tm context --part %s]\n", n, name)
	}
	var full strings.Builder
	body := 0
	for _, l := range lines {
		full.WriteString(l.text)
		if l.body {
			body++
		}
	}
	if !short && full.Len() <= avail {
		return full.String()
	}
	if short && full.Len()+len(pointer(0)) <= avail {
		return full.String() + pointer(0)
	}
	room := avail - len(pointer(body))
	var b []byte
	shown := 0
	head, first := 0, false // where the section's heading starts; no line of it yet
	for _, l := range lines {
		if !l.body {
			if len(b)+len(l.text) > room {
				break
			}
			head, first = len(b), true
			b = append(b, l.text...)
			continue
		}
		if len(b)+len(l.text) > room {
			// A section's first line too long for what is left is cut
			// to fit; a heading with no line under it goes.
			if c := cutLine(l.text, room-len(b)); first && len(c) >= minCut {
				b = append(b, c...)
				shown++
			} else if first {
				b = b[:head]
			}
			break
		}
		b = append(b, l.text...)
		shown++
		first = false
	}
	return string(b) + pointer(body-shown)
}

// minCut is the shortest cut line worth showing, in bytes.
const minCut = 40

// cutLine is line l (with its line end) cut to at most n bytes, on a
// character's edge, still ending the line; "" when n has no room.
func cutLine(l string, n int) string {
	if n < 5 { // "…" and a line end
		return ""
	}
	return textsafe.Cut(strings.TrimSuffix(l, "\n"), n-4) + "\n"
}

// needsYouRE is CONTEXT.md's heading of what waits on the user.
var needsYouRE = regexp.MustCompile(`(?i)^#{1,6}\s+needs you\b`)

// needsYou splits CONTEXT.md's lines into its "## Needs you" section's
// body and the rest; ok is false when it has none.
func needsYou(lines []string) (needs, rest []string, ok bool) {
	start := -1
	level := 0
	for i, l := range lines {
		if needsYouRE.MatchString(l) {
			start, level = i, len(l)-len(strings.TrimLeft(l, "#"))
			break
		}
	}
	if start < 0 {
		return nil, lines, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if n := len(lines[i]) - len(strings.TrimLeft(lines[i], "#")); n > 0 && n <= level && strings.HasPrefix(lines[i][n:], " ") {
			end = i
			break
		}
	}
	needs = trimBlank(lines[start+1 : end])
	rest = append(append([]string{}, lines[:start]...), lines[end:]...)
	return needs, trimBlank(rest), true
}

// trimBlank drops blank lines at both ends of lines.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// memoryIndex is MEMORY.md's notes in the start-up index: a line each,
// its name and title (what it holds and its touches are in `tm context
// --part memory` and tm memory list).
func memoryIndex(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		path, title, _, ok := keptLine(strings.TrimRight(l, "\r"))
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(path, "memory/"), ".md")
		out = append(out, "- "+name+": "+title)
	}
	if out == nil {
		return nil
	}
	return out
}

// StartupCuts are the journal's context.startup.cut lines since since:
// how many, and the latest. tm doctor warns of them, since a cut block
// means a part passed its budget and an agent lost some of its state.
func (p *Project) StartupCuts(since time.Time) (n int, last string, err error) {
	data, err := os.ReadFile(p.Path("JOURNAL.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	for _, l := range strings.Split(string(data), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || f[2] != ActionStartupCut {
			continue
		}
		if at, err := time.Parse(time.RFC3339, f[0]); err != nil || at.Before(since) {
			continue
		}
		n, last = n+1, l
	}
	return n, last, nil
}
