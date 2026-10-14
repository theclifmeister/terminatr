package tasks

import "regexp"

// RefPattern is a task ref as tm writes it ("T12"): T and the id, no
// leading zero, at most nine digits. It is unanchored, for patterns that
// find a ref inside a line; ValidRef is the whole-string check.
const RefPattern = `T[1-9][0-9]{0,8}`

// ThreadIDPattern is a thread id ("t-0003"): t- and four digits or more.
// It lives here, below project and thread, so both can use it;
// ValidThreadID is the whole-string check.
const ThreadIDPattern = `t-[0-9]{4,}`

var (
	refRE      = regexp.MustCompile(`^` + RefPattern + `$`)
	threadIDRE = regexp.MustCompile(`^` + ThreadIDPattern + `$`)
)

// ValidRef reports whether s is a task ref as tm writes it (RefPattern).
// ParseRef is more lenient: it reads what a human types ("t12", "12").
func ValidRef(s string) bool { return refRE.MatchString(s) }

// ValidThreadID reports whether s is a thread id (ThreadIDPattern).
func ValidThreadID(s string) bool { return threadIDRE.MatchString(s) }
