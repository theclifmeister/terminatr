package tasks

import "testing"

func TestRenameRefs(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"status: open · after: [[api/tasks/T2|api/T2]], api/T3,api/T12", "status: open · after: [[api/tasks/T2|core/T2]], core/T3,core/T12"},
		{"status: open · owner: director:api", "status: open · owner: director:core"},
		{"director:api (2026-11-03): routed web/T1, api/T4", "director:core (2026-11-03): routed web/T1, core/T4"},
		{"filed by the director of api (api/T1)", "filed by the director of core (core/T1)"},
		{"status: open · after: mini/api/T3, api-web/T3, xapi/T3", "status: open · after: mini/api/T3, api-web/T3, xapi/T3"},
		{"status: open · owner: director:api-web", "status: open · owner: director:api-web"},
		{"status: open · after: api/T", "status: open · after: api/T"},
		{"parts: [[api/tasks/T2|api/T2]], web/T1, api/T5", "parts: [[api/tasks/T2|core/T2]], web/T1, core/T5"},
	} {
		if got := RenameRefs(c.in, "api", "core"); got != c.want {
			t.Errorf("RenameRefs(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	if !RenameLine("director:acme (2026-11-03): x") || !RenameLine("status: open") || !RenameLine("parts: [[api/tasks/T2|api/T2]], web/T1") || RenameLine("api/T3 is next") || RenameLine("parts: of it") {
		t.Error("RenameLine")
	}
}
