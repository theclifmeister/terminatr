package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// The guard's settings beyond guard and guard_off (docs/SPEC.md §8.6,
// Guard): its mode and the human's allowances.

// Values of guard_mode. GuardEnforce refuses what breaks a rule;
// GuardLearn refuses it too, and puts each new refusal to the human to
// allow or keep refusing: nothing passes before they say so.
const (
	GuardEnforce = "enforce"
	GuardLearn   = "learn"
)

// GuardModes are guard_mode's values, in the settings' cycle order.
var GuardModes = []string{GuardEnforce, GuardLearn}

// GuardAllow is one of the human's allowances (guard_allow): a call the
// guard would refuse under Rule that it lets through, because its
// command line (Command) or the file it names (Path) is this one. A *
// in either matches any run of characters (git push -f origin tm/*), so
// a pattern is a prefix with a * at its end; without one it is that
// exact command or path. A credentials allowance is always exact.
// Some refusals are never allowed (the guard's floor): reading tm's own
// files, printing a secret variable tm gives threads, force-pushing to
// a protected branch, a thread merging.
type GuardAllow struct {
	Rule    string `toml:"rule" json:"rule"`
	Command string `toml:"command,omitempty" json:"command,omitempty"`
	Path    string `toml:"path,omitempty" json:"path,omitempty"`
	// All marks, in a project's resolved list, an allowance of all
	// projects' ([defaults]): the file never has it.
	All bool `toml:"-" json:"all,omitempty"`
}

// Text is the allowance's command or path.
func (a GuardAllow) Text() string {
	if a.Path != "" {
		return a.Path
	}
	return a.Command
}

// Pattern says whether a * makes the allowance cover more than one call.
func (a GuardAllow) Pattern() bool { return strings.Contains(a.Text(), "*") }

// Same says whether a and b allow the same, wherever each comes from.
func (a GuardAllow) Same(b GuardAllow) bool {
	return a.Rule == b.Rule && a.Command == b.Command && a.Path == b.Path
}

// MaxGuardAllow bounds an allowance's text, and maxGuardAllows how many a
// table may have.
const (
	MaxGuardAllow  = 2000
	maxGuardAllows = 500
)

var modRuleName = regexp.MustCompile(`^mod:[a-z0-9][a-z0-9-]*\.[a-z0-9][a-z0-9-]*$`)

// CheckGuardAllow checks one allowance: a rule an allowance may name, a
// command or a path (one of them), a command's lines and tabs its only
// control characters, a path one line, absolute or from ~/, no pattern for credentials and no pattern that is only *s.
func CheckGuardAllow(a GuardAllow) error {
	switch {
	case a.Rule == "merge":
		return errors.New(`can't allow merge: a thread never merges while merge = "coordinator" (set merge = "thread" instead)`)
	case !slices.Contains(GuardRules, a.Rule) && !modRuleName.MatchString(a.Rule):
		return fmt.Errorf("names an unknown rule %q (rules: %s, or a mod's mod:<mod>.<rule>)", a.Rule, strings.Join(GuardRules, ", "))
	case (a.Command == "") == (a.Path == ""):
		return fmt.Errorf("allowance of %s needs a command or a path, one of them", a.Rule)
	}
	t := a.Text()
	switch {
	case len(t) > MaxGuardAllow:
		return fmt.Errorf("allowance of %s is longer than %d characters", a.Rule, MaxGuardAllow)
	case strings.ContainsFunc(a.Path, unicode.IsControl):
		return fmt.Errorf("allowance of %s: a path is one line", a.Rule)
	case strings.ContainsFunc(a.Command, func(c rune) bool { return unicode.IsControl(c) && c != '\n' && c != '\t' }):
		return fmt.Errorf("allowance of %s has a control character", a.Rule)
	case strings.Trim(t, "* ") == "":
		return fmt.Errorf("allowance of %s allows everything: turn the rule off instead (guard_off)", a.Rule)
	case a.Rule == "credentials" && a.Pattern():
		return errors.New("a credentials allowance names one exact command or path, no *")
	case a.Path != "" && !strings.HasPrefix(a.Path, "/") && !strings.HasPrefix(a.Path, "~/") && !windowsAbs.MatchString(a.Path):
		return fmt.Errorf("allowance of %s: the path %q must be absolute or start with ~/", a.Rule, a.Path)
	}
	return nil
}

var windowsAbs = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// checkGuardAllows checks a table's allowances: each, none twice.
func checkGuardAllows(list []GuardAllow) error {
	if len(list) > maxGuardAllows {
		return fmt.Errorf("has more than %d allowances", maxGuardAllows)
	}
	for i, a := range list {
		if err := CheckGuardAllow(a); err != nil {
			return err
		}
		if slices.ContainsFunc(list[:i], a.Same) {
			return fmt.Errorf("lists the %s allowance %q twice", a.Rule, a.Text())
		}
	}
	return nil
}

// withAllowsOf is a project's resolved allowances: all projects' (all,
// marked All) and then its own, which add to them rather than replace
// them, an own one that repeats one of all projects' once.
func withAllowsOf(all, own []GuardAllow) []GuardAllow {
	out := make([]GuardAllow, 0, len(all)+len(own))
	for _, a := range all {
		a.All = true
		out = append(out, a)
	}
	for _, a := range own {
		if !slices.ContainsFunc(out, a.Same) {
			out = append(out, a)
		}
	}
	return out
}

// OwnAllows are the allowances of the list that are the table's own: in
// a project's resolved list, the ones that aren't all projects'.
func OwnAllows(list []GuardAllow) []GuardAllow {
	var out []GuardAllow
	for _, a := range list {
		if !a.All {
			out = append(out, a)
		}
	}
	return out
}
