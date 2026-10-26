package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/vault"
	"github.com/theclifmeister/terminatr/internal/version"
)

const updateUsage = `usage: tm update [--check] [--yes] [--restart] [--force] [--app] [--json]`

func init() {
	commands["update"] = func(e *Env, args []string) error { return updateCmd(e, args, defaultUpdater(e)) }
}

// updater is everything `tm update` touches outside its arguments, so
// tests can replace it.
type updater struct {
	Exe, Channel, Version, Team string
	GOOS, GOARCH                string
	Client                      *update.Client
	// Verify checks a downloaded binary (macOS: codesign); nil skips.
	Verify update.Verifier
	// TTY says whether stdin is a terminal, for the questions.
	TTY bool
	// RunVersion runs `<bin> version` and returns its output.
	RunVersion func(bin string) (string, error)
	// Brew runs brew with args, on the user's terminal.
	Brew func(args ...string) error
	// Server describes the running server; ok is false when none runs.
	// Sessions is -1 for a server of an older protocol, which only says
	// hello.
	Server func() (st proto.ServerStatus, ok bool)
	// Restart runs `<bin> server restart [--yes]` on the user's terminal.
	Restart func(bin string, yes bool) int
	// YAML reports whether the projects' front matter has moved to YAML
	// (vault.Migrated); nil is no.
	YAML func() bool
	// FindApp finds the installed Mac app (update.FindApp); nil, off
	// macOS, leaves the app alone.
	FindApp func() (update.App, bool)
	// AppPlaces are where a new app may go, the first writable one
	// (update.AppPlaces).
	AppPlaces []string
	// VerifyApp checks a downloaded app (update.VerifyApp); nil skips.
	VerifyApp update.Verifier
}

func defaultUpdater(e *Env) *updater {
	exe, _ := os.Executable()
	interactive := func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(name, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, e.Stdout, e.Stderr
		return cmd
	}
	u := &updater{
		Exe: exe, Channel: version.Channel, Version: version.Version, Team: version.TeamID,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Client: update.NewClient(e.Getenv),
		Verify: update.DefaultVerifier(),
		TTY:    stdinTTY(e),
		RunVersion: func(bin string) (string, error) {
			out, err := exec.Command(bin, "version").CombinedOutput()
			return strings.TrimSpace(string(out)), err
		},
		Brew: func(args ...string) error { return interactive("brew", args...).Run() },
		Server: func() (proto.ServerStatus, bool) {
			c, _, err := connect(false)
			var verr *proto.MismatchError
			if errors.As(err, &verr) {
				// An older server: the hello says enough to offer the
				// restart, which works whatever its protocol.
				h := verr.Server
				return proto.ServerStatus{PID: h.PID, Version: h.Version, Build: h.Build, Protocol: h.Protocol, Sessions: -1}, true
			}
			if err != nil {
				return proto.ServerStatus{}, false
			}
			defer c.Close()
			var st proto.ServerStatus
			if c.Call(proto.MethodServerStatus, nil, &st) != nil {
				return proto.ServerStatus{}, false
			}
			return st, true
		},
		YAML: vault.Migrated,
		Restart: func(bin string, yes bool) int {
			args := []string{"server", "restart"}
			if yes {
				args = append(args, "--yes")
			}
			err := interactive(bin, args...).Run()
			var x *exec.ExitError
			if errors.As(err, &x) {
				return x.ExitCode()
			}
			if err != nil {
				fmt.Fprintf(e.Stderr, "tm update: %v\n", err)
				return ExitIO
			}
			return ExitOK
		},
	}
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		u.AppPlaces = update.AppPlaces(home)
		u.FindApp = func() (update.App, bool) { return update.FindApp(u.AppPlaces, update.CaskRooms) }
		u.VerifyApp = update.VerifyApp
	}
	return u
}

// updateCheck is `tm update --check --json`.
type updateCheck struct {
	update.Install
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	Newer   bool   `json:"newer"`
	Error   string `json:"error,omitempty"`
	// App is the Mac app, on macOS.
	App *appCheck `json:"app,omitempty"`
}

// appCheck is the Mac app in `tm update --check --json`.
type appCheck struct {
	Installed bool `json:"installed"`
	update.App
	// Newer: the latest release's app is newer than the installed one
	// (or its version is unknown).
	Newer bool `json:"newer"`
}

// updateOpts are tm update's flags that act.
type updateOpts struct {
	yes, restart, force, app bool
}

