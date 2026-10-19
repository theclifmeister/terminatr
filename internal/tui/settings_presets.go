package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Presets as "add these rules" (T574 §3.3, §4.3): p on the Access tab
// lists the presets; enter on one shows the rules it adds and which
// are there already, and enter adds them. Each added rule then stands
// on its own: nothing ties it to the preset afterwards.

// presetsView lists the presets, for the rows' table.
type presetsView struct {
	sr  safetyRows
	sel int
}

// openPresets opens the preset list for slug's Access tab ("" for all
// projects').
func (m *dash) openPresets(slug string) tea.Cmd {
	m.push(&presetsView{sr: newSafetyRows(slug)})
	return nil
}

func (v *presetsView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		m.pop()
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(config.Presets))
	case "enter", "space":
		m.push(newPresetAdd(m, v.sr, config.Presets[v.sel]))
	}
	return nil
}

func (v *presetsView) render(m *dash) string {
	w := m.inner(viewWidth)
	goos := m.serverOS()
	lines := faintLines("A preset is a group of rules: adding it adds them "+v.sr.forWho+", each then yours to change or remove.", w)
	sel := -1
	for i, p := range config.Presets {
		lines = append(lines, "")
		text := textsafe.Line(p.Title, w)
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(text, w)))
			sel = len(lines) - 1
		} else {
			lines = append(lines, th().Head.Render(text))
		}
		about := p.About + " " + presetOSWords(p, goos)
		for _, l := range faintLines(about, w-2) {
			lines = append(lines, "  "+l)
		}
	}
	return m.popup(box{title: "Add a preset " + v.sr.forWho, body: lines, sel: sel, keys: "enter show its rules · ↑ ↓ move · esc back"})
}

// presetOSWords says where p's rules apply, and that they don't where
// threads run.
func presetOSWords(p config.Preset, goos string) string {
	var names []string
	for _, os := range p.OS {
		names = append(names, config.OSName(os))
	}
	s := strings.Join(names, ", ") + "."
	if !slices.Contains(p.OS, goos) {
		s += " Its rules don't apply on " + config.OSName(goos) + ", where this server's threads run."
	}
	return s
}

// presetAdd shows the rules a preset adds to a table and adds the ones
// left checked: space leaves one out, A (on a project) adds them to all
// projects instead.
type presetAdd struct {
	sr      safetyRows
	project safetyRows // the project it was opened on; all false there
	p       config.Preset
	off     map[int]bool // rules left out, by index in p.Rules
	sel     int
}

func newPresetAdd(m *dash, sr safetyRows, p config.Preset) *presetAdd {
	return &presetAdd{sr: sr, project: sr, p: p, off: map[int]bool{}}
}

// missing are the preset's rules the table's threads don't have, by
// index in p.Rules; there those they have.
func (v *presetAdd) missing(m *dash) (missing []int, there []config.PresetRule) {
	t := v.sr.merged(m)
	for i, r := range v.p.Rules {
		if t.Has(r) {
			there = append(there, r)
		} else {
			missing = append(missing, i)
		}
	}
	return missing, there
}

func (v *presetAdd) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	missing, _ := v.missing(m)
	switch k.String() {
	case "esc", "q":
		m.pop()
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(missing))
	case "space":
		if v.sel < len(missing) {
			v.off[missing[v.sel]] = !v.off[missing[v.sel]]
		}
	case "A":
		if v.project.all {
			return nil
		}
		if v.sr.all {
			v.sr = v.project
		} else {
			v.sr = newSafetyRows("")
		}
		v.off, v.sel = map[int]bool{}, 0
	case "enter":
		var add []config.PresetRule
		for _, i := range missing {
			if !v.off[i] {
				add = append(add, v.p.Rules[i])
			}
		}
		if len(add) == 0 {
			m.msg = "nothing to add: " + v.p.Title + "'s rules are there already, or left out"
			return nil
		}
		add = config.WordRules(add, v.sr.merged(m))
		words := make([]string, len(add))
		for i, r := range add {
			words[i] = presetRuleWords(r)
		}
		m.pop()
		m.pop()
		msg := fmt.Sprintf("%d rules of %s added %s: %s; threads get them from their next start", len(add), v.p.Title, v.sr.forWho, strings.Join(words, ", "))
		if len(add) == 1 {
			msg = fmt.Sprintf("%s added %s (%s): threads get it from their next start", words[0], v.sr.forWho, v.p.Title)
		}
		return v.sr.editSandbox(m, "preset "+v.p.Name+": "+strings.Join(words, ", "), msg, func(t *config.SandboxTable) {
			for _, r := range add {
				t.AddRule(r)
			}
		})
	}
	return nil
}

// presetKindWords name a preset rule's kind in its line.
var presetKindWords = map[string]string{
	config.SandboxHosts: "host", config.SandboxGuides: "guide", config.SandboxLocalPorts: "local ports",
	config.SandboxExcluded: "excluded", config.SandboxAllow: "allow",
}

// presetRuleWords is a preset rule as its line and messages say it.
func presetRuleWords(r config.PresetRule) string {
	return presetKindWords[r.Key] + " " + r.Value
}

func (v *presetAdd) render(m *dash) string {
	w := m.inner(viewWidth)
	missing, there := v.missing(m)
	lines := faintLines(v.p.About+" "+presetOSWords(v.p, m.serverOS()), w)
	lines = append(lines, "")
	lines = append(lines, faintLines("These rules are added "+v.sr.forWho+". Each one is then yours, to change or remove:", w)...)
	lines = append(lines, "")
	sel := -1
	n := 0
	for i, idx := range missing {
		r := v.p.Rules[idx]
		box := "[x] "
		if v.off[idx] {
			box = "[ ] "
		} else {
			n++
		}
		text := box + fit(presetKindWords[r.Key], 12) + " " + textsafe.Line(r.Value, max(w-17, 10))
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(text, w)))
			sel = len(lines) - 1
		} else {
			lines = append(lines, th().Head.Render(text))
		}
		if about := config.PresetGuides[r.Value]; r.Key == config.SandboxGuides && about != "" {
			about = "Its text, " + about + ", is written out from the rules threads have once it is added: enter on it in Guidance for threads shows and edits it."
			for _, l := range faintLines(about, w-4) {
				lines = append(lines, "    "+l)
			}
		}
	}
	thereWords := "nothing"
	if len(there) > 0 {
		words := make([]string, len(there))
		for i, r := range there {
			words[i] = presetRuleWords(r)
		}
		thereWords = strings.Join(words, ", ")
	}
	lines = append(lines, "")
	lines = append(lines, faintLines("Already there: "+thereWords, w)...)
	keys := []string{"space leave one out"}
	switch {
	case v.project.all:
	case v.sr.all:
		keys = append(keys, "A add to "+v.project.slug+" instead")
	default:
		keys = append(keys, "A add to all projects instead")
	}
	keys = append(keys, fmt.Sprintf("enter add %d rules", n), "esc")
	if n == 1 {
		keys[len(keys)-2] = "enter add 1 rule"
	}
	return m.popup(box{title: "Add " + v.p.Title + " " + v.sr.forWho, body: lines, sel: sel, keys: strings.Join(keys, " · ")})
}
