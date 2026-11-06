package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/doctor"
)

func TestCheckPrinterLayout(t *testing.T) {
	var out bytes.Buffer
	p := &checkPrinter{e: &Env{Stdout: &out}}
	p.print([]doctor.Check{
		{Group: "toolchain", Name: "git", Status: doctor.OK, Detail: "2.50"},
		{Group: "toolchain", Name: "日本語", Status: doctor.Warn, Detail: "wide"},
	})
	p.print([]doctor.Check{{Group: "server", Name: "pid", Status: doctor.OK, Detail: "42"}})
	p.waiting("code host")
	p.print([]doctor.Check{{Group: "code host", Name: "gh auth", Status: doctor.Fail, Detail: "no"}})
	want := "TOOLCHAIN\n" +
		"  ok    git                     2.50\n" +
		"  warn  日本語                  wide\n" +
		"\nSERVER\n" +
		"  ok    pid                     42\n" +
		"\nCODE HOST (checking…)\n" +
		"  fail  gh auth                 no\n"
	if got := out.String(); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestDoctorFixByRole: tm doctor --fix is the human's. An agent is refused
// before any check runs, and so before any fix could; the human's own run is
// judged by doctorFixAllowed alone, so that no test ever applies a real fix.
func TestDoctorFixByRole(t *testing.T) {
	human := caller.Caller{Kind: caller.Human}
	thread := caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}
	for _, who := range []caller.Caller{
		{Kind: caller.Coordinator, Project: "demo"}, thread, {Kind: caller.Director, Project: "acme"},
	} {
		for _, args := range [][]string{{"--fix"}, {"--fix", "--yes"}, {"--fix", "--yes", "--json"}} {
			var out, errs bytes.Buffer
			e := &Env{Stdout: &out, Stderr: &errs, Getenv: func(string) string { return "" }, Caller: who}
			code := e.report("tm doctor", doctorCmd(e, args))
			if code != ExitRefused || !strings.Contains(errs.String(), "human-only") || out.Len() != 0 {
				t.Errorf("%s tm doctor %v: %d stdout %q stderr %q", who.Kind, args, code, out.String(), errs.String())
			}
		}
	}

	for _, c := range []struct {
		name     string
		env      caller.Caller
		identify func() (caller.Caller, bool)
		allowed  bool
	}{
		{"human", human, nil, true},
		{"human, the server agrees", human, func() (caller.Caller, bool) { return human, true }, true},
		{"human, no server answers", human, func() (caller.Caller, bool) { return caller.Caller{}, false }, true},
		{"variables unset, the server sees a thread", human, func() (caller.Caller, bool) { return thread, true }, false},
	} {
		e := &Env{Getenv: func(string) string { return "" }, Caller: c.env, Identify: c.identify}
		if err := e.doctorFixAllowed(); (err == nil) != c.allowed {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
