package cli

import "github.com/theclifmeister/terminatr/internal/textsafe"

// shown is text an agent or a mod wrote (a title, a summary, a status,
// a file name) as tm prints it for people: one line, with nothing a
// terminal would act on (textsafe.Clean).
func shown(s string) string { return textsafe.Clean(s, false) }

// shownText is shown keeping its newlines, for text that breaks into
// lines: a report, a screen.
func shownText(s string) string { return textsafe.Clean(s, true) }
