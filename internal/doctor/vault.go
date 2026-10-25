package doctor

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/vault"
)

// Vault shows tm vault doctor's checks under "vault" once the projects
// folder is a vault (Obsidian opened it, or tm vault init ran): what
// Obsidian would ship off the machine or rewrite, tm's notes edited
// outside tm, and the links that point at nothing, counted (tm vault
// doctor lists them). There is no fix: the settings are the user's.
func Vault(d Deps) []Check {
	if !vault.Exists() {
		return nil
	}
	const g = "vault"
	var out []Check
	for _, f := range vault.Health() {
		st := OK
		switch f.Status {
		case "warn":
			st = Warn
		case "fail":
			st = Fail
		}
		out = append(out, Check{Group: g, Name: f.Name, Status: st, Detail: f.Detail})
	}
	broken, err := vault.Links()
	switch {
	case err != nil:
		out = append(out, Check{Group: g, Name: "links", Status: Warn, Detail: firstLine(err.Error())})
	case len(broken) > 0:
		out = append(out, Check{Group: g, Name: "links", Status: Warn, Detail: fmt.Sprintf("%d point at nothing (tm vault doctor lists them)", len(broken))})
	default:
		out = append(out, Check{Group: g, Name: "links", Status: OK, Detail: "every link resolves"})
	}
	return out
}
