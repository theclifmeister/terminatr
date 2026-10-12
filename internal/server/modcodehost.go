package server

// Code host mods (mods design §2 tier 5, §12 Phase 2): a project whose
// code_host names a kind tm has no Go host for ("gitlab") is served by
// the enabled code mod that provides it, through its host's codehost.*
// methods. codehost.Pick reaches it through the provider the server
// sets (codehost.Provide); the ticker and the forwarded tm thread
// commands run here, so they all do. What is destructive stays tm's
// (codehost.Mod, codehost.External).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// codeHostMod finds the mod that serves code host kind for project ("" for
// any, as tm doctor asks): enabled, a code mod, running for the project,
// granted commands, and the only one. It answers how to call it, or why
// there is none, in fixed words.
func (s *Server) codeHostMod(kind, project string) (string, codehost.ModCall, error) {
	none := func(problem, advice string, err error) (string, codehost.ModCall, error) {
		return "", nil, &codehost.CLIError{CLI: kind, Problem: problem, Advice: advice, Err: err}
	}
	sup := s.modSup.Load()
	if sup == nil {
		return none("this server runs no mods", "the user checks tm doctor", fmt.Errorf("code host %s: this server runs no mods", kind))
	}
	reg := s.modRegistry()
	if reg == nil {
		return none("the mods can't be read", "the user checks tm doctor", fmt.Errorf("code host %s: the mods can't be read", kind))
	}
	var ids, ungranted []string
	for _, m := range reg.Mods {
		if m.Manifest == nil || !m.Manifest.IsCode() || m.Manifest.Provides.CodeHost != kind || m.State != mod.Enabled || s.noMods && !m.Builtin {
			continue
		}
		if project != "" && !m.Runs(project) {
			continue
		}
		if !m.Granted(mod.PermCommands) {
			ungranted = append(ungranted, m.ID)
			continue
		}
		ids = append(ids, m.ID)
	}
	switch {
	case len(ids) == 0 && len(ungranted) > 0:
		return none("mod "+ungranted[0]+" serves it without the commands grant", "the user grants it commands in the Mods tab",
			fmt.Errorf("code host %s: mod %s isn't granted commands, which a code host mod needs", kind, ungranted[0]))
	case len(ids) == 0:
		return none("no enabled mod serves it", "the user installs and enables a code host mod for "+kind+" (tm mod list)",
			fmt.Errorf("code host %s: no enabled mod serves it for this project", kind))
	case len(ids) > 1:
		return none("more than one mod serves it", "the user enables only one of "+strings.Join(ids, ", "),
			fmt.Errorf("code host %s: mods %s all serve it; enable one", kind, strings.Join(ids, ", ")))
	}
	id := ids[0]
	return id, func(ctx context.Context, method string, params any) (raw json.RawMessage, err error) {
		start := time.Now()
		defer func() {
			outcome := "ok"
			if err != nil {
				outcome = "error: " + err.Error()
			}
			where := "any project"
			if project != "" {
				where = project
			}
			s.modLog(id, fmt.Sprintf("%s for %s in %s: %s", method, where, time.Since(start).Round(time.Millisecond), firstLine(outcome)))
		}()
		raw, err = sup.CallCodeHost(ctx, id, method, params)
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("no answer within %s", time.Since(start).Round(time.Second))
		}
		return raw, err
	}, nil
}
