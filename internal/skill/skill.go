// Package skill holds the standing rules of each agent role, embedded in
// tm and versioned with it (docs/SPEC.md §7.7–7.8). Role files and briefs
// only point at `tm skill <role>`, so the rules can't drift from the
// binary.
package skill

import (
	"embed"
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
)

//go:embed rules/*.md
var rules embed.FS

// Each role's rules are two files: <role>.safety.md, locked and always
// first, and <role>.policy.md, which a mod may replace (docs/SPEC.md §7.7,
// mods design §7 "Tighten only"). Nothing a policy says can loosen the
// safety part, because it comes before and is never replaced.

// Text returns the rules of a role. The first line is
// "tm skill <role> v<version>".
func Text(role, version string) (string, bool) {
	return TextWithPolicy(role, version, "")
}

// TextWithPolicy is Text with the role's policy part replaced by policy,
// when it is not empty. The safety part is always the built-in one, first.
func TextWithPolicy(role, version, policy string) (string, bool) {
	if role == "" || strings.ContainsAny(role, "/.") {
		return "", false
	}
	safety, err := rules.ReadFile("rules/" + role + ".safety.md")
	if err != nil {
		return "", false
	}
	if policy == "" {
		body, err := rules.ReadFile("rules/" + role + ".policy.md")
		if err != nil {
			return "", false
		}
		policy = string(body)
	}
	return fmt.Sprintf("tm skill %s v%s\n\n%s\n%s\n", role, strings.TrimPrefix(version, "v"), safety, strings.TrimRight(policy, "\n")), true
}

// Addendum is a mod's rules text for a role: a policy that replaces the
// built-in one, or house rules added after it.
type Addendum struct {
	Mod, Text string
}

// WithMods is Text with mods' rules (mods design §2 tier 3, §7 "Tighten
// only"): policy, when it names a mod, replaces the role's policy part
// under a heading that names it; each addendum follows the built-in
// text under "## House rules (mod: <id>)". Neither can remove a safety
// rule: the safety part comes first, always the built-in one.
func WithMods(role, version string, policy Addendum, addenda []Addendum) (string, bool) {
	text := ""
	if policy.Mod != "" {
		text = fmt.Sprintf("## Policy (mod: %s)\n\n%s", policy.Mod, strings.TrimSpace(policy.Text))
	}
	out, ok := TextWithPolicy(role, version, text)
	if !ok {
		return "", false
	}
	for _, a := range addenda {
		out += fmt.Sprintf("\n## House rules (mod: %s)\n\n%s\n", a.Mod, strings.TrimSpace(a.Text))
	}
	return out, true
}

// ForProject is the role's rules with the mods' that run for project
// slug ("" for every enabled mod): Text when no mod adds any, or the
// mods can't be read.
func ForProject(role, version, slug string) (string, bool) {
	cfg, _ := config.Load()
	r, err := mod.Load(cfg)
	if err != nil {
		return Text(role, version)
	}
	var policy Addendum
	p, add, _ := r.Rules(role, slug)
	if p != nil {
		policy = Addendum{Mod: p.Mod, Text: p.Text}
	}
	addenda := make([]Addendum, len(add))
	for i, a := range add {
		addenda[i] = Addendum{Mod: a.Mod, Text: a.Text}
	}
	return WithMods(role, version, policy, addenda)
}
