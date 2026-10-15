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
// process.run is refused, answer or not, as the broker refuses it: a
// program the commands don't name, an option the program may not have,
// or an argument naming a path the mod couldn't read, by name only
// (its folder and home are made up).
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
	if b.files == nil {
		b.files = map[string]string{}
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
	if method == "process.run" {
		if err := b.checkRun(params); err != nil {
			return nil, err
		}
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
		t, ok := b.files[p.Path]
		if !ok {
			return nil, &modproto.Error{Code: modproto.CodeInternal, Message: "no file " + p.Path}
		}
		return marshal(t), nil
	case "fs.write":
		if p.Append {
			b.files[p.Path] += p.Text
		} else {
			b.files[p.Path] = p.Text
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

// checkRun refuses a process.run the broker would refuse.
func (b *Broker) checkRun(params json.RawMessage) *modproto.Error {
	var a struct {
		Argv []string `json:"argv"`
		modbroker.RunOptions
		Options *modbroker.RunOptions `json:"options"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return &modproto.Error{Code: modproto.CodeInvalidParams, Message: "process.run: " + err.Error()}
	}
	if a.Options != nil {
		a.RunOptions = *a.Options
	}
	home := filepath.Join(fakeRoot, "home")
	p := modbroker.Paths{Home: home, TMHome: filepath.Join(home, ".terminatr"), Own: filepath.Join(home, ".terminatr", "state", "mods", "mod"),
		Secrets: append(agent.Secrets(nil, home, noenv), modbroker.Keychains(home, noenv)...)}
	if slices.Contains(b.grants, mod.PermFiles) {
		p.Read, p.Write = b.perms.Files.Read, b.perms.Files.Write
	}
	p = p.Resolved()
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
	var e *modbroker.Error
	if errors.As(err, &e) {
		return &modproto.Error{Code: e.Code, Message: e.Message}
	}
	return nil
}

func noenv(string) string { return "" }
