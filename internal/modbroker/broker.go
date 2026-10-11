// Package modbroker is the server's broker for user mods (the mods design
// §4 and §7): every $ call a code mod's host makes comes here by its
// method name ("fs.read", "inbox.raise"), is checked against what the mod
// runs with (granted ∩ requested, from the registry) and is done by the
// broker itself, so a mod host needs no file, network or process access
// of its own. A mod's caller is caller.Mod: what a call changes in a
// project is journaled there as mod:<id>, and every call outside tm (a
// file written, a fetch, a program run) and every refusal goes to the
// broker's audit line, the mod's log.
//
// There is no permission for credentials: the paths a files grant
// reaches never include a credential store, a keychain, a .env file, a
// repository's .git or tm's own folder (Paths), a net grant reaches only
// its HTTPS hosts at public addresses (CheckURL), and a commands grant
// runs its programs with an environment that carries no tokens (Run).
package modbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

// Error codes a refused or failed call answers with: the JSON-RPC errors
// of the mod host's wire (modproto).
const (
	CodeNoMethod  = modproto.CodeMethodNotFound
	CodeBadParams = modproto.CodeInvalidParams
	CodeDenied    = modproto.CodeDenied
	CodeNotFound  = modproto.CodeNotFound
	CodeFailed    = modproto.CodeFailed
)

// Error is a call's refusal or failure, said to the mod.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Denied, BadParams, NotFound and Failed make the Error of each kind.
func Denied(format string, a ...any) *Error { return &Error{CodeDenied, fmt.Sprintf(format, a...)} }
func BadParams(format string, a ...any) *Error {
	return &Error{CodeBadParams, fmt.Sprintf(format, a...)}
}
func NotFound(format string, a ...any) *Error { return &Error{CodeNotFound, fmt.Sprintf(format, a...)} }
func Failed(format string, a ...any) *Error   { return &Error{CodeFailed, fmt.Sprintf(format, a...)} }

// IsDenied reports whether err is a refusal.
func IsDenied(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == CodeDenied
}

// Call is one brokered call, as a Handler sees it.
type Call struct {
	// Mod is the calling mod as the registry has it now: enabled, with
	// its grants, projects and settings.
	Mod mod.Mod
	// Caller is caller.Mod for it: journal what the call changes with it.
	Caller caller.Caller
	Method string
	Params json.RawMessage
	// Paths are the file rules for this mod: its own data folder, and the
	// roots of its files grant.
	Paths Paths
	b     *Broker
}

// Decode reads the params into v; none read as {}.
func (c *Call) Decode(v any) error {
	p := c.Params
	if len(p) == 0 || string(p) == "null" {
		p = json.RawMessage("{}")
	}
	if err := json.Unmarshal(p, v); err != nil {
		return BadParams("%s: %v", c.Method, err)
	}
	return nil
}

// Project refuses project slug unless the mod runs for it: one of its
// projects, or any when it lists none.
func (c *Call) Project(slug string) error {
	if slug == "" {
		return BadParams("%s: no project", c.Method)
	}
	if !c.Mod.Runs(slug) {
		return Denied("%s doesn't run for project %s", c.Mod.ID, slug)
	}
	return nil
}

// Audit writes one line to the mod's audit log.
func (c *Call) Audit(format string, a ...any) { c.b.audit(c.Mod.ID, fmt.Sprintf(format, a...)) }

// Handler does one method's work, once the broker has checked the mod
// may call it. Errors that aren't an *Error are said as CodeFailed.
type Handler func(ctx context.Context, c *Call) (any, error)

// method is a Handler and the grant it needs ("" for none).
type method struct {
	needs string
	fn    Handler
}

// Options are what a Broker works from.
type Options struct {
	// Mod returns installed mod id as the registry has it now.
	Mod func(id string) (mod.Mod, bool)
	// Home is the user's home folder; TMHome is ~/.terminatr; StateDir is
	// where each mod's own folder is (~/.terminatr/state/mods).
	Home, TMHome, StateDir string
	// Secrets are the credential stores no mod reaches (agent.Secrets);
	// the broker adds the keychains (Keychains).
	Secrets []string
	// Audit writes one line about mod id; nil drops it.
	Audit func(id, line string)
	// Net overrides how fetches reach the network (tests).
	Net *NetOptions
}

