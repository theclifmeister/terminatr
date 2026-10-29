package mod

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Rules sections (docs/SPEC.md §5.3, Rules sections; mod API 1.11): a
// rules file's lines between "<!-- tm:if <cond> -->" and
// "<!-- tm:end -->" are in the text only when the mod's settings for the
// project meet cond: "<key>" a bool setting that is true, "!<key>" one
// that is false, "<key>=<value>" a choice setting with that value. No
// nesting, no else. A setting's value is only compared, never put into
// the text, so it can't add an instruction: what an agent reads is
// always some of what the consent screen showed.

// RulesAPIMinor is the mod API minor that brought rules sections and
// per-project settings.
const RulesAPIMinor = 11

var (
	// rulesMarkRE is any line that looks meant as a marker: it must be
	// one of the two, so a typo can't leave a section always in.
	rulesMarkRE = regexp.MustCompile(`^<!--\s*tm:`)
	rulesIfRE   = regexp.MustCompile(`^<!--\s*tm:if\s+(\S+)\s*-->$`)
	rulesEndRE  = regexp.MustCompile(`^<!--\s*tm:end\s*-->$`)
)

// rulesCond is a section's condition.
type rulesCond struct {
	key, value string
	not        bool
}

// holds reports whether settings meet the condition.
func (c rulesCond) holds(settings map[string]any) bool {
	if c.value != "" {
		v, _ := settings[c.key].(string)
		return v == c.value
	}
	b, _ := settings[c.key].(bool)
	return b != c.not
}

// parseRulesCond reads cond, checked against the manifest's settings.
func parseRulesCond(cond string, settings map[string]Setting) (rulesCond, error) {
	var c rulesCond
	switch key, value, eq := strings.Cut(cond, "="); {
	case eq:
		c.key, c.value = key, value
	case strings.HasPrefix(cond, "!"):
		c.key, c.not = cond[1:], true
	default:
		c.key = cond
	}
	s, ok := settings[c.key]
	switch {
	case !ok:
		return c, fmt.Errorf("tm:if %s: the mod has no setting %q", cond, c.key)
	case c.value == "" && strings.Contains(cond, "="):
		return c, fmt.Errorf("tm:if %s: names no value", cond)
	case c.value != "" && s.Type != SettingChoice:
		return c, fmt.Errorf("tm:if %s: %s=<value> is for a choice setting; %s is %s", cond, c.key, c.key, s.Type)
	case c.value != "" && !slices.Contains(s.Options, c.value):
		return c, fmt.Errorf("tm:if %s: %q isn't one of %s's options (%s)", cond, c.value, c.key, strings.Join(s.Options, ", "))
	case c.value == "" && s.Type != SettingBool:
		return c, fmt.Errorf("tm:if %s: a bare key is for a bool setting; %s is %s (use %s=<option>)", cond, c.key, s.Type, c.key)
	}
	return c, nil
}

// HasRulesSections reports whether text has a line that looks like a
// section marker.
func HasRulesSections(text string) bool {
	for l := range strings.Lines(text) {
		if rulesMarkRE.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

// ResolveRules is rules text with its sections resolved for settings
// (the mod's for one project, Mod.SettingsFor): a section whose
// condition holds keeps its lines, others are dropped, and the marker
// lines go. settings nil only checks the text. An error says what is
// wrong with the markers, by line: the text is then not used.
func ResolveRules(text string, man *Manifest, settings map[string]any) (string, error) {
	var decl map[string]Setting
	if man != nil {
		decl = man.Settings
	}
	var out strings.Builder
	open, keep := 0, true
	n := 0
	for l := range strings.Lines(text) {
		n++
		t := strings.TrimSpace(l)
		if !rulesMarkRE.MatchString(t) {
			if keep {
				out.WriteString(l)
			}
			continue
		}
		if m := rulesIfRE.FindStringSubmatch(t); m != nil {
			if open > 0 {
				return "", fmt.Errorf("line %d: a tm:if inside the section that line %d opens; sections don't nest", n, open)
			}
			c, err := parseRulesCond(m[1], decl)
			if err != nil {
				return "", fmt.Errorf("line %d: %w", n, err)
			}
			open, keep = n, c.holds(settings)
			continue
		}
		if rulesEndRE.MatchString(t) {
			if open == 0 {
				return "", fmt.Errorf("line %d: a tm:end with no tm:if before it", n)
			}
			open, keep = 0, true
			continue
		}
		return "", fmt.Errorf("line %d: %q isn't a section marker: <!-- tm:if <key> -->, <!-- tm:if !<key> -->, <!-- tm:if <key>=<option> --> or <!-- tm:end -->", n, t)
	}
	if open > 0 {
		return "", fmt.Errorf("line %d: the section opened here has no tm:end", open)
	}
	return out.String(), nil
}

// checkRulesSections checks rules file rel's sections, and that a mod
// using them says a mod API that has them.
func checkRulesSections(man *Manifest, rel, text string) error {
	if !HasRulesSections(text) {
		return nil
	}
	if maj, min, ok := splitAPI(man.API); ok && maj == 1 && min < RulesAPIMinor {
		return fmt.Errorf("%s: sections (<!-- tm:if … -->) need api = \"1.%d\" in mod.toml", rel, RulesAPIMinor)
	}
	if _, err := ResolveRules(text, man, nil); err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return nil
}
