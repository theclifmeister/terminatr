package tasks

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// maxTitle caps the title column; longer titles end in "…".
const maxTitle = 60

// Lines are the tasks' one-line forms ("T12  started  must  Fix login
// redirect  2/3  t-0005  owner:claude  repo:web") with their columns
// aligned, for `tm task list` and `tm context`: id, status, priority,
// title, steps, thread, owner, repo. A column no task has takes no room. What agents wrote is
// shown as plain text (textsafe.Clean).
func Lines(ts []*Task) []string {
	// The id and status keep their old minimum widths, so a short list
	// reads as it always did.
	wRef, wStatus := 3, 7
	var wPrio, wTitle, wSteps, wThread int
	steps := make([]string, len(ts))
	titles, threads := make([]string, len(ts)), make([]string, len(ts))
	for i, t := range ts {
		titles[i], threads[i] = textsafe.Clean(t.Title, false), textsafe.Clean(t.Thread, false)
		wRef = max(wRef, width(t.Ref()))
		wStatus = max(wStatus, width(string(t.Status)))
		wPrio = max(wPrio, width(string(t.Priority)))
		wTitle = max(wTitle, min(width(titles[i]), maxTitle))
		if len(t.Steps) > 0 {
			steps[i] = fmt.Sprintf("%d/%d", t.StepsDone(), len(t.Steps))
		}
		wSteps = max(wSteps, width(steps[i]))
		wThread = max(wThread, width(threads[i]))
	}
	out := make([]string, len(ts))
	for i, t := range ts {
		cols := []string{pad(t.Ref(), wRef), pad(string(t.Status), wStatus)}
		if wPrio > 0 {
			cols = append(cols, pad(string(t.Priority), wPrio))
		}
		cols = append(cols, pad(textsafe.Clip(titles[i], maxTitle), wTitle))
		if wSteps > 0 {
			cols = append(cols, strings.Repeat(" ", wSteps-width(steps[i]))+steps[i])
		}
		if wThread > 0 {
			cols = append(cols, pad(threads[i], wThread))
		}
		if t.Owner != "" {
			cols = append(cols, "owner:"+textsafe.Clean(t.Owner, false))
		}
		if t.Repo != "" {
			cols = append(cols, "repo:"+textsafe.Clean(t.Repo, false))
		}
		if t.Archived {
			cols = append(cols, "(archived)")
		}
		out[i] = strings.TrimRight(strings.Join(cols, "  "), " ")
	}
	return out
}

func width(s string) int { return utf8.RuneCountInString(s) }

func pad(s string, w int) string { return s + strings.Repeat(" ", max(w-width(s), 0)) }

// Detail is a task in full, for `tm task show`, what agents wrote shown
// as plain text (textsafe.Clean).
func Detail(t *Task) string {
	one := func(s string) string { return textsafe.Clean(s, false) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", t.Ref(), one(t.Title))
	meta := []string{"status: " + string(t.Status)}
	if t.Priority != NoPriority {
		meta = append(meta, "priority: "+string(t.Priority))
	}
	for _, kv := range [][2]string{{"owner", t.Owner}, {"repo", t.Repo}, {"thread", t.Thread}, {"created", t.Created}, {"updated", t.Updated}} {
		if kv[1] != "" {
			meta = append(meta, kv[0]+": "+one(kv[1]))
		}
	}
	if t.Archived {
		meta = append(meta, "archived")
	}
	b.WriteString(strings.Join(meta, sep) + "\n")
	if t.Notes != "" {
		b.WriteString("\n" + textsafe.Clean(t.Notes, true) + "\n")
	}
	if len(t.Steps) > 0 {
		fmt.Fprintf(&b, "\nSteps (%d/%d):\n", t.StepsDone(), len(t.Steps))
		for _, s := range t.Steps {
			mark := " "
			if s.Done {
				mark = "x"
			}
			fmt.Fprintf(&b, "%3d. [%s] %s\n", s.N, mark, one(s.Text))
		}
	}
	return b.String()
}

// JSON is the stable --json shape of a task (§6.3).
type JSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	// Priority is must, should or could; absent for none.
	Priority Priority `json:"priority,omitempty"`
	Group    string   `json:"group"`
	Notes    string   `json:"notes"`
	Steps    []Step   `json:"steps"`
	Thread   string   `json:"thread"`
	Owner    string   `json:"owner"`
	Repo     string   `json:"repo,omitempty"`
	Created  string   `json:"created"`
	Updated  string   `json:"updated"`
	Archived bool     `json:"archived,omitempty"`
}

// ToJSON converts a task to its --json shape.
func ToJSON(t *Task) JSON {
	steps := t.Steps
	if steps == nil {
		steps = []Step{}
	}
	return JSON{
		ID: t.Ref(), Title: t.Title, Status: t.Status, Priority: t.Priority, Group: string(GroupOf(t.Status)),
		Notes: t.Notes, Steps: steps, Thread: t.Thread, Owner: t.Owner, Repo: t.Repo,
		Created: t.Created, Updated: t.Updated, Archived: t.Archived,
	}
}
