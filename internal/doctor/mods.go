package doctor

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Mods checks the user mods in ~/.terminatr/mods and the built-in ones
// (mods design §8, Doctor): a mod that is broken or needs another mod
// API, a data file that doesn't load (mod.CheckData: a theme, icon set,
// keymap, layout, rules or template), one asking for permissions the
// human hasn't granted, settings it refused, [mods.<id>] tables with no
// mod installed and installed folders a built-in mod's id shadows; from
// a running server (live), the code mods whose host failed or is backing
// off, and safe mode. Warnings only: a mod never stops tm. Unknown
// [mods.<id>] keys are Settings' (config.Unknown).
func Mods(d Deps, live Live) []Check {
	const g = "mods"
	c, _ := config.Load() // Settings reports a broken file
	r, err := mod.Load(c)
	if err != nil {
		return append([]Check{{Group: g, Name: "mods", Status: Warn, Detail: firstLine(err.Error())}}, hostChecks(live.Mods)...)
	}
	out := hostChecks(live.Mods)
	for _, m := range r.Mods {
		switch m.State {
		case mod.Invalid:
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn, Detail: "doesn't load: " + firstLine(m.Err.Error())})
			continue
		case mod.Incompatible:
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn, Detail: firstLine(m.Err.Error())})
			continue
		}
		if m.State == mod.Enabled && len(m.WantsMore) > 0 {
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn,
				Detail: fmt.Sprintf("asks for %s, not granted: it runs without", strings.Join(m.WantsMore, ", "))})
		}
		for _, err := range m.CheckData() {
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn, Detail: "doesn't load its " + firstLine(err.Error())})
		}
		for _, b := range m.BadSettings {
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn, Detail: "setting " + b})
		}
	}
	for _, id := range r.Shadowed {
		out = append(out, Check{Group: g, Name: id, Status: Warn,
			Detail: fmt.Sprintf("mods/%s isn't loaded: %s is a built-in mod's id; give your copy another id", id, id)})
	}
	if len(r.Orphans) > 0 {
		out = append(out, Check{Group: g, Name: "config.toml", Status: Warn,
			Detail: fmt.Sprintf("[mods.%s] names no installed mod, ignored", strings.Join(r.Orphans, "], [mods."))})
	}
	return out
}

// hostChecks are a running server's supervised code mods that failed or
// are backing off, and its safe mode.
func hostChecks(res *proto.ModHostsResult) []Check {
	const g = "mods"
	if res == nil {
		return nil
	}
	var out []Check
	if res.NoMods {
		out = append(out, Check{Group: g, Name: "safe mode", Status: Warn,
			Detail: "the server runs no user mods (tm --no-mods); tm server restart runs them again"})
	}
	for _, h := range res.Hosts {
		why := ""
		if h.Err != "" {
			why = ": " + firstLine(h.Err)
		}
		switch h.State {
		case "failed": // modsup.Failed
			out = append(out, Check{Group: g, Name: h.ID, Status: Warn,
				Detail: fmt.Sprintf("failed%s; its areas are blank (tm mod logs %s, then tm mod disable %s or fix it)", why, h.ID, h.ID)})
		case "backoff": // modsup.Backoff
			out = append(out, Check{Group: g, Name: h.ID, Status: Warn,
				Detail: fmt.Sprintf("crashed%s; restarting (tm mod logs %s)", why, h.ID)})
		}
	}
	return out
}
