package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/theclifmeister/terminatr/internal/gitx"
)

// DefaultBranchName is the branch_name template of a thread's branch
// when no table sets one: tm/<project>/<id>-<title>.
const DefaultBranchName = "tm/{project}/{id}-{title}"

var placeholder = regexp.MustCompile(`\{[^{}]*\}`)

// ExpandBranchName fills template with v. A {task} or {title} that is
// empty takes one separator ('-' or '/') along, so no doubled or
// trailing one is left: the one after it, else the one before it
// (feature/{task}-{title} is feature/fix-login without a task, and the
// default is tm/demo/t-0001 without a title). It does not check the
// result: CheckBranchName does.
func ExpandBranchName(template, project, id, task, title string) string {
	vals := map[string]string{"{project}": project, "{id}": id, "{task}": task, "{title}": title}
	isSep := func(c byte) bool { return c == '-' || c == '/' }
	var b strings.Builder
	last := 0
	skip := false // the literal being copied starts after a separator to drop
	lit := func(s string) {
		if skip && s != "" && isSep(s[0]) {
			s = s[1:]
		}
		skip = false
		b.WriteString(s)
	}
	for _, m := range placeholder.FindAllStringIndex(template, -1) {
		lit(template[last:m[0]])
		last = m[1]
		name := template[m[0]:m[1]]
		val, known := vals[name]
		switch {
		case !known:
			b.WriteString(name)
		case val != "":
			b.WriteString(val)
		case name == "{task}" || name == "{title}":
			if last < len(template) && isSep(template[last]) {
				skip = true
			} else if s := b.String(); s != "" && isSep(s[len(s)-1]) {
				b.Reset()
				b.WriteString(s[:len(s)-1])
			}
		}
	}
	lit(template[last:])
	return b.String()
}

// CheckBranchName checks a branch_name template, with the reason it is
// refused: it must name {id} (a branch per thread), only use the
// placeholders {project}, {id}, {task} and {title}, make a branch name
// tm takes (gitx.ValidBranch), also for the longest project slug with
// no title (BranchName shortens the title to fit), and, with no task,
// not leave a doubled or trailing '-' or '/'. It runs no program: it is
// checked on every settings resolution. The error has no key in front.
func CheckBranchName(template string) error {
	if strings.TrimSpace(template) == "" {
		return limit("must not be empty")
	}
	for _, p := range placeholder.FindAllString(template, -1) {
		switch p {
		case "{project}", "{id}", "{task}", "{title}":
		default:
			return limit(fmt.Sprintf("has the unknown placeholder %s (use {project}, {id}, {task} or {title})", p))
		}
	}
	if strings.ContainsAny(placeholder.ReplaceAllString(template, ""), "{}") {
		return limit("has a stray { or }")
	}
	if !strings.Contains(template, "{id}") {
		return limit("must contain {id}, so every thread gets its own branch")
	}
	for _, empty := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		task, title := "T1", "short-title"
		if empty[0] {
			task = ""
		}
		if empty[1] {
			title = ""
		}
		name := ExpandBranchName(template, "demo", "t-0001", task, title)
		if bad := separatorFault(name); bad != "" {
			return limit(fmt.Sprintf("would leave %s in the branch name (%s)", bad, name))
		}
		if !gitx.ValidBranch(name) {
			return limit(fmt.Sprintf("would make a branch name git refuses (%s)", name))
		}
	}
	if name := ExpandBranchName(template, strings.Repeat("p", MaxProjectSlug), "t-99999", "T99999", ""); len(name) > gitx.MaxBranch {
		return limit(fmt.Sprintf("would make a branch name over %d characters for a project slug of %d", gitx.MaxBranch, MaxProjectSlug))
	}
	return nil
}

// MaxProjectSlug is the longest project slug (project.ValidSlug).
const MaxProjectSlug = 64

// separatorFault names what is wrong with name's separators, "" for
// nothing: a doubled '-' or '/', a '-' next to a '/', or either at an
// end.
func separatorFault(name string) string {
	switch {
	case strings.Contains(name, "--"), strings.Contains(name, "//"), strings.Contains(name, "-/"), strings.Contains(name, "/-"):
		return "a doubled separator"
	case strings.HasSuffix(name, "-"), strings.HasSuffix(name, "/"):
		return "a trailing separator"
	case strings.HasPrefix(name, "-"), strings.HasPrefix(name, "/"):
		return "a leading separator"
	}
	return ""
}
