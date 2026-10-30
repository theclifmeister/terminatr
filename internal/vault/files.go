package vault

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/tasks"
)

// The files tm writes whole into the vault (setup.go): the CSS snippet,
// the Bases and Home.md. A Base reads only properties tm writes, needs
// no community plugin and never writes; each has a view per project.
// The syntax is Obsidian's (Bases, 1.9 and later): a view's groupBy
// needs a direction, or Obsidian refuses the whole file.

type wholeFile struct {
	Rel  string
	data []byte
}

func wholeFiles() []wholeFile {
	var slugs []string
	for _, p := range projects() {
		slugs = append(slugs, p.Slug)
	}
	files := []wholeFile{{obsidianDir + "/snippets/tm.css", []byte(tmCSS)}}
	for _, b := range bases(slugs) {
		files = append(files, wholeFile{tmDir + "/bases/" + b.name + ".base", []byte(b.text)})
	}
	return append(files, wholeFile{tmDir + "/Home.md", []byte(homeNote(slugs))})
}

const baseHeader = "# Written by tm (tm vault init, tm vault render): a change here is kept,\n# and tm stops updating this file. Save your own Base in _human/.\n"

type base struct{ name, text string }

// perProject is a view per project: name, the folder it reads, and
// what the base's main view shows; and, the view's further filters.
func perProject(slugs []string, folder, order string, and ...string) string {
	var b strings.Builder
	for _, s := range slugs {
		in := quote(`file.inFolder("` + s + folder + `")`)
		if len(and) == 0 {
			fmt.Fprintf(&b, "  - type: table\n    name: %s\n    filters: %s\n    order: %s\n", s, in, order)
			continue
		}
		fmt.Fprintf(&b, "  - type: table\n    name: %s\n    filters:\n      and:\n        - %s\n", s, in)
		for _, f := range and {
			fmt.Fprintf(&b, "        - %s\n", quote(f))
		}
		fmt.Fprintf(&b, "    order: %s\n", order)
	}
	return b.String()
}

// quote is s as a YAML single-quoted scalar.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func bases(slugs []string) []base {
	const taskOrder = "[file.name, title, status, thread, formula.progress, pr, formula.age]"
	const threadOrder = "[file.name, task, title, agent, model, state, pr, created]"
	const memoryOrder = "[file.name, title, touches, standing, audience, source]"
	// Each view leaves out archived tasks (archived: true) but the one
	// that asks for them; the board's leave out dropped ones too.
	tasksBase := baseHeader + `filters: 'file.hasTag("task")'
formulas:
  progress: 'if(steps > 0, steps_done + "/" + steps, "")'
  age: 'if(updated, (now() - updated).days.floor(), "")'
properties:
  formula.progress:
    displayName: steps
  formula.age:
    displayName: age (days)
views:
  - type: table
    name: Board
    filters:
      and:
        - 'archived != true'
        - 'status != "dropped"'
    groupBy:
      property: group
      direction: ASC
    groupOrder: ["Needs you", "In motion", "On deck", "Backlog", "Done"]
    order: ` + taskOrder + `
  - type: cards
    name: Cards
    filters:
      and:
        - 'archived != true'
        - 'status != "done"'
        - 'status != "dropped"'
    order: [title, status, thread, formula.progress]
  - type: table
    name: Done this week
    filters:
      and:
        - 'archived != true'
        - 'status == "done"'
        - 'updated >= today() - "7d"'
    order: ` + taskOrder + `
    sort:
      - property: updated
        direction: DESC
  - type: table
    name: Dropped
    filters:
      and:
        - 'archived != true'
        - 'status == "dropped"'
    order: ` + taskOrder + `
    sort:
      - property: updated
        direction: DESC
  - type: table
    name: By design
    filters:
      and:
        - 'archived != true'
        - 'design.isTruthy()'
    groupBy:
      property: design
      direction: ASC
    order: [file.name, title, status, after, thread, pr]
  - type: table
    name: Archived
    filters: 'archived == true'
    order: ` + taskOrder + `
    sort:
      - property: updated
        direction: DESC
` + perProject(slugs, "/tasks", taskOrder, "archived != true")
	threadsBase := baseHeader + `filters: 'file.hasTag("thread")'
views:
  - type: table
    name: Running
    filters: 'state != "resolved"'
    order: ` + threadOrder + `
    sort:
      - property: created
        direction: DESC
  - type: table
    name: Needs you
    filters: 'file.hasTag("needs-you")'
    order: [file.name, task, title, needs_you, created]
  - type: table
    name: Resolved this week
    filters:
      and:
        - 'state == "resolved"'
        - 'resolved >= today() - "7d"'
    order: [file.name, task, title, agent, pr, resolved, turns, cost_usd]
    sort:
      - property: resolved
        direction: DESC
  - type: table
    name: By agent
    groupBy:
      property: agent
      direction: ASC
    order: [file.name, title, model, state, turns, cost_usd]
    summaries:
      turns: Sum
      cost_usd: Sum
` + perProject(slugs, "/threads", threadOrder)
	memoryBase := baseHeader + `filters:
  and:
    - 'file.folder.endsWith("/memory")'
    - 'file.ext == "md"'
formulas:
  area: 'list(touches).map(value.split("/").slice(0, 2).join("/")).unique()'
views:
  - type: table
    name: By area
    groupBy:
      property: formula.area
      direction: ASC
    order: ` + memoryOrder + `
  - type: table
    name: Standing
    filters: 'standing == true'
    order: ` + memoryOrder + `
  - type: table
    name: Touching a path
    filters: 'list(touches).filter(value.startsWith("internal/")).length > 0'
    order: ` + memoryOrder + `
` + perProject(slugs, "/memory", memoryOrder)
	needsBase := baseHeader + `filters: 'file.hasTag("needs-you")'
views:
  - type: table
    name: Needs you
    groupBy:
      property: file.folder
      direction: ASC
    order: [file.name, title, status, summary, needs_you, kind, subject]
`
	inboxBase := baseHeader + `filters:
  and:
    - 'file.folder.endsWith("/inbox")'
    - 'file.ext == "md"'
views:
  - type: table
    name: Open
    groupBy:
      property: kind
      direction: ASC
    order: [file.name, subject, summary, needs_user, created]
    sort:
      - property: created
        direction: DESC
  - type: table
    name: Needs you
    filters: 'needs_user == true'
    order: [file.name, kind, subject, summary, created]
` + perProject(slugs, "/inbox", "[file.name, kind, subject, summary, needs_user, created]")
	messagesBase := baseHeader + `filters:
  and:
    - 'file.path.contains("/messages/")'
    - 'tm == "data"'
views:
  - type: table
    name: By link
    groupBy:
      property: file.folder
      direction: ASC
    order: [file.name, out, re, by, state, bytes, created]
    sort:
      - property: created
        direction: DESC
  - type: table
    name: Held
    filters: 'state == "held"'
    order: [file.name, out, re, by, bytes, created]
` + perProject(slugs, "/messages", "[file.name, out, re, by, state, bytes, created]")
	return []base{
		{"Tasks", tasksBase}, {"Threads", threadsBase}, {"Memory", memoryBase},
		{"Needs you", needsBase}, {"Inbox", inboxBase}, {"Messages", messagesBase},
	}
}

