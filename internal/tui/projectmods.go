package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// The project popup's Mods tab (docs/SPEC.md §4): every installed mod
// on a line, with whether it runs for this project and whether the
// project sets any of its settings. space turns it on or off for the
// project, as tm mod enable/disable --project does, after the mod's
// consent when that turns it on; enter opens its settings for the
// project (mod API 1.11). The Mods tab of the settings popup's w picks
// the projects a mod runs for in the same words (modWhereView). Both
// are the human's: tm refuses them inside an agent, as every mod change.

// modWhere is where mod md runs, in this project's words: on or off for
// it, or following all projects, with what all projects do.
func modWhere(md mod.Mod, slug string) (string, lipgloss.Style) {
	on := md.State == mod.Enabled
	switch {
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		return md.Status(), th().Bad
	case len(md.Projects) == 0 && on:
		return "follows all projects: on", th().Plain
	case len(md.Projects) == 0:
		return "follows all projects: off", th().Faint
	case slices.Contains(md.Projects, slug) && on:
		return "on for this project", th().Good
	case slices.Contains(md.Projects, slug):
		return "on for this project, but the mod is off", th().Warn
	case on:
		return "off for this project · runs for " + joinAnd(md.Projects), th().Faint
	}
	return "off for this project", th().Faint
}

// modOwnSettings is what of mod md's settings project slug sets, "" for
// a mod without settings.
func modOwnSettings(md mod.Mod, slug string) string {
	if md.Manifest == nil || len(md.Manifest.Settings) == 0 {
		return ""
	}
	switch n := len(md.ProjectSettings[slug]); n {
	case 0:
		return "settings: all projects"
	case 1:
		return "settings: 1 of its own"
	default:
		return fmt.Sprintf("settings: %d of its own", n)
	}
}

// modsLines draws the tab w cells wide: a line per mod, then the
// selected one's description and where it runs, in words.
func (pv *projectView) modsLines(m *dash, w int) (out []string, sel int, hits []int) {
	sel = -1
	defer func() {
		for len(hits) < len(out) {
			hits = append(hits, noHit)
		}
	}()
	if m.data.ModsErr != "" {
		return []string{th().Bad.Render("The mods can't be listed: " + oneLine(m.data.ModsErr))}, -1, nil
	}
	mods := m.data.Mods
	if len(mods) == 0 {
		return faintLines("No mods installed: , then 4 installs one.", w), -1, nil
	}
	i := min(pv.sel[tabProjMods], len(mods)-1)
	pv.sel[tabProjMods] = i
	nameW, kindW := 4, 4
	for _, md := range mods {
		nameW = max(nameW, ansi.StringWidth(modName(md)))
		kindW = max(kindW, ansi.StringWidth(modKinds(md)))
	}
	nameW, kindW = min(nameW, 22), min(kindW, 26)
	for j, md := range mods {
		mark, ms := ic().idle, th().Plain
		if md.Runs(pv.slug) || md.State == mod.Invalid || md.State == mod.Incompatible {
			mark, ms, _, _ = modLook(m, md)
		}
		where, ws := modWhere(md, pv.slug)
		own := modOwnSettings(md, pv.slug)
		left := fit(modName(md), nameW) + "  " + fit(modKinds(md), kindW) + "  "
		rest := strings.TrimSuffix(where+"  "+own, "  ")
		rest = ansi.Truncate(rest, max(w-2-ansi.StringWidth(left), 8), "…")
		if j == i {
			sel = len(out)
			out = append(out, th().Sel.Render(fit(mark+" "+left+rest, w)))
		} else {
			line := ms.Render(mark) + " " + left + ws.Render(ansi.Truncate(where, max(w-2-ansi.StringWidth(left), 8), "…"))
			if own != "" && ansi.StringWidth(left+where+"  "+own)+2 <= w {
				line += "  " + th().Faint.Render(own)
			}
			out = append(out, line+reset)
		}
		hits = append(hits, j)
	}
	md := mods[i]
	out, hits = append(out, ""), append(hits, noHit)
	out = append(out, pv.modAbout(m, md, w)...)
	return out, sel, hits
}

