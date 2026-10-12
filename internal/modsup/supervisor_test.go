package modsup

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/plat/sandbox"
)

// The test binary is the hosts' tm: run as `mod host …` it is one, as
// cmd/tm's modHost is.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "mod" && os.Args[2] == "host" {
		os.Exit(hostMain(os.Args[3:]))
	}
	os.Exit(m.Run())
}

func hostMain(args []string) int {
	fs := flag.NewFlagSet("tm mod host", flag.ContinueOnError)
	budget := fs.Duration("budget", modhost.DefaultBudget, "")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		return modproto.ExitUsage
	}
	out := os.Stdout
	os.Stdout = os.Stderr
	script, err := modhost.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return modproto.ExitLoad
	}
	_, _ = time.Now().Zone()
	if err := sandbox.Lockdown(); err != nil { // as cmd/tm's host does
		fmt.Fprintln(os.Stderr, err)
		return modproto.ExitLoad
	}
	return modhost.Run(modhost.Options{Name: fs.Arg(0), Script: script, Budget: *budget, In: os.Stdin, Out: out})
}

type fixture struct {
	t   *testing.T
	sup *Supervisor
	bus *modbus.Bus
	dir string
}

func newFixture(t *testing.T, edit func(*Options)) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, bus: modbus.New(), dir: dir}
	o := Options{
		Bin:      os.Args[0],
		CacheDir: filepath.Join(dir, "cache"), StateDir: filepath.Join(dir, "state"), LogDir: filepath.Join(dir, "logs"),
		Subscribe: func(id string, grants []string, projects func(string) bool) *modbus.Sub {
			return f.bus.Subscribe(id, modbus.Options{Grants: grants, Project: projects})
		},
		Logf:    t.Logf,
		API:     "1.0",
		Backoff: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond},
		Sample:  20 * time.Millisecond,
	}
	if edit != nil {
		edit(&o)
	}
	f.sup = New(o)
	t.Cleanup(f.sup.Close)
	return f
}

