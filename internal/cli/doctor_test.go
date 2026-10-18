package cli

import (
	"bytes"
	"testing"

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
