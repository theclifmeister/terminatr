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
	lines, first := dialogLines(screen)
	for _, l := range lines[max(first, 0):] {
		m := optionRE.FindStringSubmatch(strings.TrimSpace(l))
		if m != nil && m[1] == strconv.Itoa(n) && option == "" {
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
	lines, first := dialogLines(screen)
	if first < 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, l := range lines[first:] {
		m := optionRE.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, strings.TrimSpace(m[2]))
	}
	return out
}

// dialogLines are the screen's lines and the index of the dialog's
// option 1: the last line numbered 1, since the conversation above the
// dialog may hold numbered lines of its own ("3. Exit code: 0"); -1
// for none.
func dialogLines(screen string) ([]string, int) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := optionRE.FindStringSubmatch(strings.TrimSpace(lines[i])); m != nil && m[1] == "1" {
			return lines, i
		}
	}
	return lines, -1
}

// AllowsAlways says whether a permission dialog's option allows more
// than once ("Yes, and don't ask again", "always"): only the user picks
// that, in the agent's pane.
func AllowsAlways(option string) bool {
	l := strings.ToLower(option)
	return strings.Contains(l, "don't ask again") || strings.Contains(l, "always") || strings.Contains(l, "don’t ask again")
}

// Scope is how long a permission dialog's option allows what the agent
// asks: this once, for the rest of the agent's session, or beyond it
// (the project's or the user's settings, every later session).
type Scope string

const (
	ScopeOnce       Scope = "once"
	ScopeSession    Scope = "session"
	ScopePersistent Scope = "persistent"
)

// OptionScope classifies a permission dialog's option by the agent's
// wording ([approve] in its manifest): persistent first, then session,
// then once, and an option that matches none persists, so a wording tm
// doesn't know is never answered for the user. A manifest with no
// wording (nil, or an agent tm doesn't ship) has only AllowsAlways to
// go by, and no session options.
func OptionScope(m *Manifest, option string) Scope {
	var a Approve
	if m != nil {
		a = m.Approve
	}
	if len(a.Persistent)+len(a.Session)+len(a.Once) == 0 {
		if AllowsAlways(option) {
			return ScopePersistent
		}
		return ScopeOnce
	}
	// The screen's apostrophe may be typographic; the wording's isn't.
	option = strings.ReplaceAll(strings.TrimSpace(option), "’", "'")
	switch {
	case matchesAny(a.Persistent, option):
		return ScopePersistent
	case matchesAny(a.Session, option):
		return ScopeSession
	case matchesAny(a.Once, option):
		return ScopeOnce
	}
	return ScopePersistent
}

func matchesAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if re, err := regexp.Compile("(?i)" + p); err == nil && re.MatchString(s) {
			return true
		}
	}
	return false
}
