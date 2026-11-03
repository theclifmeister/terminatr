package tasks

import (
	"regexp"
	"strings"
)

// After a rename of a project or a program (the programs design §3.4),
// the words tm wrote on other boards name it by its slug: cross refs in
// a task's after ("acme-api/T12"), a director's owner and note lines
// ("director:acme"), and the umbrella parts a route notes ("routed
// acme-api/T12, acme-web/T3", "(acme/T4)"). RenameRefs names the new
// slug in them; the rest of a board is someone's words, which a rename
// leaves alone.

// DirectorLine reports whether line of a board file is a note line a
// director's act wrote: "director:<program> (<date>): …" (ProgramEdit)
// or "filed by the director of <program> …" (ProgramAdd's filed line).
func DirectorLine(line string) bool {
	return strings.HasPrefix(line, "director:") || strings.HasPrefix(line, "filed by the director of ")
}

// RenameLine reports whether line of a board file is one RenameRefs
// rewrites: a metadata line (LinkLine) or a director's (DirectorLine).
func RenameLine(line string) bool { return LinkLine(line) || DirectorLine(line) }

// RenameRefs is line with slug from named to where tm wrote it: a cross
// ref from/T<n> on this server (not <alias>/from/T<n>, a paired
// server's), director:from, and "the director of from". Other words are
// unchanged, and so is a slug that only starts with from.
func RenameRefs(line, from, to string) string {
	if from == "" || to == "" || from == to || !strings.Contains(line, from) {
		return line
	}
	q := regexp.QuoteMeta(from)
	ref := regexp.MustCompile(`(^|[^a-z0-9/_-])` + q + `/(` + RefPattern + `)([^0-9]|$)`)
	line = replaceAll(ref, line, "${1}"+to+"/${2}${3}")
	own := regexp.MustCompile(`(^|[^a-z0-9/_-])(director:|the director of )` + q + `([^a-z0-9_-]|$)`)
	return replaceAll(own, line, "${1}${2}"+to+"${3}")
}

// replaceAll is re.ReplaceAllString until nothing changes: a match's
// last group is the next one's first, which one pass would skip
// ("a/T1,a/T2").
func replaceAll(re *regexp.Regexp, s, repl string) string {
	for range 64 {
		next := re.ReplaceAllString(s, repl)
		if next == s {
			break
		}
		s = next
	}
	return s
}
