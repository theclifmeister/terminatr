package agent

import (
	"regexp"
	"strconv"
	"strings"
)

// Numbered dialogs on an agent's screen (a permission prompt, a question
// menu): what tm thread approve and answer, and the server's
// session.approve, read off the screen before they send a key.

var optionRE = regexp.MustCompile(`^[^0-9A-Za-z]*([0-9])\.\s+(.+)$`)

// DialogOption finds option n of a numbered dialog on screen, and the
// question above the options.
func DialogOption(screen string, n int) (option, question string) {
	lines := strings.Split(screen, "\n")
	first := -1
	for i, l := range lines {
		m := optionRE.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		if first < 0 {
			first = i
		}
		if m[1] == strconv.Itoa(n) && option == "" {
			option = strings.TrimSpace(m[2])
		}
	}
	for i := first - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); strings.HasSuffix(t, "?") {
			question = t
			break
		}
	}
	return option, question
}

// DialogOptions are the options of the numbered dialog on screen, in
// the order of their numbers' first lines, as DialogOption finds each.
func DialogOptions(screen string) []string {
	var out []string
	seen := map[string]bool{}
	for l := range strings.SplitSeq(screen, "\n") {
		m := optionRE.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, strings.TrimSpace(m[2]))
	}
	return out
}

// AllowsAlways says whether a permission dialog's option allows more
// than once ("Yes, and don't ask again", "always"): only the user picks
// that, in the agent's pane.
func AllowsAlways(option string) bool {
	l := strings.ToLower(option)
	return strings.Contains(l, "don't ask again") || strings.Contains(l, "always") || strings.Contains(l, "don’t ask again")
}
