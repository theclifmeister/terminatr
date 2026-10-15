package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/doctor"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/service"
	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/version"
)

const doctorUsage = `usage: tm doctor [--fix [--yes]] [--json]`

func init() {
	commands["doctor"] = doctorCmd
}

// doctorCmd implements `tm doctor [--fix]` (docs/SPEC.md). It
// never starts the server and removes nothing without --fix and a
// confirmation (a y on a TTY, or --yes).
func doctorCmd(e *Env, args []string) error {
	f := newFlags()
	fix := f.Bool("fix") // remove what the checks found left over, after confirmation
	yes := f.Bool("yes") // with --fix: don't ask
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 {
		return srvUsage("doctor", doctorUsage)
	}
	p, err := client.ResolvePaths()
	if err != nil {
		return srvFail("doctor", err)
	}
	d := doctor.DefaultDeps(p, version.Version, version.BuildID())
	d.Selftest = libghosttySelftest
	exe, _ := os.Executable()
	in := update.Detect(exe, version.Channel)
	d.Install = &in
	d.SandboxProbe = func(p agent.SandboxProber) (string, error) { return p.ProbeSandbox(exe) }
	d.ThreadSandboxProbe = func(p agent.ThreadSandboxProber) (string, error) { return p.ProbeThreadSandbox(exe) }
	d.Latest = func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r, err := update.NewClient(e.Getenv).Latest(ctx)
		return r.Tag, err
	}
	d.Restart = func() error {
		// The same path as tm server restart, which never refuses on
		// version (docs/SPEC.md §3.3).
		if err := serverCmd(e, []string{"restart", "--yes"}); err != nil {
			return fmt.Errorf("tm server restart exited %d", e.report("tm server", err))
		}
		return nil
	}
	d.Launchd = func() bool {
		// From inside one of the server's sessions the restart would
		// end the session running it before the start.
		if !service.Wanted(runtime.GOOS, e.Getenv) || e.Getenv("TERMINATR_SESSION") != "" {
			return false
		}
		c, err := client.LaunchdConfig(p)
		return err == nil && c.HasConsole()
	}
	if service.Wanted(runtime.GOOS, e.Getenv) {
		if c, err := client.LaunchdConfig(p); err == nil {
			d.Strays, d.Bootout = c.Strays, c.Bootout
		}
	}
	if *asJSON && !*fix {
		checks := doctor.Run(d)
		if err := e.printJSON(map[string]any{"status": doctor.Worst(checks), "checks": checks}); err != nil {
			return &exitError{ExitIO}
		}
		if doctor.Worst(checks) == doctor.Fail {
			return &exitError{ExitRefused}
		}
		return nil
	}
	// Each group prints as soon as it is checked (doctor.Each).
	var checks []doctor.Check
	pr := &checkPrinter{e: e}
	doctor.Each(d, func(cs []doctor.Check) {
		checks = append(checks, cs...)
		pr.print(cs)
	}, pr.waiting)
	pr.summary()
	fixes := doctor.Fixes(checks)
	var failed error // exit 1 for a failed check or fix
	if doctor.Worst(checks) == doctor.Fail {
		failed = &exitError{ExitRefused}
	}
	if len(fixes) == 0 {
		return failed
	}
	if !*fix {
		fmt.Fprintf(e.Stdout, "\n%d thing(s) can be fixed: tm doctor --fix\n", len(fixes))
		return failed
	}
	fmt.Fprintln(e.Stdout, "\ntm doctor --fix will:")
	for _, f := range fixes {
		fmt.Fprintf(e.Stdout, "  - %s\n", f.Desc)
	}
	if !*yes {
		if !isTTY(os.Stdin) {
			fmt.Fprintln(e.Stderr, "tm doctor: not a terminal; pass --yes to do these")
			return &exitError{ExitRefused}
		}
		fmt.Fprint(e.Stderr, "Do them? [y/N] ")
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(e.Stdout, "nothing done")
			return &exitError{ExitRefused}
		}
	}
	for _, f := range fixes {
		if err := f.Apply(); err != nil {
			fmt.Fprintf(e.Stdout, "  fail  %s: %v\n", f.Desc, err)
			failed = &exitError{ExitRefused}
			continue
		}
		fmt.Fprintf(e.Stdout, "  done  %s\n", f.Desc)
	}
	return failed
}

// checkPrinter prints doctor's checks group by group as they come.
type checkPrinter struct {
	e      *Env
	group  string
	counts map[doctor.Status]int
}

func (p *checkPrinter) print(checks []doctor.Check) {
	if p.counts == nil {
		p.counts = map[doctor.Status]int{}
	}
	for _, c := range checks {
		if c.Group != p.group {
			p.group = c.Group
			fmt.Fprintln(p.e.Stdout, p.group)
		}
		p.counts[c.Status]++
		detail := c.Detail
		if c.Source != "" && c.Name != "local shell" {
			detail += " [" + c.Source + "]"
		}
		fmt.Fprintf(p.e.Stdout, "  %-4s  %-14s %s\n", c.Status, c.Name, detail)
	}
}

// waiting heads a group doctor is still checking, so a slow one
// (the code hosts) doesn't look frozen; its lines follow.
func (p *checkPrinter) waiting(group string) {
	p.group = group
	fmt.Fprintln(p.e.Stdout, group+" (checking…)")
}

func (p *checkPrinter) summary() {
	fmt.Fprintf(p.e.Stdout, "\n%d ok, %s, %s\n", p.counts[doctor.OK], plural(p.counts[doctor.Warn], "warning"), plural(p.counts[doctor.Fail], "failure"))
}

// libghosttySelftest feeds a line through the emulator, as tm selftest.
func libghosttySelftest() error {
	t, err := emu.New(80, 24)
	if err != nil {
		return err
	}
	defer t.Close()
	if _, err := t.Write([]byte("ok\r\n")); err != nil {
		return err
	}
	got, err := t.PlainText()
	if err != nil {
		return err
	}
	if got != "ok" {
		return fmt.Errorf("unexpected screen %q", got)
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
