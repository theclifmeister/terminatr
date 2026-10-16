package moddev

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// Broker is the fake broker: it answers a mod's $ API calls as the
// server's broker would, from the fixture's answers, then from its own
// store and files, refuses a method the mod lacks the grant of, and
// records every call in order.
//
// Without an answer: store.* and fs.* use the broker's own store and
// files; projects.list, tasks.list and actions.list are empty;
// thread.get and report.get null; thread.changes a worktree with no
// changes (state none); actions.available false;
// project.get, http.fetch and process.run an error (the fixture must
// say); everything else (the ui, annotate, inbox.raise) null.
//
// A call is refused, answer or not, as the broker refuses it, by name
// only (the mod's folder and home are made up): process.run for a
// program the commands don't name, an option the program may not have,
// or an argument naming a path the mod couldn't read; http.fetch for a
// URL (modbroker.CheckURL) that isn't https, on port 443, at a host of
// permissions.net; fs.* for a path (modbroker.Paths) outside the mod's
// own folder and files roots, or on the deny list; store.* for a key
// not 1 to 256 bytes, or a store over modbroker.MaxStore. fs.read of a
// file the fixture doesn't have is NotFound.
type Broker struct {
	grants  []string
	perms   mod.Permissions
	answers []Answer

	mu    sync.Mutex
	store map[string]json.RawMessage
	files map[string]string
	calls []Call
}

// NewBroker makes a broker for a mod asking for perms and granted
// grants, with the fixture's answers, store and files.
func NewBroker(grants []string, perms mod.Permissions, answers []Answer, store map[string]json.RawMessage, files map[string]string) *Broker {
	b := &Broker{grants: grants, perms: perms, answers: answers, store: maps.Clone(store), files: maps.Clone(files)}
	if b.store == nil {
		b.store = map[string]json.RawMessage{}
	}
	// The fixture names files as a mod would; keep them by the path the
	// checks make of the name.
	b.files = map[string]string{}
	for k, v := range files {
		if abs, err := b.paths().CheckName(k, false); err == nil {
			k = abs
		}
		b.files[k] = v
	}
	return b
}

// Calls are the calls so far, in the order the mod made them.
func (b *Broker) Calls() []Call {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.calls)
}

// Store is the store now.
func (b *Broker) Store() map[string]json.RawMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return maps.Clone(b.store)
}

// Answer records the call and answers it: a result, or an error.
func (b *Broker) Answer(method string, params json.RawMessage) (json.RawMessage, *modproto.Error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, Call{Method: method, Params: params})
	m, ok := modapi.Lookup(method)
	if !ok || m.Local {
		return nil, &modproto.Error{Code: modproto.CodeMethodNotFound, Message: "unknown method " + method}
	}
	if m.Needs != "" && !slices.Contains(b.grants, m.Needs) {
		return nil, &modproto.Error{Code: modproto.CodeDenied, Message: fmt.Sprintf("refused: %s needs the %s permission", method, m.Needs)}
	}
	if err := b.check(method, params); err != nil {
		return nil, err
	}
	for _, a := range b.answers {
		if a.Method == method && Match(a.Params, params) {
			if a.Error != "" {
				return nil, &modproto.Error{Code: modproto.CodeInternal, Message: a.Error}
			}
			if len(a.Result) == 0 {
				return json.RawMessage("null"), nil
			}
			return a.Result, nil
		}
	}
	var p struct {
		Key    string          `json:"key"`
		Value  json.RawMessage `json:"value"`
		Path   string          `json:"path"`
		Text   string          `json:"text"`
		Append bool            `json:"append"`
		Slug   string          `json:"slug"`
		Level  string          `json:"level"`
	}
	_ = json.Unmarshal(params, &p)
	switch method {
	case "store.get":
		if v, ok := b.store[p.Key]; ok {
			return v, nil
		}
		return json.RawMessage("null"), nil
	case "store.set":
		if len(p.Value) == 0 || string(p.Value) == "null" {
			delete(b.store, p.Key)
		} else {
			b.store[p.Key] = p.Value
		}
	case "store.del":
		delete(b.store, p.Key)
	case "fs.read":
		abs, _ := b.paths().CheckName(p.Path, false) // checked already
		t, ok := b.files[abs]
		if !ok {
			return nil, &modproto.Error{Code: modproto.CodeNotFound, Message: p.Path + ": no such file"}
		}
		return marshal(t), nil
	case "fs.write":
		abs, _ := b.paths().CheckName(p.Path, true)
		if p.Append {
			b.files[abs] += p.Text
		} else {
			b.files[abs] = p.Text
		}
	case "ui.toast":
		// Refused as the server refuses it (ui.toast in internal/server).
		if strings.TrimSpace(p.Text) == "" || p.Level != "" && !slices.Contains(modapi.Levels, p.Level) {
			return nil, &modproto.Error{Code: modproto.CodeInvalidParams, Message: "ui.toast: a toast needs a text, and a level of info, warn or error"}
		}
	case "projects.list", "tasks.list", "actions.list", "memory.list":
		return json.RawMessage("[]"), nil
	case "actions.available":
		return json.RawMessage("false"), nil
	case "thread.changes":
		return marshal(changes.Changeset{State: changes.StateNone, Mode: changes.ModeBase, Files: []changes.File{}}), nil
	case "project.get":
		return nil, &modproto.Error{Code: modproto.CodeInternal, Message: fmt.Sprintf("no project %q: the fixture answers project.get", p.Slug)}
	case "memory.read":
		return nil, &modproto.Error{Code: modproto.CodeNotFound, Message: "no memory file: the fixture answers memory.read"}
	case "http.fetch", "process.run":
		return nil, &modproto.Error{Code: modproto.CodeInternal, Message: "the fixture has no answer for " + method}
	}
	return json.RawMessage("null"), nil
}

func marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// fakeRoot is the made-up folder the mod's home and own folder are in.
var fakeRoot = map[bool]string{false: "/fixture", true: `C:\fixture`}[caps.Windows]

// paths are the file rules of the mod, over the made-up folders: its own
// folder always, the roots of its files grant when granted.
func (b *Broker) paths() modbroker.Paths {
	home := filepath.Join(fakeRoot, "home")
	p := modbroker.Paths{Home: home, TMHome: filepath.Join(home, ".terminatr"), Own: filepath.Join(home, ".terminatr", "state", "mods", "mod"),
		Secrets: append(agent.Secrets(nil, home, noenv), modbroker.Keychains(home, noenv)...)}
	if slices.Contains(b.grants, mod.PermFiles) {
		p.Read, p.Write = b.perms.Files.Read, b.perms.Files.Write
	}
	return p.Resolved()
}

// check refuses a call the broker would refuse for what it names: a
// program, a URL, a path or a store key and size. Nothing else.
func (b *Broker) check(method string, params json.RawMessage) *modproto.Error {
	var err error
	switch method {
	case "process.run":
		err = b.checkRun(params)
	case "http.fetch":
		var a struct {
			URL string `json:"url"`
		}
		if err = json.Unmarshal(params, &a); err == nil {
			_, err = modbroker.CheckURL(a.URL, b.perms.Net)
		}
	case "fs.read", "fs.write":
		var a struct {
			Path string `json:"path"`
		}
		if err = json.Unmarshal(params, &a); err == nil {
			_, err = b.paths().CheckName(a.Path, method == "fs.write")
		}
	case "store.get", "store.set", "store.del":
		var a struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err = json.Unmarshal(params, &a); err == nil {
			err = modbroker.CheckKey(method, a.Key)
		}
		if err == nil && method == "store.set" {
			next := maps.Clone(b.store)
			if len(a.Value) == 0 {
				a.Value = json.RawMessage("null")
			}
			next[a.Key] = a.Value
			err = modbroker.CheckStoreSize(next)
		}
	}
	var e *modbroker.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &e):
		return &modproto.Error{Code: e.Code, Message: e.Message}
	default:
		return &modproto.Error{Code: modproto.CodeInvalidParams, Message: method + ": " + err.Error()}
	}
}

// checkRun refuses a process.run the broker would refuse.
func (b *Broker) checkRun(params json.RawMessage) error {
	var a struct {
		Argv []string `json:"argv"`
		modbroker.RunOptions
		Options *modbroker.RunOptions `json:"options"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return err
	}
	if a.Options != nil {
		a.RunOptions = *a.Options
	}
	p := b.paths()
	err := modbroker.CheckRun(a.Argv, b.perms.Commands)
	if err == nil {
		dir := p.Own
		if a.Cwd != "" {
			dir, err = p.CheckName(a.Cwd, false)
		}
		if err == nil {
			err = p.CheckArgsByName(a.Argv[1:], dir)
		}
	}
	return err
}

func noenv(string) string { return "" }
