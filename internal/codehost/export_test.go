package codehost

import "testing"

// For contract_mod_test.go, in package codehost_test: a code host mod
// runs in a host whose bindings (modapi) import package mod, which
// imports this package.

// Scenario is one contract scenario, as a mod's fake glab sees it.
type Scenario struct{ Name, GL, GLA string }

// CheckModContract runs the contract against a host made per scenario.
func CheckModContract(t *testing.T, name string, newHost func(s Scenario) Host) {
	t.Helper()
	CheckContract(t, []hostFake{{name, func(s scenario) Host { return newHost(Scenario{s.name, s.gl, s.gla}) }}})
}
