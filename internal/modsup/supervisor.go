// Package modsup is the mod supervisor (mods design §3, §7, §8): it runs
// each enabled code mod in a `tm mod host` child of the server, feeds it
// its events from the event bus, and keeps it within its limits.
//
// A mod's host starts lazily: once its last host said which events it
// handles, the next one starts on the first of them, not with the server.
// It runs in its own data folder with an empty environment, and its
// stderr, log lines and failed handlers go to its log. A host that
// crashes is restarted after 1 s, 5 s and 30 s; a third crash within
// 5 minutes leaves the mod failed until it is changed or restarted. A
// host the CPU budget interrupted (exit 3) is restarted at once, being
// the only safe state after an interrupt (SPEC §3.7); three interrupted
// handlers in a row fail the mod too. A host whose resident set passes
// the ceiling (256 MiB; goja has no heap cap, so it is the only memory
// limit) or that stops answering is killed, which counts as a crash.
// A failed mod's areas go blank; nothing else waits on it.
package modsup

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

// Defaults of the limits (mods design §7, §8).
const (
	// DefaultRSSLimit is the resident set a host may reach.
	DefaultRSSLimit = 256 << 20
	// CrashLimit crashes within CrashWindow fail the mod.
	CrashLimit  = 3
	CrashWindow = 5 * time.Minute
	// InterruptLimit interrupted handlers in a row fail the mod.
	InterruptLimit = 3
	// HelloLimit is how long a host has to load its script and answer
	// hello.
	HelloLimit = 15 * time.Second
	// StopGrace is how long a host has to exit after shutdown.
	StopGrace = 2 * time.Second
)

// DefaultBackoff are the waits before restarting after the first and
// second crash in the window (design §8: 1 s, 5 s, 30 s).
var DefaultBackoff = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

// State is where a supervised mod stands (design §8).
type State string

const (
	// Idle: enabled, its host not started until its first event.
	Idle State = "idle"
	// Starting: the host runs its script, before its hello answer.
	Starting State = "starting"
	// Running: the host gets its events.
	Running State = "running"
	// Backoff: the host crashed and is restarted at Status.Next.
	Backoff State = "backoff"
	// Failed: crashed too often; stays so until the mod changes or
	// Restart.
	Failed State = "failed"
)

// Spec is one code mod to run.
type Spec struct {
	ID string
	// Entry is the mod's main (a .ts or .js file), bundled with
	// modhost.Build into the mod's cache folder before each start.
	Entry string
	// Bundle, when set, is a bundled mod's script, precompiled when tm
	// was built: it runs as is, without esbuild, and Entry is unused.
	Bundle []byte
	// Grants are its granted permissions (granted ∩ requested).
	Grants []string
	// Projects are the projects it runs for; empty for all.
	Projects []string
	// Settings are its options, sent in hello.
	Settings map[string]any
	// Eager starts the host with the supervisor instead of on its first
	// event: a bundled mod that draws on the sidebar, whose cells shown
	// from the last run would otherwise stay stale until something
	// changes (a lazy host never gets mod.ready's snapshot before).
	Eager bool
}

// Status is one supervised mod's state, for the Mods tab, tm mod list and
// tm doctor.
type Status struct {
	ID    string
	State State
	// Since is when it came to State.
	Since time.Time
	// PID is the running host's; 0 when none runs.
	PID int
	// RSS is the host's resident set when last sampled, in bytes.
	RSS uint64
	// Events are what its last host handles.
	Events []string
	// Restarts counts the host's restarts since the mod was set.
	Restarts int
	// Crashes in the last CrashWindow, and Interrupts in a row.
	Crashes, Interrupts int
	// Next is when a host in backoff restarts.
	Next time.Time
	// Err is why the last host ended, or why the mod failed.
	Err string
	// Sandbox is the OS sandbox its last host ran in (sandbox.Mode, as
	// "seatbelt"), or "none: <why>"; empty before a host started.
	Sandbox string
}

// Options configure a supervisor. The zero value of each limit is its
// default.
type Options struct {
	// Bin is the tm binary that runs `tm mod host`; empty is this one.
	Bin string
	// CacheDir holds each mod's bundle and last hello (<CacheDir>/<id>),
	// StateDir each mod's data folder, its working directory
	// (<StateDir>/<id>), and LogDir its log (<LogDir>/<id>.log).
	CacheDir, StateDir, LogDir string
	// Subscribe opens a mod's event queue (Server.SubscribeMod). It is
	// called again at every (re)start: the new queue starts with
	// mod.ready, never with what the old one held.
	Subscribe func(id string, grants []string, projects func(slug string) bool) *modbus.Sub
	// Broker answers a host's $ API request on conn; nil answers
	// method-not-found.
	Broker func(id string, conn *modproto.Conn, m *modproto.Message)
	// Logf is the server's log.
	Logf func(format string, args ...any)
	// Hello fills a hello's tm and API versions.
	TMVersion, API string

	Budget      time.Duration   // each loop slice's CPU budget; modhost.DefaultBudget
	RSSLimit    uint64          // DefaultRSSLimit
	Backoff     []time.Duration // DefaultBackoff
	CrashWindow time.Duration   // CrashWindow
	// Sample is how often the hosts' RSS is read (2 s), and Ping how
	// often each host must answer stats (30 s, within 10 s).
	Sample, Ping, PingLimit time.Duration
	// StopGrace is how long a host has to exit after shutdown (StopGrace).
	StopGrace time.Duration
}