// mod writes a mod's main script and returns its spec.
func (f *fixture) mod(id, name, script string) Spec {
	f.t.Helper()
	dir := filepath.Join(f.dir, "mods", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(script), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return Spec{ID: id, Entry: p}
}

func (f *fixture) status(id string) Status {
	for _, st := range f.sup.Status() {
		if st.ID == id {
			return st
		}
	}
	return Status{}
}

func (f *fixture) waitState(id string, want State) Status {
	f.t.Helper()
	var st Status
	f.waitFor(fmt.Sprintf("%s %s", id, want), func() bool {
		st = f.status(id)
		return st.State == want
	})
	return st
}

func (f *fixture) waitFor(what string, cond func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s; status %+v", what, f.sup.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *fixture) log(id string) string {
	b, _ := os.ReadFile(filepath.Join(f.dir, "logs", id+".log"))
	return string(b)
}

func (f *fixture) waitLog(id, want string) {
	f.t.Helper()
	f.waitFor(fmt.Sprintf("%q in %s's log", want, id), func() bool { return strings.Contains(f.log(id), want) })
}

func (f *fixture) minute() {
	f.bus.Publish(modbus.ClockMinute, "", modbus.Clock{Now: time.Now()})
}

const minuteMod = `$.on("clock.minute", (d) => { $.log("minute", typeof d.now); });`

func TestStartDeliverStop(t *testing.T) {
	f := newFixture(t, nil)
	f.sup.Set([]Spec{f.mod("m", "main.ts", "const n: number = 1;\n"+minuteMod)})
	st := f.waitState("m", Running)
	if st.PID == 0 || len(st.Events) != 1 || st.Events[0] != modbus.ClockMinute {
		t.Fatalf("running status %+v", st)
	}
	f.minute()
	f.waitLog("m", "info: minute string")
	if fi, err := os.Stat(filepath.Join(f.dir, "state", "m")); err != nil || !fi.IsDir() {
		t.Fatalf("data folder: %v", err)
	}
	f.sup.Set(nil)
	f.waitFor("m gone", func() bool { return len(f.sup.Status()) == 0 })
	f.waitLog("m", "supervisor: stopped")
	f.waitFor("host exited", func() bool { return !proc.Alive(st.PID) })
	if strings.Contains(f.log("m"), "restarting") {
		t.Errorf("a stop restarted:\n%s", f.log("m"))
	}
}

func TestLazyStartFromLastHello(t *testing.T) {
	f := newFixture(t, nil)
	spec := f.mod("m", "main.js", minuteMod)
	f.sup.Set([]Spec{spec})
	f.waitState("m", Running) // first run: no hello known, starts at once
	f.sup.Set(nil)
	f.waitFor("m gone", func() bool { return len(f.sup.Status()) == 0 })

	f.sup.Set([]Spec{spec})
	st := f.waitState("m", Idle)
	time.Sleep(50 * time.Millisecond)
	if st = f.status("m"); st.State != Idle || st.PID != 0 {
		t.Fatalf("lazy mod started before an event: %+v", st)
	}
	f.bus.Publish(modbus.SessionState, "", map[string]string{"x": "y"}) // not one it handles
	time.Sleep(50 * time.Millisecond)
	if st = f.status("m"); st.State != Idle {
		t.Fatalf("an event it doesn't handle started it: %+v", st)
	}
	f.minute()
	f.waitState("m", Running)
	f.waitLog("m", "info: minute string") // the event that woke it is delivered
}

func TestReadyOnlyModStartsAtOnce(t *testing.T) {
	f := newFixture(t, nil)
	spec := f.mod("m", "main.js", `$.on("mod.ready", () => $.log("ready"));`)
	f.sup.Set([]Spec{spec})
	f.waitState("m", Running)
	f.sup.Set(nil)
	f.waitFor("m gone", func() bool { return len(f.sup.Status()) == 0 })
	f.sup.Set([]Spec{spec})
	f.waitState("m", Running) // no event would ever wake it
}

func TestCrashBackoffThenFailed(t *testing.T) {
	f := newFixture(t, nil)
	f.sup.Set([]Spec{f.mod("m", "main.js", `throw new Error("boom")`)})
	st := f.waitState("m", Failed)
	if st.Crashes != CrashLimit || !strings.Contains(st.Err, "failed to load") {
		t.Fatalf("failed status %+v", st)
	}
	log := f.log("m")
	for _, want := range []string{"restarting in 10ms", "restarting in 20ms", "failed: its script failed to load", "boom"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if !f.sup.Restart("m") {
		t.Fatal("Restart: not supervised")
	}
	f.waitFor("a fresh count", func() bool { st := f.status("m"); return st.State != Failed && st.Crashes < CrashLimit })
	f.waitState("m", Failed)
	if f.sup.Restart("nope") {
		t.Error("Restart of an unknown mod")
	}
}

func TestInterruptsRestartAtOnceThenFail(t *testing.T) {
	f := newFixture(t, func(o *Options) {
		o.Budget = 50 * time.Millisecond
		o.Backoff = []time.Duration{time.Hour} // a crash would stall the test
	})
	f.sup.Set([]Spec{f.mod("m", "main.js", `$.on("clock.minute", () => { for (;;) {} });`)})
	f.waitState("m", Running)
	f.waitFor("failed", func() bool {
		if f.status("m").State == Running {
			f.minute()
		}
		time.Sleep(10 * time.Millisecond)
		return f.status("m").State == Failed
	})
	st := f.status("m")
	if st.Interrupts != InterruptLimit || st.Crashes != 0 || !strings.Contains(st.Err, "in a row") {
		t.Fatalf("status %+v", st)
	}
	if log := f.log("m"); !strings.Contains(log, "interrupted") || !strings.Contains(log, "exit 3); restarting") {
		t.Errorf("log:\n%s", log)
	}
}

func TestRSSCeilingKills(t *testing.T) {
	var mu sync.Mutex
	big := false
	readRSS = func(pids []int) (map[int]uint64, error) {
		mu.Lock()
		defer mu.Unlock()
		out := map[int]uint64{}
		for _, p := range pids {
			out[p] = 10 << 20
			if big {
				out[p] = 300 << 20
			}
		}
		return out, nil
	}
	t.Cleanup(func() { readRSS = proc.RSS })
	f := newFixture(t, func(o *Options) { o.Backoff = []time.Duration{time.Hour} })
	f.sup.Set([]Spec{f.mod("m", "main.js", minuteMod)})
	st := f.waitState("m", Running)
	f.waitFor("an RSS sample", func() bool { return f.status("m").RSS == 10<<20 })
	mu.Lock()
	big = true
	mu.Unlock()
	st2 := f.waitState("m", Backoff)
	if !strings.Contains(st2.Err, "RSS of 300 MiB passed the 256 MiB ceiling") || st2.Crashes != 1 {
		t.Fatalf("status %+v", st2)
	}
	f.waitFor("host killed", func() bool { return !proc.Alive(st.PID) })
}

func TestRSSUnsupportedLogsOnce(t *testing.T) {
	readRSS = func([]int) (map[int]uint64, error) { return nil, errors.ErrUnsupported }
	t.Cleanup(func() { readRSS = proc.RSS })
	var mu sync.Mutex
	var lines []string
	f := newFixture(t, func(o *Options) {
		o.Logf = func(format string, args ...any) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(format, args...))
			mu.Unlock()
		}
	})
	f.sup.Set([]Spec{f.mod("m", "main.js", minuteMod)})
	f.waitState("m", Running)
	f.waitFor("the log line", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(strings.Join(lines, "\n"), "no RSS ceiling")
	})
}