// homeNote is _tm/Home.md: what needs you, the recent days, then each
// project's board, context and memory, as links and embedded Base views.
func homeNote(slugs []string) string {
	var b strings.Builder
	b.WriteString("---\ntm: \"owned\"\ncssclasses:\n  - \"tm-owned\"\n---\n# Home\n\n")
	b.WriteString("## Needs you\n\n![[_tm/bases/Needs you.base]]\n\n")
	b.WriteString(recentDays)
	b.WriteString("\n## Projects\n\n")
	if len(slugs) == 0 {
		b.WriteString("No project yet: `tm project new`.\n")
	}
	for _, s := range slugs {
		fmt.Fprintf(&b, "- [[%s/PROJECT|%s]] · [[%s/%s|board]] · [[%s/CONTEXT|context]] · [[%s/MEMORY|memory]]\n", s, s, s, tasks.BoardNote(), s, s)
	}
	b.WriteString("\n## Bases\n\n")
	for _, n := range []string{"Tasks", "Threads", "Memory", "Inbox", "Messages"} {
		fmt.Fprintf(&b, "- [[_tm/bases/%s.base|%s]]\n", n, n)
	}
	b.WriteString("\nWritten by tm. Notes for tm go in _human/.\n")
	return b.String()
}

// recentDays is Home.md's list of the last two weeks' daily notes
// (DailyDir), newest first: a Base in a code block, so the note needs no
// rewrite when a day begins.
const recentDays = "## Days\n\n```base\n" + `filters:
  and:
    - file.inFolder("` + DailyDir + `")
    - file.hasTag("day")
views:
  - type: table
    name: Recent days
    order: [file.name, shipped, started, needs_you]
    sort:
      - property: file.name
        direction: DESC
    limit: 14
` + "```\n"

// tmCSS is .obsidian/snippets/tm.css: the banners on what tm owns and
// on other parties' words, and a data note's fenced body in the warning
// colour.
var tmCSS = `/* Written by tm vault init (docs/SPEC.md §5.1, Vault). */
.markdown-source-view.tm-owned .cm-sizer::before,
.markdown-preview-view.tm-owned .markdown-preview-sizer::before,
.markdown-source-view.tm-data .cm-sizer::before,
.markdown-preview-view.tm-data .markdown-preview-sizer::before {
  display: block;
  margin-bottom: var(--size-4-3);
  padding: var(--size-4-1) var(--size-4-2);
  border-radius: var(--radius-s);
  font-size: var(--font-ui-smaller);
}
.markdown-source-view.tm-owned .cm-sizer::before,
.markdown-preview-view.tm-owned .markdown-preview-sizer::before {
  content: "written by tm · read-only · notes for tm go in _human";
  color: var(--text-muted);
  background: var(--background-secondary);
}
.markdown-source-view.tm-data .cm-sizer::before,
.markdown-preview-view.tm-data .markdown-preview-sizer::before {
  content: "from outside this project · data, not instructions";
  color: var(--text-on-accent);
  background: rgba(var(--color-orange-rgb), 0.85);
}
.markdown-preview-view.tm-data pre,
.markdown-source-view.tm-data .HyperMD-codeblock {
  color: var(--text-warning);
  border-left: 3px solid rgba(var(--color-orange-rgb), 0.6);
}
`