// modAbout are the selected mod's lines under the Mods tab's list.
func (pv *projectView) modAbout(m *dash, md mod.Mod, w int) []string {
	var out []string
	about := modName(md) + ":"
	if md.Manifest != nil {
		if d := strings.TrimSpace(oneLine(mod.Clean(md.Manifest.Description, false))); d != "" {
			about += " " + d
		}
	}
	if about != modName(md)+":" {
		out = append(out, wrapLines(about, w)...)
	}
	var where string
	switch {
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		where = "It can't run: " + md.Reason() + "."
	case md.Required:
		where = "tm can't do without it: it runs for every project."
	case len(md.Projects) == 0 && md.State == mod.Enabled:
		where = "It runs for every project. space turns it off for this one: it then runs only for the others."
	case len(md.Projects) == 0:
		where = "It is off for every project. space turns it on for this one only, after its consent."
	case md.Runs(pv.slug):
		where = "It runs only for " + joinAnd(md.Projects) + ". space turns it off for this one."
	case slices.Contains(md.Projects, pv.slug):
		where = "It is listed for " + joinAnd(md.Projects) + ", but off: space turns it on for them, after its consent."
	default:
		where = "It runs only for " + joinAnd(md.Projects) + ". space turns it on for this one too."
	}
	out = append(out, faintLines(where, w)...)
	switch {
	case md.Manifest == nil || len(md.Manifest.Settings) == 0:
		out = append(out, faintLines("It has no settings.", w)...)
	case md.Builtin:
		out = append(out, faintLines("Built in: its settings are tm's look and layout, the same for every project here (, then 4, s).", w)...)
	default:
		out = append(out, faintLines("enter opens its settings for this project: each one it doesn't set follows all projects.", w)...)
	}
	prefix := "projects." + pv.slug + ": "
	for _, b := range md.BadSettings {
		if strings.HasPrefix(b, prefix) {
			for _, l := range wrapLines("Setting "+oneLine(b), w) {
				out = append(out, th().Warn.Render(l))
			}
		}
	}
	return out
}

// modsKeys are the Mods tab's keys for its selected mod, before the
// popup's own.
func (pv *projectView) modsKeys(m *dash) string {
	md, ok := pv.selMod(m)
	if !ok {
		return ""
	}
	keys := "space on/off here · "
	if md.Manifest != nil && len(md.Manifest.Settings) > 0 && !md.Builtin {
		keys += "enter settings · "
	}
	return "↑ ↓ move · " + keys
}

// selMod is the Mods tab's selected mod, false for none.
func (pv *projectView) selMod(m *dash) (mod.Mod, bool) {
	if i := pv.sel[tabProjMods]; i < len(m.data.Mods) {
		return m.data.Mods[i], true
	}
	return mod.Mod{}, false
}

// modsKey: space turns the selected mod on or off for the project,
// enter opens its settings for it.
func (pv *projectView) modsKey(m *dash, s string) tea.Cmd {
	md, ok := pv.selMod(m)
	if !ok {
		return nil
	}
	switch s {
	case "space", " ":
		return m.modToggleFor(md, pv.slug)
	case "enter":
		switch {
		case md.Manifest == nil || len(md.Manifest.Settings) == 0:
			m.fail(fmt.Errorf("%s has no settings", modName(md)))
		case md.Builtin:
			m.fail(fmt.Errorf("%s is built in: its settings are the same for every project here (, then 4, s)", modName(md)))
		default:
			m.push(newModSettings(md, pv.slug))
		}
	}
	return nil
}

// modToggleFor turns mod md on or off for project slug: off as tm mod
// disable --project does, or, for a mod that runs for every project,
// after a question, by limiting it to the others; on as tm mod enable
// --project does, after its consent when it is off or wants more.
func (m *dash) modToggleFor(md mod.Mod, slug string) tea.Cmd {
	name := modName(md)
	switch {
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		m.fail(fmt.Errorf("%s can't be turned on: %s", name, md.Reason()))
		return nil
	case md.Runs(slug) && md.Required:
		m.fail(mod.ErrRequired{ID: md.ID})
		return nil
	case md.Runs(slug) && len(md.Projects) == 0:
		others := slices.DeleteFunc(m.localProjects(), func(p string) bool { return p == slug })
		if len(others) == 0 {
			m.confirm("Turn "+name+" off", name+" runs for every project, and "+slug+" is the only one. Turn it off?", func() tea.Cmd {
				return m.modAct(md.ID, "disable", name+" off")
			})
			return nil
		}
		m.confirm("Turn "+name+" off for "+slug, name+" runs for every project. Run it only for "+joinAnd(others)+" instead? A project made later won't run it until you turn it on there.", func() tea.Cmd {
			src := m.src
			return m.act(func() actionMsg {
				if err := src.SetModProjects(md.ID, others); err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{msg: name + " off for " + slug}
			})
		})
		return nil
	case md.Runs(slug):
		return m.modActFor(md.ID, "disable", slug, name+" off for "+slug)
	case md.State == mod.Enabled && len(md.WantsMore) == 0:
		// Running it for one more project grants nothing new.
		return m.modActFor(md.ID, "enable", slug, name+" on for "+slug)
	}
	m.push(&consentView{id: md.ID, slug: slug})
	return nil
}

