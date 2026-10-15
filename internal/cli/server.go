package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/service"
	"github.com/theclifmeister/terminatr/internal/version"
)

const serverUsage = `usage: tm server run [--detached] | start [--no-launchd] | stop [--yes] [--force] | restart [--yes] [--no-launchd] | status [--json]
       tm server service install|uninstall [--print]`

// serverCmd implements `tm server …` (docs/SPEC.md §3.1).
func serverCmd(e *Env, args []string) error {
	if len(args) == 0 {
		return srvUsage("server", serverUsage)
	}
	switch args[0] {
	case "run":
		return serverRun(e, args[1:])
	case "start":
		return serverStart(e, args[1:])
	case "stop":
		err := serverStop(e, args[1:])
		if err == nil {
			unloadJob()
		}
		return err
	case "restart":
		var stop, start []string
		for _, a := range args[1:] {
			if a == "--no-launchd" || a == "-no-launchd" {
				start = append(start, a)
			} else {
				stop = append(stop, a)
			}
		}
		if err := serverStop(e, stop); err != nil {
			return err
		}
		return serverStart(e, start)
	case "status":
		return serverStatus(e, args[1:])
	case "service":
		return serverService(e, args[1:])
	}
	return srvUsage("server", serverUsage)
}

func serverRun(e *Env, args []string) error {
	f := newFlags()
	detached := f.Bool("detached")        // detach from the terminal and log to the server log
	launchd := f.Bool("launchd")          // started by launchd (macOS): log to the server log
	launchFile := f.String("launch-file") // with --launchd: the file holding the environment to run with
	if _, err := f.Parse(args); err != nil {
		return srvFail("server run", err)
	}
	var source string
	if *launchFile != "" {
		// Before the paths: the starting tm's environment says where
		// they are.
		var err error
		if source, err = service.ApplyLaunchFile(*launchFile); err != nil {
			return srvFail("server run", err)
		}
	}
	p, err := client.ResolvePaths()
	if err != nil {
		return srvFail("server run", err)
	}
	logger := log.New(e.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	sigs := []os.Signal{syscall.SIGTERM, syscall.SIGINT}
	if *launchd {
		// launchd owns the process: its stdout and stderr go to
		// logs/service.log, the server's log to the server log.
		lf, err := server.OpenLog(p)
		if err != nil {
			return srvFail("server run", err)
		}
		defer lf.Close()
		logger.SetOutput(lf)
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		go func() {
			for range hup {
			}
		}()
	} else if !*detached || !proc.Detached() {
		// Started by hand; a detached child of StartDetached logs it.
		warnSSH(e.Stderr, runtime.GOOS, e.Getenv)
	}
	if *detached {
		if !proc.Detached() {
			// Started by hand from a shell: get a fresh session first.
			if err := server.Respawn(); err != nil {
				return srvFail("server run", err)
			}
			return nil
		}
		lf, err := server.OpenLog(p)
		if err != nil {
			return srvFail("server run", err)
		}
		defer lf.Close()
		logger.SetOutput(lf)
		if err := proc.Detach(); err != nil {
			logger.Printf("detach: %v", err)
			return &exitError{ExitIO}
		}
		sigs = []os.Signal{syscall.SIGTERM}
	}
	ctx, stop := signal.NotifyContext(context.Background(), sigs...)
	defer stop()
	// A server started from inside a session (tm autostarting it there)
	// is no agent: its own calls (cli.run's commands) dial the human
	// socket, which a session's variables would make them skip.
	for _, k := range []string{caller.EnvSession, caller.EnvRole, caller.EnvProject, caller.EnvThread} {
		os.Unsetenv(k)
	}
	bin, _ := os.Executable()
	opts := server.Options{Paths: p, Log: logger, Bin: bin, Source: source, RunCLI: RunInServer}
	if len(os.Args) > 2 && os.Args[1] == "server" && os.Args[2] == "run" {
		// This process is `tm server run` (not a test calling in): the
		// server runs from its pin (docs/SPEC.md §3.6).
		opts.Exec, opts.Args = proc.Exec, os.Args[1:]
	}
	err = server.Run(ctx, opts)
	var running *server.AlreadyRunningError
	if errors.As(err, &running) {
		logger.Printf("%v", err)
		if !*detached {
			fmt.Fprintf(e.Stderr, "tm server run: %v\n", err)
		}
		return &exitError{ExitRefused}
	}
	if err != nil {
		logger.Printf("server: %v", err)
		return &exitError{ExitIO}
	}
	return nil
}

func serverStart(e *Env, args []string) error {
	f := newFlags()
	// macOS: start the server from this session, not launchd's (no
	// keychain over SSH).
	direct := f.Bool("no-launchd")
	if pos, err := f.Parse(args); err != nil || len(pos) > 0 {
		return srvUsage("server start", serverUsage)
	}
	c, p, err := connect(false)
	if err == nil {
		fmt.Fprintf(e.Stdout, "already running (pid %d)\n", c.Server.PID)
		c.Close()
		return nil
	}
	if !errors.Is(err, client.ErrNotRunning) {
		return srvFail("server start", err)
	}
	start := client.StartDetached
	if *direct {
		start = client.StartChild
	}
	if err := start(p); err != nil {
		return srvFail("server start", err)
	}
	c, err = client.Dial(p, proto.KindControl)
	if err != nil {
		return srvFail("server start", err)
	}
	defer c.Close()
	fmt.Fprintf(e.Stdout, "started (pid %d)\n", c.Server.PID)
	if *direct || !service.Wanted(runtime.GOOS, e.Getenv) {
		warnSSH(e.Stderr, runtime.GOOS, e.Getenv)
	}
	return nil
}

func serverStop(e *Env, args []string) error {
	f := newFlags()
	yes := f.Bool("yes")     // stop even while agent sessions run
	force := f.Bool("force") // SIGKILL a hung server (the pid that holds the lock)
	if _, err := f.Parse(args); err != nil {
		return srvFail("server stop", err)
	}
	if *force {
		p, err := client.ResolvePaths()
		if err != nil {
			return srvFail("server stop", err)
		}
		pid, err := client.ForceKill(p)
		if errors.Is(err, client.ErrNotRunning) {
			fmt.Fprintln(e.Stdout, "not running")
			return nil
		}
		if err != nil {
			return srvFail("server stop", err)
		}
		client.WaitStopped(p, 5*time.Second)
		os.Remove(p.Socket)
		os.Remove(p.PID)
		fmt.Fprintf(e.Stdout, "killed (pid %d)\n", pid)
		return nil
	}
	p, err := client.ResolvePaths()
	if err != nil {
		return srvFail("server stop", err)
	}
	// Stop works whatever protocol the server speaks: restart is how a
	// server of another version is replaced, so it must never refuse on
	// version (docs/SPEC.md §3.3).
	st, err := client.Stop(p, *yes)
	var perr *proto.Error
	if errors.As(err, &perr) && perr.Code == proto.ErrRefused && !*yes && isTTY(os.Stdin) {
		fmt.Fprintf(e.Stderr, "%s. Stop anyway? [y/N] ", perr.Message)
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		if a := strings.TrimSpace(strings.ToLower(line)); a != "y" && a != "yes" {
			return &exitError{ExitRefused}
		}
		st, err = client.Stop(p, true)
	}
	if errors.Is(err, client.ErrNotRunning) {
		fmt.Fprintln(e.Stdout, "not running")
		return nil
	}
	if err != nil {
		return srvFail("server stop", err)
	}
	pid := st.PID
	switch {
	case st.Signalled:
		fmt.Fprintf(e.Stderr, "tm server stop: the server (pid %d) could not be asked; sent it SIGTERM\n", pid)
	case st.Protocol != proto.Protocol:
		fmt.Fprintf(e.Stderr, "tm server stop: the server speaks protocol %d, this tm %d; stopped it all the same\n", st.Protocol, proto.Protocol)
	}
	// The lock goes first; the process exits a moment later.
	if !client.WaitStopped(p, server.StopGrace+5*time.Second) || !client.WaitExited(pid, 3*time.Second) {
		fmt.Fprintf(e.Stderr, "tm server stop: pid %d still running; see %s, or tm server stop --force kills it\n", pid, p.Log)
		return &exitError{ExitIO}
	}
	fmt.Fprintf(e.Stdout, "stopped (pid %d)\n", pid)
	return nil
}

func serverStatus(e *Env, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	if _, err := f.Parse(args); err != nil {
		return srvFail("server status", err)
	}
	c, _, err := connect(false)
	if errors.Is(err, client.ErrNotRunning) {
		if *asJSON {
			return e.printJSON(map[string]bool{"running": false})
		}
		fmt.Fprintln(e.Stdout, "not running")
		return &exitError{ExitRefused}
	}
	if err != nil {
		return srvFail("server status", err)
	}
	defer c.Close()
	var st proto.ServerStatus
	if err := c.Call(proto.MethodServerStatus, nil, &st); err != nil {
		return srvFail("server status", err)
	}
	if *asJSON {
		return e.printJSON(st)
	}
	fmt.Fprintf(e.Stdout, "pid       %d\n", st.PID)
	fmt.Fprintf(e.Stdout, "uptime    %s\n", time.Since(st.Started).Round(time.Second))
	fmt.Fprintf(e.Stdout, "version   %s (build %s)\n", st.Version, st.Build)
	fmt.Fprintf(e.Stdout, "protocol  %d\n", st.Protocol)
	fmt.Fprintf(e.Stdout, "sessions  %d\n", st.Sessions)
	fmt.Fprintf(e.Stdout, "socket    %s\n", st.Socket)
	fmt.Fprintf(e.Stdout, "home      %s\n", st.Home)
	for _, l := range peerLines(st.Link, time.Now()) {
		fmt.Fprintln(e.Stdout, l)
	}
	if st.PreviousShutdown == "crash" {
		fmt.Fprintf(e.Stdout, "note      the previous server crashed; lost sessions: %s\n", strings.Join(st.Lost, " "))
	}
	if st.Build != version.BuildID() {
		fmt.Fprintf(e.Stderr, "note: the server runs another build than this tm (%s); 'tm server restart' switches it to this one\n", version.BuildID())
	}
	return nil
}

// unloadJob boots out a dev or test home's on-demand launchd job after
// its server stopped, so it isn't left loaded (T71). Best effort.
func unloadJob() {
	p, err := client.ResolvePaths()
	if err != nil {
		return
	}
	if c, err := client.LaunchdConfig(p); err == nil {
		c.Unload()
	}
}

// peerLines are server status's lines for the server id and each link
// to a paired server; none before the server has an identity key.
func peerLines(st *proto.LinkStatus, now time.Time) []string {
	if st == nil {
		return nil
	}
	var out []string
	if st.ID != "" {
		out = append(out, fmt.Sprintf("server id %s (%s)", st.Short, st.ID))
	}
	if st.Error != "" {
		out = append(out, "peers     "+textsafe.Line(st.Error, 200))
	}
	for _, p := range st.Peers {
		line := fmt.Sprintf("peer      %s  %s · %s", p.Alias, linkWords(p, now), p.Transport)
		if p.State != proto.LinkUp && p.Error != "" {
			line += "\n            " + textsafe.Line(p.Error, 200)
		}
		out = append(out, line)
	}
	return out
}

// linkWords is a link's state for people: "up 3m0s · 42 ms · tm 0.27.0",
// "offline 4m0s · relay · dials again in 30s".
func linkWords(p proto.PeerLink, now time.Time) string {
	parts := []string{p.State}
	switch p.State {
	case proto.LinkUp:
		parts[0] += " " + ago(now, p.Since)
		if p.RTT > 0 {
			parts = append(parts, fmt.Sprintf("%.0f ms", max(p.RTT, 1)))
		}
		if p.Version != "" {
			parts = append(parts, "tm "+textsafe.Line(p.Version, 40))
		}
	case proto.LinkOffline:
		parts[0] += " " + ago(now, p.Since)
		if p.Reason != "" {
			parts = append(parts, p.Reason)
		}
		if !p.Retry.IsZero() {
			parts = append(parts, "dials again in "+p.Retry.Sub(now).Round(time.Second).String())
		}
	}
	return strings.Join(parts, " · ")
}

// ago is how long ago t was, to the second.
func ago(now, t time.Time) string {
	return now.Sub(t).Round(time.Second).String()
}
