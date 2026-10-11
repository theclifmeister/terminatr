// Package skill holds the standing rules of each agent role, embedded in
// tm and versioned with it (docs/SPEC.md §7.7–7.8). Role files and briefs
// only point at `tm skill <role>`, so the rules can't drift from the
// binary.
package skill

import (
	"embed"
	"fmt"
	"strings"
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