// updateCmd implements `tm update` (docs/SPEC.md §10.1). It never stops
// the server on its own: the old server keeps running its binary, and
// restarting it is a question on a terminal or an explicit --restart.
func updateCmd(e *Env, args []string, u *updater) error {
	f := newFlags()
	check := f.Bool("check")     // only report whether a newer release exists
	yes := f.Bool("yes")         // install without asking
	restart := f.Bool("restart") // restart the server afterwards (agents are resumed)
	force := f.Bool("force")     // install a release that can't read YAML front matter anyway
	app := f.Bool("app")         // macOS: install the Mac app if it isn't installed
	asJSON := f.Bool("json")     // with --check: print JSON
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 || *asJSON && !*check {
		return srvUsage("update", updateUsage)
	}
	in := update.Detect(u.Exe, u.Channel)
	res := updateCheck{Install: in, Current: u.Version}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var rel update.Release
	if in.Method != update.Dev || *check {
		var err error
		if rel, err = u.Client.Latest(ctx); err != nil {
			res.Error = err.Error()
		} else {
			res.Latest = rel.Tag
			res.Newer, _ = update.Newer(rel.Tag, u.Version)
		}
	}
	if *check {
		if u.FindApp != nil {
			a, ok := u.FindApp()
			res.App = &appCheck{Installed: ok, App: a}
			if ok && res.Latest != "" {
				res.App.Newer = appNewer(res.Latest, a)
			}
		}
		if *asJSON {
			return e.printJSON(res)
		}
		fmt.Fprintf(e.Stdout, "installed  %s (%s, %s)\n", u.Version, in.Method, in.Path)
		switch {
		case res.Error != "":
			fmt.Fprintf(e.Stdout, "latest     unknown: %s\n", res.Error)
			return &exitError{ExitIO}
		case res.Newer:
			fmt.Fprintf(e.Stdout, "latest     %s: %s\n", rel.Tag, updateHint(in))
		default:
			fmt.Fprintf(e.Stdout, "latest     %s: up to date\n", rel.Tag)
		}
		if a := res.App; a != nil {
			switch {
			case !a.Installed:
				fmt.Fprintln(e.Stdout, "app        not installed: tm update --app installs it")
			case a.Newer:
				fmt.Fprintf(e.Stdout, "app        %s (%s, %s): %s\n", orUnknown(a.Version), a.Method, a.Path, appHint(a.App))
			default:
				fmt.Fprintf(e.Stdout, "app        %s (%s, %s): up to date\n", a.Version, a.Method, a.Path)
			}
		}
		return nil
	}

	opts := updateOpts{yes: *yes, restart: *restart, force: *force, app: *app}
	if err := u.updateTM(e, in, rel, res, opts); err != nil {
		return err
	}
	// The Mac app follows tm: not for a source build (its app is built
	// from source too), nor without a release to compare it with.
	if u.FindApp == nil || in.Method == update.Dev || res.Error != "" {
		return nil
	}
	return u.updateApp(e, in, rel, opts)
}

// updateTM updates tm itself.
func (u *updater) updateTM(e *Env, in update.Install, rel update.Release, res updateCheck, opts updateOpts) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch in.Method {
	case update.Dev:
		fmt.Fprintf(e.Stderr, "tm update: this tm (%s) was built from source; update it with git pull && make\n", u.Version)
		return &exitError{ExitRefused}
	case update.Homebrew:
		if res.Error == "" && !res.Newer {
			fmt.Fprintf(e.Stdout, "tm %s is the latest release\n", u.Version)
			return nil
		}
		if err := u.yamlGuard(e, res.Latest, opts.force); err != nil {
			return err
		}
		// Homebrew owns its files: never replace them, let brew do it.
		fmt.Fprintf(e.Stdout, "tm was installed with Homebrew (%s); it updates with:\n  %s\n", in.Path, in.Upgrade)
		if !opts.yes && !(u.TTY && ask(e, "Run it now? [y/N] ")) {
			return nil
		}
		if err := u.Brew("upgrade", update.Formula); err != nil {
			fmt.Fprintf(e.Stderr, "tm update: brew upgrade: %v\n", err)
			return &exitError{ExitIO}
		}
		return u.afterUpdate(e, brewBin(in.Path), res.Latest, opts.restart)
	}

	if res.Error != "" {
		fmt.Fprintf(e.Stderr, "tm update: %s\n", res.Error)
		return &exitError{ExitIO}
	}
	if !res.Newer {
		fmt.Fprintf(e.Stdout, "tm %s is the latest release\n", u.Version)
		return nil
	}
	if err := u.yamlGuard(e, rel.Tag, opts.force); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "tm %s is available (installed: %s at %s)\n", rel.Tag, u.Version, in.Path)
	if !opts.yes {
		if !u.TTY {
			fmt.Fprintln(e.Stderr, "tm update: not a terminal; pass --yes to install it")
			return &exitError{ExitRefused}
		}
		if !ask(e, "Install it? [y/N] ") {
			fmt.Fprintln(e.Stdout, "nothing changed")
			return &exitError{ExitRefused}
		}
	}
	dir := filepath.Dir(in.Path)
	update.CleanOld(dir, u.GOOS)
	staged, err := u.Client.Download(ctx, rel, u.GOOS, u.GOARCH, dir)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			err = fmt.Errorf("%w: %s is not writable; reinstall where you can write, or update as its owner", err, dir)
		}
		fmt.Fprintf(e.Stderr, "tm update: %v\n", err)
		return &exitError{ExitIO}
	}
	defer staged.Remove() // gone after the rename; cleans up on failure
	fmt.Fprintf(e.Stdout, "checksum   ok (%s)\n", update.ArchiveName(u.GOOS, u.GOARCH))
	tmp := staged.Program()
	if u.Verify != nil {
		team, err := u.Verify(tmp, u.Team)
		if err != nil {
			fmt.Fprintf(e.Stderr, "tm update: the downloaded tm fails the signature check: %v; nothing changed\n", err)
			return &exitError{ExitRefused}
		}
		fmt.Fprintf(e.Stdout, "signature  ok (Developer ID, team %s)\n", team)
	}
	if out, err := u.RunVersion(tmp); err != nil || !strings.Contains(out, rel.Tag) {
		fmt.Fprintf(e.Stderr, "tm update: the downloaded tm doesn't run as %s (%v): %s; nothing changed\n", rel.Tag, err, out)
		return &exitError{ExitRefused}
	}
	if err := staged.Install(dir); err != nil {
		fmt.Fprintf(e.Stderr, "tm update: %v\n", err)
		return &exitError{ExitIO}
	}
	fmt.Fprintf(e.Stdout, "installed  %s at %s\n", rel.Tag, in.Path)
	return u.afterUpdate(e, in.Path, rel.Tag, opts.restart)
}

