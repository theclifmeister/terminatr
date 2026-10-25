package vault

import (
	"fmt"
	"strings"
)

// The files tm writes whole into the vault (setup.go): the CSS snippet,
// the Bases and Home.md. A Base reads only properties tm writes, needs
// no community plugin and never writes; each has a view per project.
// The syntax is Obsidian's (Bases, 1.9 and later): a view's groupBy
// needs a direction, or Obsidian refuses the whole file.

type ownedFile struct {
	Rel  string
	data []byte
}

func ownedFiles() []ownedFile {
	var slugs []string
	for _, p := range projects() {
		slugs = append(slugs, p.Slug)
	}
	files := []ownedFile{{obsidianDir + "/snippets/tm.css", []byte(tmCSS)}}
	for _, b := range bases(slugs) {
		files = append(files, ownedFile{tmDir + "/bases/" + b.name + ".base", []byte(b.text)})
	}
	return append(files, ownedFile{tmDir + "/Home.md", []byte(homeNote(slugs))})
}

const baseHeader = "# Written by tm (tm vault init, tm vault render): a change here is kept,\n# and tm stops updating this file. Save your own Base in _human/.\n"

type base struct{ name, text string }

// perProject is a view per project: name, the folder it reads, and
// what the base's main view shows.
func perProject(slugs []string, folder, order string) string {
	var b strings.Builder
	for _, s := range slugs {
		fmt.Fprintf(&b, "  - type: table\n    name: %s\n    filters: %s\n    order: %s\n", s, quote(`file.inFolder("`+s+folder+`")`), order)
	}
	return b.String()
}

// quote is s as a YAML single-quoted scalar.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func bases(slugs []string) []base {
	const taskOrder = "[file.name, title, status, thread, formula.progress, pr, formula.age]"
	const threadOrder = "[file.name, task, title, agent, model, state, pr, created]"
	const memoryOrder = "[file.name, title, touches, standing, audience, source]"
	tasksBase := baseHeader + `filters:
  and:
    - file.hasTag("task")
    - 'archived != true'
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
    groupBy:
      property: group
      direction: ASC
    groupOrder: ["Needs you", "In motion", "On deck", "Backlog", "Done"]
    order: ` + taskOrder + `
  - type: cards
    name: Cards
    filters: 'status != "done"'
    order: [title, status, thread, formula.progress]
  - type: table
    name: Done this week
    filters:
      and:
        - 'status == "done"'
        - 'updated >= today() - "7d"'
    order: ` + taskOrder + `
    sort:
      - property: updated
        direction: DESC
  - type: table
    name: By design
    filters: 'design.isTruthy()'
    groupBy:
      property: design
      direction: ASC
    order: [file.name, title, status, after, thread, pr]
` + perProject(slugs, "/tasks", taskOrder)
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

// homeNote is _tm/Home.md: what needs you, then each project's board,
// threads and memory, as links and embedded Base views.
func homeNote(slugs []string) string {
	var b strings.Builder
	b.WriteString("---\ntm: \"owned\"\ncssclasses:\n  - \"tm-owned\"\n---\n# Home\n\n")
	b.WriteString("## Needs you\n\n![[_tm/bases/Needs you.base]]\n\n## Projects\n\n")
	if len(slugs) == 0 {
		b.WriteString("No project yet: `tm project new`.\n")
	}
	for _, s := range slugs {
		fmt.Fprintf(&b, "- [[%s/PROJECT|%s]] · [[%s/TASKS|board]] · [[%s/CONTEXT|context]] · [[%s/MEMORY|memory]]\n", s, s, s, s, s)
	}
	b.WriteString("\n## Bases\n\n")
	for _, n := range []string{"Tasks", "Threads", "Memory", "Inbox", "Messages"} {
		fmt.Fprintf(&b, "- [[_tm/bases/%s.base|%s]]\n", n, n)
	}
	b.WriteString("\nWritten by tm. Notes for tm go in _human/.\n")
	return b.String()
}

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
  content: "written by tm · edits here are overwritten · notes for tm go in _human";
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

