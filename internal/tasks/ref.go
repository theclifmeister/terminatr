package tasks

import (
	"regexp"
	"strconv"
)

// RefPattern is a task ref as tm writes it ("T12"): T and the id, no
// leading zero, at most nine digits. It is unanchored, for patterns that
// find a ref inside a line; ValidRef is the whole-string check.
const RefPattern = `T[1-9][0-9]{0,8}`

// ThreadIDPattern is a thread id ("t-0003"): t- and four digits or more.
// It lives here, below project and thread, so both can use it;
// ValidThreadID is the whole-string check.
const ThreadIDPattern = `t-[0-9]{4,}`

var (
	// crossRE is a ref to another project's task: [<alias>/]<slug>/T<n>,
	// the alias a paired server's (peer.CheckAlias), the slug a
	// project's (project.ValidSlug).
	crossRE    = regexp.MustCompile(`^(?:([a-z0-9][a-z0-9-]{0,31})/)?([a-z0-9][a-z0-9-]{0,63})/(` + RefPattern + `)$`)
	refRE      = regexp.MustCompile(`^` + RefPattern + `$`)
	threadIDRE = regexp.MustCompile(`^` + ThreadIDPattern + `$`)
)

// ValidRef reports whether s is a task ref as tm writes it (RefPattern).
// ParseRef is more lenient: it reads what a human types ("t12", "12").
func ValidRef(s string) bool { return refRE.MatchString(s) }

// ValidThreadID reports whether s is a thread id (ThreadIDPattern).
func ValidThreadID(s string) bool { return threadIDRE.MatchString(s) }

// CrossRef reads a ref to another project's task, as a task's "after"
// holds it (programs, §6.1): "acme-api/T12" (alias "") or, for a
// project on a paired server, "mini/acme-ios/T7".
func CrossRef(s string) (alias, slug string, id int, ok bool) {
	m := crossRE.FindStringSubmatch(s)
	if m == nil {
		return "", "", 0, false
	}
	id, _ = strconv.Atoi(m[3][1:])
	return m[1], m[2], id, true
}