// yamlGuard refuses to install release tag when it can't read YAML front
// matter (older than vault.YAMLSince) and the projects' files have moved
// to it: such a tm would read a YAML TASKS.md as having no front matter
// and hand out task ids from T1 again. force installs it anyway.
func (u *updater) yamlGuard(e *Env, tag string, force bool) error {
	older, ok := update.Newer(vault.YAMLSince, tag)
	if !ok || !older || u.YAML == nil || !u.YAML() {
		return nil
	}
	if force {
		fmt.Fprintf(e.Stderr, "tm update: warning: tm %s can't read YAML front matter, which the projects' files have (state/front-yaml); installing it anyway (--force)\n", tag)
		return nil
	}
	fmt.Fprintf(e.Stderr, "tm update: tm %s is older than %s and can't read YAML front matter, which the projects' files have had since the move (state/front-yaml): it would lose TASKS.md's next_id; nothing changed (--force installs it anyway)\n", tag, vault.YAMLSince)
	return &exitError{ExitRefused}
}

// brewBin is the tm Homebrew links into its prefix's bin, which names
// the newest keg after an upgrade: .../Cellar/terminatr/0.2.0/bin/tm
// becomes .../bin/tm.
func brewBin(keg string) string {
	if prefix, _, ok := strings.Cut(keg, "/Cellar/"); ok {
		return filepath.Join(prefix, "bin", "tm")
	}
	return keg
}

// afterUpdate deals with a server that still runs the old binary: it
// says so, and restarts it only on --restart or a y on a terminal,
// because a restart ends the agents' running turns.
func (u *updater) afterUpdate(e *Env, bin, newVersion string, restart bool) error {
	st, ok := u.Server()
	if !ok || newVersion != "" && st.Version == newVersion {
		return nil
	}
	sessions := plural(st.Sessions, "session")
	if st.Sessions < 0 {
		sessions = "its sessions"
	}
	fmt.Fprintf(e.Stdout, "\nThe server (pid %d) still runs %s with %s; it keeps the old binary until it restarts.\n",
		st.PID, st.Version, sessions)
	fmt.Fprintln(e.Stdout, "Restarting stops every session: agents are resumed but lose the turn they are in, shells are lost.")
	code := ExitOK
	switch {
	case restart:
		code = u.Restart(bin, true)
	case u.TTY && ask(e, "Restart the server now? [y/N] "):
		// No --yes: the restart itself still asks when agents are working.
		code = u.Restart(bin, false)
	default:
		fmt.Fprintln(e.Stdout, "Restart it when convenient: tm server restart")
	}
	if code != ExitOK {
		return &exitError{code}
	}
	return nil
}

func updateHint(in update.Install) string {
	switch in.Method {
	case update.Homebrew:
		return in.Upgrade
	case update.Dev:
		return "git pull && make"
	}
	return "tm update"
}

// ask prints a question on stderr and reads a y/yes. It reads byte by
// byte, so a second question still finds its own line on stdin.
func ask(e *Env, q string) bool {
	a := strings.ToLower(answer(e, q))
	return a == "y" || a == "yes"
}

// answer prints a question on stderr and reads a line of stdin, byte by
// byte, trimmed.
func answer(e *Env, q string) string {
	fmt.Fprint(e.Stderr, q)
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := e.Stdin.Read(b)
		if n == 1 && b[0] != '\n' {
			line = append(line, b[0])
		}
		if err != nil || n == 1 && b[0] == '\n' {
			break
		}
	}
	return strings.TrimSpace(string(line))
}