// Broker answers mods' calls.
type Broker struct {
	o       Options
	secrets []string
	mu      sync.RWMutex
	methods map[string]method
	store   sync.Mutex // one store write at a time
	net     *fetcher
}

// New is a Broker with the methods every mod has: settings, its store,
// its files, and fetch and run under their grants. The server adds the
// project methods (Handle).
func New(o Options) *Broker {
	b := &Broker{o: o, methods: map[string]method{}}
	b.secrets = Real(append(append([]string{}, o.Secrets...), Keychains(o.Home, os.Getenv)...))
	b.net = newFetcher(o.Net)
	b.Handle("settings", "", func(_ context.Context, c *Call) (any, error) { return c.Mod.Settings, nil })
	b.Handle("store.get", "", b.storeGet)
	b.Handle("store.set", "", b.storeSet)
	b.Handle("store.del", "", b.storeDel)
	b.Handle("fs.read", "", fsRead)
	b.Handle("fs.write", "", fsWrite)
	b.Handle("http.fetch", mod.PermNet, b.fetch)
	b.Handle("process.run", mod.PermCommands, run)
	return b
}

// Handle adds method name, which needs grant needs ("" for none). A
// second Handle of a name replaces the first.
func (b *Broker) Handle(name, needs string, fn Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.methods[name] = method{needs: needs, fn: fn}
}

// Methods lists the method names, sorted.
func (b *Broker) Methods() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.methods))
	for n := range b.methods {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Call answers mod id's call of method with params. The mod must be
// installed and enabled, and have the method's grant; the handler checks
// the rest (paths, hosts, programs, projects). A refusal is audited.
func (b *Broker) Call(ctx context.Context, id, name string, params json.RawMessage) (any, error) {
	res, err := b.call(ctx, id, name, params)
	if err != nil && IsDenied(err) {
		b.audit(id, fmt.Sprintf("denied %s: %v", name, err))
	}
	return res, err
}

func (b *Broker) call(ctx context.Context, id, name string, params json.RawMessage) (any, error) {
	m, ok := b.o.Mod(id)
	if !ok {
		return nil, Denied("no mod %q is installed", id)
	}
	if m.State != mod.Enabled {
		return nil, Denied("%s is %s", id, m.State)
	}
	b.mu.RLock()
	meth, ok := b.methods[name]
	b.mu.RUnlock()
	if !ok {
		return nil, &Error{CodeNoMethod, fmt.Sprintf("no method %q", name)}
	}
	if meth.needs != "" && !m.Granted(meth.needs) {
		return nil, Denied("%s needs the %s permission, which %s doesn't have", name, meth.needs, id)
	}
	paths, err := b.paths(m)
	if err != nil {
		return nil, Failed("%s's folder: %v", id, err)
	}
	c := &Call{Mod: m, Caller: caller.ForMod(id), Method: name, Params: params, Paths: paths, b: b}
	res, err := meth.fn(ctx, c)
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			err = Failed("%s: %v", name, err)
		}
		return nil, err
	}
	return res, nil
}

// OwnDir is mod id's own folder, ~/.terminatr/state/mods/<id>.
func (b *Broker) OwnDir(id string) string { return filepath.Join(b.o.StateDir, id) }

// paths are mod m's file rules; its own folder is made when missing.
func (b *Broker) paths(m mod.Mod) (Paths, error) {
	own := b.OwnDir(m.ID)
	if err := os.MkdirAll(own, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return Paths{}, err
	}
	p := Paths{Home: b.o.Home, TMHome: b.o.TMHome, Own: own, Secrets: b.secrets}
	if m.Granted(mod.PermFiles) && m.Manifest != nil {
		p.Read = m.Manifest.Permissions.Files.Read
		p.Write = m.Manifest.Permissions.Files.Write
	}
	return p.resolve(), nil
}

func (b *Broker) audit(id, line string) {
	if b.o.Audit != nil {
		b.o.Audit(id, "broker: "+strings.Join(strings.Fields(line), " "))
	}
}
