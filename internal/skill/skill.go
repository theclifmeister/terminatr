// Package skill holds the standing rules of each agent role, embedded in
// tm and versioned with it (docs/SPEC.md §7.7–7.8). Role files and briefs
// only point at `tm skill <role>`, so the rules can't drift from the
// binary.
package skill

import (
	"embed"
	"fmt"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
)

//go:embed rules/*.md
var rules embed.FS

// Each role's rules are two parts: rules/<role>.safety.md, locked and
// always first, and the policy, which a mod may replace (docs/SPEC.md
// §7.7, mods design §7 "Tighten only"). The built-in policy ships as the
// bundled default-policy mod (internal/mod/bundled/default-policy, mods
// design §11). Nothing a policy says can loosen the safety part, because
// it comes before and is never replaced.

// Text returns the rules of a role. The first line is
// "tm skill <role> v<version>".
func Text(role, version string) (string, bool) {
	return TextWithPolicy(role, version, "")
}

// TextWithPolicy is Text with the role's policy part replaced by policy,
// when it is not empty. The safety part is always the built-in one, first.
func TextWithPolicy(role, version, policy string) (string, bool) {
	return compose(role, version, policy, nil)
}

// Programs is the coordinator's program part (the programs design §4.3,
// §3.5), which goes in only while [features] programs is on: its locked
// safety line, right after the coordinator's safety part, and its
// policy, after the coordinator's policy. Policy "" is the built-in one.
type Programs struct {
	Policy string
}

// compose is a role's rules: its safety part, then policy (the built-in
// one when ""). programs, for a coordinator, adds the program part
// (Programs) while programs are on; nil leaves it out, as off does.
func compose(role, version, policy string, programs *Programs) (string, bool) {
	if role == "" || strings.ContainsAny(role, "/.") {
		return "", false
	}
	safety, err := rules.ReadFile("rules/" + role + ".safety.md")
	if err != nil {
		return "", false
	}
	if policy == "" {
		body, ok := mod.BundledPolicyText(role)
		if !ok {
			return "", false
		}
		policy = body
	}
	policy = strings.TrimRight(policy, "\n")
	if programs != nil && role == "coordinator" {
		line, err := rules.ReadFile("rules/coordinator.programs.safety.md")
		if err != nil {
			return "", false
		}
		// One more bullet of the Safety list, which ends the file.
		safety = append(slices.Clip(safety), line...)
		add := programs.Policy
		if add == "" {
			body, ok := mod.BundledPolicyText("coordinator_programs")
			if !ok {
				return "", false
			}
			add = body
		}
		policy += "\n\n" + strings.TrimRight(add, "\n")
	}
	return fmt.Sprintf("tm skill %s v%s\n\n%s\n%s\n", role, strings.TrimPrefix(version, "v"), safety, policy), true
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
	return withMods(role, version, policy, addenda, nil)
}

func withMods(role, version string, policy Addendum, addenda []Addendum, programs *Programs) (string, bool) {
	text := ""
	if policy.Mod != "" {
		text = fmt.Sprintf("## Policy (mod: %s)\n\n%s", policy.Mod, strings.TrimSpace(policy.Text))
	}
	out, ok := compose(role, version, text, programs)
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
// mods can't be read. For a director, slug is its program's. A
// coordinator's have the program part (Programs) while programs are on,
// and only then: off, they are today's text exactly (§3.5).
func ForProject(role, version, slug string) (string, bool) {
	cfg, _ := config.Load()
	var programs *Programs
	if role == "coordinator" && cfg.ProgramsOn() {
		programs = &Programs{}
	}
	r, err := mod.Load(cfg)
	if err != nil {
		return compose(role, version, "", programs)
	}
	var policy Addendum
	p, add, _ := r.Rules(role, slug)
	if p != nil && !p.Builtin {
		policy = Addendum{Mod: p.Mod, Text: p.Text}
	}
	if programs != nil {
		if pp, _, _ := r.Rules("coordinator_programs", slug); pp != nil && !pp.Builtin {
			programs.Policy = fmt.Sprintf("## Program (mod: %s)\n\n%s", pp.Mod, strings.TrimSpace(pp.Text))
		}
	}
	addenda := make([]Addendum, len(add))
	for i, a := range add {
		addenda[i] = Addendum{Mod: a.Mod, Text: a.Text}
	}
	return withMods(role, version, policy, addenda, programs)
}

// Headings are the "## " headings of rules text, in order.
func Headings(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimRight(l, "\r"), "## "); ok {
			out = append(out, strings.TrimSpace(h))
		}
	}
	return out
}

// Sections are the sections of rules text whose "## " heading is name,
// ignoring case, each from its heading up to the next "## " one, one
// blank line between them; with none, those whose heading starts with
// name ("house rules" finds each mod's). "" when no heading matches.
func Sections(text, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	for _, prefix := range []bool{false, true} {
		var out []string
		in := false
		for _, l := range strings.Split(text, "\n") {
			l = strings.TrimRight(l, "\r")
			if h, ok := strings.CutPrefix(l, "## "); ok {
				h = strings.ToLower(strings.TrimSpace(h))
				in = h == name || prefix && strings.HasPrefix(h, name)
				if in && len(out) > 0 {
					out = append(trimBlank(out), "")
				}
			}
			if in {
				out = append(out, l)
			}
		}
		if out = trimBlank(out); len(out) > 0 {
			return strings.Join(out, "\n") + "\n"
		}
	}
	return ""
}

// trimBlank is lines without the blank ones at the end.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