func TestBundledModRunsPrecompiled(t *testing.T) {
	f := newFixture(t, nil)
	f.sup.Set([]Spec{{ID: "b", Bundle: []byte(minuteMod)}})
	f.waitState("b", Running)
	f.minute()
	f.waitLog("b", "info: minute string")
	js, _ := filepath.Glob(filepath.Join(f.dir, "cache", "b", "*.js"))
	if len(js) != 1 {
		t.Fatalf("bundles in the cache: %v", js)
	}
}

func TestLaterSubscribeAndChangedSpec(t *testing.T) {
	f := newFixture(t, nil)
	spec := f.mod("m", "main.js", `setTimeout(() => $.on("clock.minute", () => $.log("late")), 1);`)
	f.sup.Set([]Spec{spec})
	f.waitFor("late subscribe", func() bool {
		st := f.status("m")
		return st.State == Running && len(st.Events) == 1
	})
	f.minute()
	f.waitLog("m", "info: late")
	pid := f.status("m").PID

	spec.Settings = map[string]any{"warn": 2}
	f.sup.Set([]Spec{spec})
	f.waitState("m", Idle) // the new host starts lazily, on the event it handled
	f.minute()
	f.waitFor("a new host", func() bool { st := f.status("m"); return st.State == Running && st.PID != pid })
}

func TestCloseStopsHosts(t *testing.T) {
	f := newFixture(t, nil)
	f.sup.Set([]Spec{f.mod("a", "main.js", minuteMod), f.mod("b", "main.js", minuteMod)})
	pa, pb := f.waitState("a", Running).PID, f.waitState("b", Running).PID
	f.sup.Close()
	if proc.Alive(pa) || proc.Alive(pb) {
		t.Fatal("a host outlived Close")
	}
	f.sup.Set([]Spec{f.mod("c", "main.js", minuteMod)}) // ignored after Close
	if st := f.status("c"); st.ID != "" {
		t.Fatalf("Set after Close started %+v", st)
	}
}

func TestHostSandboxed(t *testing.T) {
	f := newFixture(t, nil)
	f.sup.Set([]Spec{f.mod("m", "main.ts", minuteMod)})
	st := f.waitState("m", Running)
	f.minute()
	f.waitLog("m", "info: minute string")
	if err := sandbox.Check(); err != nil {
		if !strings.HasPrefix(st.Sandbox, "none: ") {
			t.Fatalf("sandbox unavailable (%v), status says %q", err, st.Sandbox)
		}
		t.Skipf("no OS sandbox here: %v", err)
	}
	if st.Sandbox == "" || strings.HasPrefix(st.Sandbox, "none") {
		t.Fatalf("host not sandboxed: %q", st.Sandbox)
	}
}

func TestHostWithoutSandbox(t *testing.T) {
	old := startSandboxed
	startSandboxed = func(*exec.Cmd, sandbox.Policy) (*sandbox.Proc, error) {
		return nil, fmt.Errorf("%w: test", sandbox.ErrUnavailable)
	}
	t.Cleanup(func() { startSandboxed = old })
	f := newFixture(t, nil)
	f.sup.Set([]Spec{f.mod("m", "main.ts", minuteMod)})
	st := f.waitState("m", Running)
	if want := "none: " + sandbox.ErrUnavailable.Error() + ": test"; st.Sandbox != want {
		t.Fatalf("sandbox %q, want %q", st.Sandbox, want)
	}
	f.waitLog("m", "runs without the OS sandbox")
	f.minute()
	f.waitLog("m", "info: minute string")
}