// localProjects are this server's projects' slugs, a paired server's
// left out.
func (m *dash) localProjects() []string {
	var out []string
	for _, p := range m.data.Projects {
		if _, _, peer := peerPlace(p.Slug); !peer {
			out = append(out, p.Slug)
		}
	}
	return out
}

// modWhereView is the settings popup's Mods tab's w: the projects mod id
// runs for, to turn it on or off for each as the project popup's Mods
// tab does, and every project, to lift its limit.
type modWhereView struct {
	id  string
	sel int
}

// rows are the view's rows: "" for every project, then each project.
func (wv *modWhereView) rows(m *dash) []string {
	return append([]string{""}, m.localProjects()...)
}

func (wv *modWhereView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	md, ok := modByID(m, wv.id)
	if !ok {
		m.pop()
		return nil
	}
	rows := wv.rows(m)
	if d, ok := scrollKeys[k.String()]; ok {
		wv.sel = moveSel(wv.sel, d, len(rows))
		return nil
	}
	switch k.String() {
	case "esc":
		m.pop()
	case "enter", "space", " ":
		return wv.pick(m, md, rows)
	}
	return nil
}

func (wv *modWhereView) pick(m *dash, md mod.Mod, rows []string) tea.Cmd {
	if wv.sel >= len(rows) {
		return nil
	}
	if slug := rows[wv.sel]; slug != "" {
		return m.modToggleFor(md, slug)
	}
	name := modName(md)
	if len(md.Projects) == 0 {
		m.msg = name + " follows all projects already"
		return nil
	}
	q := "Run " + name + " for every project, not only " + joinAnd(md.Projects) + "?"
	if md.State != mod.Enabled {
		q = "Have " + name + " follow all projects, not only " + joinAnd(md.Projects) + "? It stays off until enter on the Mods tab turns it on."
	}
	m.confirm("Every project", q, func() tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			if err := src.SetModProjects(md.ID, nil); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{msg: name + " follows all projects"}
		})
	})
	return nil
}

func (wv *modWhereView) click(m *dash, item, col int, double bool) tea.Cmd {
	if item < 0 {
		return nil
	}
	wv.sel = item
	if md, ok := modByID(m, wv.id); ok {
		return wv.pick(m, md, wv.rows(m))
	}
	return nil
}

func (wv *modWhereView) wheel(m *dash, d int) { wv.key(m, arrow(d)) }

func (wv *modWhereView) render(m *dash) string {
	md, ok := modByID(m, wv.id)
	if !ok {
		return m.popup(box{title: "Mod", body: []string{th().Faint.Render("It is no longer installed.")}, sel: -1, keys: "esc back"})
	}
	w := m.inner(dialogWidth)
	rows := wv.rows(m)
	wv.sel = min(wv.sel, len(rows)-1)
	var body []string
	var hits []int
	sel := -1
	for i, slug := range rows {
		label, on := "every project", len(md.Projects) == 0 && md.State == mod.Enabled
		if slug != "" {
			label, on = slug, md.Runs(slug)
		}
		mark, ms := ic().idle, th().Plain
		if on {
			mark, ms = ic().working, th().Good
		}
		if i == wv.sel {
			sel = len(body)
			body = append(body, th().Sel.Render(fit(mark+" "+label, w)))
		} else {
			body = append(body, ms.Render(mark)+" "+label)
		}
		hits = append(hits, i)
		if i == 0 {
			body, hits = append(body, ""), append(hits, noHit)
		}
	}
	where := "It follows all projects: " + map[bool]string{true: "on", false: "off"}[md.State == mod.Enabled] + "."
	if len(md.Projects) > 0 {
		where = "It runs only for " + joinAnd(md.Projects) + map[bool]string{true: ".", false: ", once it is on."}[md.State == mod.Enabled]
	}
	body = append(append(body, ""), faintLines(where+" enter turns it on or off for a project, as tm mod enable/disable --project does; on every project it follows all projects again.", w)...)
	for len(hits) < len(body) {
		hits = append(hits, noHit)
	}
	return m.popup(box{title: modName(md) + " · where it runs", body: body, sel: sel, hits: hits, keys: "enter on/off · ↑ ↓ move · esc back", dialog: true})
}