// Supervisor runs the code mods it is given.
type Supervisor struct {
	o      Options
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu   sync.Mutex
	mods map[string]*runner
}

// New starts a supervisor that runs no mod until Set.
func New(o Options) *Supervisor {
	if o.Bin == "" {
		o.Bin, _ = os.Executable()
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if o.Budget <= 0 {
		o.Budget = modhost.DefaultBudget
	}
	if o.RSSLimit == 0 {
		o.RSSLimit = DefaultRSSLimit
	}
	if len(o.Backoff) == 0 {
		o.Backoff = DefaultBackoff
	}
	if o.CrashWindow <= 0 {
		o.CrashWindow = CrashWindow
	}
	if o.Sample <= 0 {
		o.Sample = 2 * time.Second
	}
	if o.Ping <= 0 {
		o.Ping = 30 * time.Second
	}
	if o.PingLimit <= 0 {
		o.PingLimit = 10 * time.Second
	}
	if o.StopGrace <= 0 {
		o.StopGrace = StopGrace
	}
	s := &Supervisor{o: o, mods: map[string]*runner{}}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sampleRSS()
	}()
	return s
}

// Set makes specs the mods that run: a new one starts (lazily unless
// Eager), one that
// changed is stopped and started again (which also clears its failure),
// and one no longer listed is stopped. It returns at once.
func (s *Supervisor) Set(specs []Spec) {
	want := map[string]Spec{}
	for _, sp := range specs {
		want[sp.ID] = sp
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	old := map[string]<-chan struct{}{}
	for id, r := range s.mods {
		if sp, ok := want[id]; !ok || !reflect.DeepEqual(sp, r.spec) {
			r.stop()
			old[id] = r.done
			delete(s.mods, id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(want)) {
		if _, ok := s.mods[id]; !ok {
			s.start(want[id], want[id].Eager, old[id])
		}
	}
}

// start runs sp's runner once after is closed (the runner it replaces
// has ended); s.mu held. eager skips the lazy start.
func (s *Supervisor) start(sp Spec, eager bool, after <-chan struct{}) {
	r := newRunner(s, sp, after)
	s.mods[sp.ID] = r
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		r.run(eager)
	}()
}

// Restart starts mod id's host again now: a failed mod gets a fresh
// count, an idle one starts. It reports whether the mod is supervised.
func (s *Supervisor) Restart(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.mods[id]
	if !ok || s.ctx.Err() != nil {
		return false
	}
	r.stop()
	s.start(r.spec, true, r.done)
	return true
}

// CallTool runs agent tool call.Tool of mod id in its host, starting an
// idle host first, and returns the text the mod's handler gave, or why
// it didn't: the mod isn't supervised, failed, is restarting, its host
// ended, ctx ended, or the handler's error (a *modproto.Error).
func (s *Supervisor) CallTool(ctx context.Context, id string, call modproto.ToolCall) (modproto.ToolResult, error) {
	s.mu.Lock()
	r, ok := s.mods[id]
	s.mu.Unlock()
	if !ok {
		return modproto.ToolResult{}, fmt.Errorf("mod %s doesn't run: only an enabled code mod has a host", id)
	}
	return r.callTool(ctx, call)
}

// Status lists the supervised mods, by id.
func (s *Supervisor) Status() []Status {
	s.mu.Lock()
	rs := slices.Collect(maps.Values(s.mods))
	s.mu.Unlock()
	out := make([]Status, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.status())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Close stops every host (shutdown, then a kill after StopGrace) and
// returns once they have exited.
func (s *Supervisor) Close() {
	s.mu.Lock()
	for _, r := range s.mods {
		r.stop()
	}
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

// sampleRSS reads the running hosts' resident sets every Sample and kills
// a host over the ceiling.
func (s *Supervisor) sampleRSS() {
	t := time.NewTicker(s.o.Sample)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		s.mu.Lock()
		hosts := map[int]*runner{}
		for _, r := range s.mods {
			if pid := r.pid(); pid > 0 {
				hosts[pid] = r
			}
		}
		s.mu.Unlock()
		if len(hosts) == 0 {
			continue
		}
		got, err := readRSS(slices.Collect(maps.Keys(hosts)))
		if errors.Is(err, errors.ErrUnsupported) {
			s.o.Logf("mods: no RSS ceiling on this OS: %v", err)
			return
		}
		if err != nil {
			s.o.Logf("mods: reading the hosts' RSS: %v", err)
			continue
		}
		for pid, n := range got {
			hosts[pid].sampled(pid, n)
		}
	}
}

// cacheDir, stateDir and logPath are mod id's folders and log.
func (s *Supervisor) cacheDir(id string) string { return filepath.Join(s.o.CacheDir, id) }
func (s *Supervisor) stateDir(id string) string { return filepath.Join(s.o.StateDir, id) }
func (s *Supervisor) logPath(id string) string  { return filepath.Join(s.o.LogDir, id+".log") }
