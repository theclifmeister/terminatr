package tui

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The settings popup's Mods tab (mods design §9): every installed mod on
// a line, its mark (● on, ○ off, ▲ failed, ◆ wants more), name, what it
// provides, version and what it may do, or why it doesn't run; the
// selected mod's description under the list. Turning a mod on, granting
// it permissions, updating and removing it are the human's calls, each
// after the mod's consent or a question; tm refuses them inside an
// agent. Built-in mods can be turned off, never removed or updated on
// their own. Nothing here installs a mod: e only shows where the
// example mods are and how to install one.

// modsTab is the Mods tab's selection.
type modsTab struct{ sel int }

// modsKeys are the Mods tab's action row.
const modsKeys = "enter on/off · p permissions · s settings · l log · r restart · u update · x remove · e examples · ← → tabs · esc close"

// selMod is the selected mod, false for none.
func (mt *modsTab) selMod(m *dash) (mod.Mod, bool) {
	if mt.sel < len(m.data.Mods) {
		return m.data.Mods[mt.sel], true
	}
	return mod.Mod{}, false
}

// modByID is mod id as the last poll has it.
func modByID(m *dash, id string) (mod.Mod, bool) {
	for _, md := range m.data.Mods {
		if md.ID == id {
			return md, true
		}
	}
	return mod.Mod{}, false
}

// modName is the mod's name, its id when its manifest doesn't load.
func modName(md mod.Mod) string {
	if md.Manifest != nil && strings.TrimSpace(md.Manifest.Name) != "" {
		return oneLine(mod.Clean(md.Manifest.Name, false))
	}
	return md.ID
}

// modKinds is what the mod provides, "built in" last for a bundled one.
func modKinds(md mod.Mod) string {
	var k []string
	if md.Manifest != nil {
		k = md.Manifest.Kinds()
	}
	if md.Builtin {
		k = append(k, "built in")
	}
	if len(k) == 0 {
		return "-"
	}
	return strings.Join(k, " · ")
}

// modHost is the enabled code mod's host as the supervisor has it;
// false for a data mod, one that is off, or when the server doesn't say.
func modHost(m *dash, md mod.Mod) (proto.ModHost, bool) {
	if md.State != mod.Enabled || md.Manifest == nil || !md.Manifest.IsCode() {
		return proto.ModHost{}, false
	}
	h, ok := m.data.ModHosts[md.ID]
	return h, ok
}

// modFailure is the last error of the mod's host when it failed.
func modFailure(m *dash, md mod.Mod) string {
	if h, ok := modHost(m, md); ok && h.State == hostFailed {
		return cmp.Or(oneLine(h.Err), "no reason given")
	}
	return ""
}

// The supervisor's states (modsup.State) as mod.hosts says them.
const (
	hostIdle     = "idle"
	hostStarting = "starting"
	hostRunning  = "running"
	hostBackoff  = "backoff"
	hostFailed   = "failed"
)

// hostRestart is "restarting in 5s: <err>" for a host in backoff.
func hostRestart(h proto.ModHost) string {
	secs := max(int(math.Ceil(time.Until(h.Next).Seconds())), 0)
	out := fmt.Sprintf("restarting in %ds", secs)
	if e := oneLine(h.Err); e != "" {
		out += ": " + e
	}
	return out
}

// modLook is the mod's mark and status, each with its style: a code
// mod that is on by its host's state (mods design §8), the others by
// their own.
func modLook(m *dash, md mod.Mod) (mark string, ms lipgloss.Style, status string, ss lipgloss.Style) {
	plain := th().Plain
	h, hosted := modHost(m, md)
	switch {
	case hosted && h.State == hostFailed:
		return ic().blocked, th().Bad, "failed: " + modFailure(m, md), th().Bad
	case hosted && h.State == hostBackoff:
		return ic().blocked, th().Warn, hostRestart(h), th().Warn
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		return ic().blocked, th().Bad, md.Status(), th().Bad
	case md.State == mod.Enabled && len(md.WantsMore) > 0:
		return ic().review, th().Warn, "wants more: " + strings.Join(md.Manifest.Permissions.Words(md.WantsMore), " · "), th().Warn
	case hosted && h.State == hostStarting:
		return ic().starting, th().Good, md.Status(), th().Accent
	case hosted && h.State == hostIdle:
		return ic().working, th().Good, md.Status() + " · waits for its first event", th().Faint
	case md.State == mod.Enabled && md.Builtin:
		return ic().working, th().Good, md.Status(), th().Faint
	case md.State == mod.Enabled:
		return ic().working, th().Good, md.Status(), th().Accent
	}
	return ic().idle, plain, md.Status(), plain
}

// lines draws the tab w cells wide: its lines, the selected mod's line
// and what a click on each line picks (the mod's index).
func (mt *modsTab) lines(m *dash, w int) (out []string, sel int, hits []int) {
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
		out = append(out, faintLines("No mods installed. tm mod install <path | git-url[@tag]> installs one; it starts off, until you turn it on here.", w)...)
		out = append(out, "")
		out = append(out, faintLines("e shows terminatr's example mods, one for each place a mod can show up, and how to install them.", w)...)
		return out, -1, nil
	}
	mt.sel = min(mt.sel, len(mods)-1)
	nameW, kindW, verW := 4, 4, 1
	for _, md := range mods {
		nameW = max(nameW, ansi.StringWidth(modName(md)))
		kindW = max(kindW, ansi.StringWidth(modKinds(md)))
		if md.Manifest != nil {
			verW = max(verW, ansi.StringWidth(md.Manifest.Version))
		}
	}
	nameW, kindW, verW = min(nameW, 22), min(kindW, 26), min(verW, 9)
	for i, md := range mods {
		mark, ms, status, ss := modLook(m, md)
		ver := "-"
		if md.Manifest != nil {
			ver = md.Manifest.Version
		}
		left := fit(modName(md), nameW) + "  " + fit(modKinds(md), kindW) + "  " + fit(ver, verW) + "  "
		rest := max(w-2-ansi.StringWidth(left), 8)
		status = ansi.Truncate(status, rest, "…")
		if i == mt.sel {
			sel = len(out)
			out = append(out, th().Sel.Render(fit(mark+" "+left+status, w)))
		} else {
			out = append(out, ms.Render(mark)+" "+left+ss.Render(status)+reset)
		}
		hits = append(hits, i)
	}
	md := mods[mt.sel]
	out, hits = append(out, ""), append(hits, noHit)
	out = append(out, modAbout(m, md, w)...)
	return out, sel, hits
}

// hostWords say where a host that is fine stands.
func hostWords(h proto.ModHost) string {
	switch h.State {
	case hostRunning:
		out := "Runs"
		if h.PID > 0 {
			out += fmt.Sprintf(" as pid %d", h.PID)
		}
		if h.RSS > 0 {
			out += ", using " + sizeWords(int64(h.RSS))
		}
		if h.Restarts > 0 {
			out += fmt.Sprintf(", restarted %d×", h.Restarts)
		}
		return out + "."
	case hostStarting:
		return "Starting: its host loads its script."
	case hostIdle:
		return "Waits for its first event: its host starts then."
	}
	return "Its host is " + h.State + "."
}

// modAbout are the selected mod's lines under the list: what it is and
// who made it, then why it doesn't run, or what of its settings tm
// didn't take.
func modAbout(m *dash, md mod.Mod, w int) []string {
	var out []string
	about := modName(md) + ":"
	if md.Manifest != nil {
		if d := strings.TrimSpace(oneLine(mod.Clean(md.Manifest.Description, false))); d != "" {
			about += " " + d
		}
		if len(md.Manifest.Authors) > 0 {
			about += " By " + oneLine(mod.Clean(strings.Join(md.Manifest.Authors, ", "), false)) + "."
		}
	}
	if about != modName(md)+":" {
		out = append(out, wrapLines(about, w)...)
	}
	if len(md.Projects) > 0 {
		out = append(out, faintLines("Runs only for "+joinAnd(md.Projects)+".", w)...)
	}
	h, hosted := modHost(m, md)
	switch f := modFailure(m, md); {
	case f != "":
		for _, l := range wrapLines("Failed: "+f, w) {
			out = append(out, th().Bad.Render(l))
		}
		out = append(out, faintLines("Its columns and panels stay blank until it runs again; your threads carry on. r restarts it; l shows its log.", w)...)
	case hosted && h.State == hostBackoff:
		for _, l := range wrapLines("Its host stopped: "+hostRestart(h)+".", w) {
			out = append(out, th().Warn.Render(l))
		}
		out = append(out, faintLines("r restarts it now; l shows its log.", w)...)
	case hosted:
		out = append(out, faintLines(hostWords(h), w)...)
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		for _, l := range wrapLines(md.Status(), w) {
			out = append(out, th().Bad.Render(l))
		}
	}
	for _, b := range md.BadSettings {
		for _, l := range wrapLines("Setting "+oneLine(b), w) {
			out = append(out, th().Warn.Render(l))
		}
	}
	return out
}

func (mt *modsTab) key(m *dash, k tea.KeyPressMsg) (tea.Cmd, bool) {
	s := k.String()
	if d, ok := scrollKeys[s]; ok {
		mt.sel = moveSel(mt.sel, d, len(m.data.Mods))
		return nil, true
	}
	if s == "e" {
		m.push(&examplesView{})
		return nil, true
	}
	md, ok := mt.selMod(m)
	if !ok {
		return nil, slices.Contains([]string{"enter", "space", " ", "p", "s", "l", "r", "u", "x"}, s)
	}
	switch s {
	case "enter", "space", " ":
		return m.modToggle(md), true
	case "p":
		m.push(&modPermsView{id: md.ID})
	case "s":
		m.push(newModSettings(md))
	case "l":
		m.openModLog(md)
	case "r":
		return m.modRestart(md), true
	case "u":
		return m.modUpdate(md), true
	case "x":
		m.modRemove(md)
	default:
		return nil, false
	}
	return nil, true
}

// modToggle turns the mod off, or asks its consent to turn it on, or to
// grant what it wants more.
func (m *dash) modToggle(md mod.Mod) tea.Cmd {
	name := modName(md)
	switch {
	case md.State == mod.Invalid || md.State == mod.Incompatible:
		m.fail(fmt.Errorf("%s can't be turned on: %s", name, md.Reason()))
		return nil
	case md.State == mod.Enabled && len(md.WantsMore) == 0:
		return m.modAct(md.ID, "disable", name+" off")
	}
	m.push(&consentView{id: md.ID})
	return nil
}

// modAct runs a mod action in the background, then reloads.
func (m *dash) modAct(id, verb, msg string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		if err := src.ModAct(id, verb); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: msg}
	})
}

// modRestart starts a code mod's host again (mod.restart): a failed one
// gets a fresh count. A data mod has no host to restart.
func (m *dash) modRestart(md mod.Mod) tea.Cmd {
	name := modName(md)
	switch {
	case md.Manifest == nil || !md.Manifest.IsCode():
		m.fail(fmt.Errorf("%s has no code to restart: only a code mod runs", name))
		return nil
	case md.State != mod.Enabled:
		m.fail(fmt.Errorf("%s is off: enter turns it on", name))
		return nil
	}
	src := m.src
	return m.act(func() actionMsg {
		if err := src.RestartMod(md.ID); err != nil {
			return actionMsg{err: fmt.Errorf("%s: %w", name, err)}
		}
		return actionMsg{msg: "restarted " + name}
	})
}

func (m *dash) modRemove(md mod.Mod) {
	name := modName(md)
	if md.Builtin {
		m.fail(fmt.Errorf("%s is built in: it can be turned off, not removed", name))
		return
	}
	m.confirm("Remove a mod", "Remove "+name+"? Its data is kept for 30 days, so installing it again finds it.", func() tea.Cmd {
		return m.modAct(md.ID, "remove", "removed "+name)
	})
}

// permStyle is a permission's colour in the consent (mods design §7):
// what reads or tells stays inside tm, files and the network reach out,
// and running programs reaches furthest.
func permStyle(perm string) lipgloss.Style {
	switch perm {
	case mod.PermFiles, mod.PermNet:
		return th().Warn
	case mod.PermCommands:
		return th().Bad
	}
	return th().Info
}

// consentView is a mod's consent (mods design §9): what it is, each
// permission it asks for in plain words and its colour, every rules
// addendum and agent manifest in full, then "Enable <name>?". y turns it
// on with every permission it asks for; a mod that wants more is asked
// for what it wants more, and o turns it off instead.
type consentView struct {
	id     string
	scroll int
}

func (cv *consentView) lines(m *dash, md mod.Mod, w int) []string {
	man := md.Manifest
	var out []string
	out = append(out, th().Head.Render(ansi.Truncate(oneLine(man.Headline()), w, "…")))
	if d := strings.TrimSpace(oneLine(mod.Clean(man.Description, false))); d != "" {
		out = append(out, wrapLines(d, w)...)
	}
	if man.Homepage != "" {
		out = append(out, th().Faint.Render(ansi.Truncate(oneLine(mod.Clean(man.Homepage, false)), w, "…")))
	}
	out = append(out, wrapLines("Provides: "+strings.Join(man.Kinds(), " · "), w)...)
	out = append(out, "")
	asked := man.Permissions.Requested()
	if len(asked) == 0 {
		out = append(out, "It asks for no permission: "+th().Good.Render("safe")+".")
		out = append(out, faintLines("Its own data, columns, settings and log, and events about sessions and the clock.", w)...)
	} else {
		out = append(out, "It may:")
		for _, p := range asked {
			l := "  • " + permStyle(p).Render(oneLine(man.Permissions.PermWords(p)))
			if md.State == mod.Enabled && slices.Contains(md.WantsMore, p) {
				l += th().Faint.Render(" · new")
			}
			out = append(out, ansi.Truncate(l, w, "…"))
		}
	}
	if shown := man.Shown(md.Dir); shown != "" {
		out = append(out, "")
		for _, l := range strings.Split(strings.TrimRight(shown, "\n"), "\n") {
			out = append(out, strings.Split(ansi.Hardwrap(strings.ReplaceAll(l, "\t", "    "), max(w, 10), true), "\n")...)
		}
	}
	out = append(out, "", th().Head.Render(cv.question(md)))
	return out
}

func (cv *consentView) question(md mod.Mod) string {
	if md.State == mod.Enabled {
		return "Grant " + modName(md) + " what it wants more?"
	}
	return "Enable " + modName(md) + "?"
}

func (cv *consentView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	md, ok := modByID(m, cv.id)
	if !ok || md.Manifest == nil {
		m.pop()
		return nil
	}
	name := modName(md)
	if d, ok := scrollKeys[k.String()]; ok {
		cv.scroll = clampScroll(cv.scroll+d, len(cv.lines(m, md, m.inner(viewWidth))))
		return nil
	}
	switch k.String() {
	case "y":
		m.pop()
		msg := name + " on"
		if md.State == mod.Enabled {
			msg = name + " has what it asked for"
		}
		return m.modAct(md.ID, "enable", msg)
	case "o":
		if md.State == mod.Enabled {
			m.pop()
			return m.modAct(md.ID, "disable", name+" off")
		}
	case "n", "esc":
		m.pop()
		m.msg = "unchanged"
	}
	return nil
}

func (cv *consentView) wheel(m *dash, d int) { cv.key(m, arrow(d)) }

func (cv *consentView) render(m *dash) string {
	md, ok := modByID(m, cv.id)
	if !ok || md.Manifest == nil {
		return m.popup(box{title: "Mod", body: []string{th().Faint.Render("It is no longer installed.")}, sel: -1, keys: "esc back"})
	}
	keys := "y yes · n no · ↑ ↓ scroll · esc cancel"
	title := "Enable " + modName(md)
	if md.State == mod.Enabled {
		keys = "y grant · n not now · o turn it off · ↑ ↓ scroll · esc cancel"
		title = modName(md) + " wants more"
	}
	return m.popup(box{title: title, body: cv.lines(m, md, m.inner(viewWidth)), sel: -1, scroll: cv.scroll, keys: keys})
}

// modPermsView lists the permissions a mod asks for, each granted or
// not: enter takes one back at once, or grants it after a question. A
// mod runs with what it is granted of what it asks for; one that asks
// for more than it has shows ◆ in the list.
type modPermsView struct {
	id  string
	sel int
}

func (v *modPermsView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	md, ok := modByID(m, v.id)
	if !ok || md.Manifest == nil {
		m.pop()
		return nil
	}
	asked := md.Manifest.Permissions.Requested()
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[s], len(asked))
	case "enter", "space", " ":
		if v.sel >= len(asked) {
			break
		}
		p, name := asked[v.sel], modName(md)
		words := oneLine(md.Manifest.Permissions.PermWords(p))
		if md.Granted(p) {
			rest := slices.DeleteFunc(slices.Clone(md.Grants), func(g string) bool { return g == p })
			return m.setModGrants(md.ID, rest, name+" no longer "+words)
		}
		grants := append(slices.Clone(md.Grants), p)
		m.confirm("Grant a permission", "Let "+name+": "+words+"?", func() tea.Cmd {
			return m.setModGrants(md.ID, grants, name+" "+words)
		})
	}
	return nil
}

func (m *dash) setModGrants(id string, grants []string, msg string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		if err := src.SetModGrants(id, grants); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: msg}
	})
}

func (v *modPermsView) render(m *dash) string {
	md, ok := modByID(m, v.id)
	w := m.inner(dialogWidth)
	if !ok || md.Manifest == nil {
		return m.popup(box{title: "Permissions", body: []string{th().Faint.Render("It is no longer installed.")}, sel: -1, keys: "esc back", dialog: true})
	}
	asked := md.Manifest.Permissions.Requested()
	lines := faintLines("What "+modName(md)+" may do, of what it asks for. It never gets your credentials: there is no permission for them.", w)
	if len(asked) == 0 {
		lines = append(lines, "", "It asks for no permission: "+th().Good.Render("safe")+".")
	}
	for i, p := range asked {
		mark, word := ic().todoOpen, "not granted"
		if md.Granted(p) {
			mark, word = ic().todoDone, "granted"
		}
		l := mark + " " + permStyle(p).Render(fit(oneLine(md.Manifest.Permissions.PermWords(p)), max(w-16, 8))) + "  " + th().Faint.Render(word)
		if i == v.sel {
			l = th().Sel.Render(fit(ansi.Strip(l), w))
		}
		lines = append(lines, l)
	}
	return m.popup(box{title: modName(md) + " · permissions", body: lines, sel: -1, keys: "enter grant or take back · ↑ ↓ move · esc back", dialog: true})
}

// newModSettings is the mod's own settings page, from its manifest:
// enter turns a yes/no over or steps a choice, + and - change a number,
// x puts one back to its default. Text settings show here and are set
// in the mod's [mods.<id>.settings] for now.
func newModSettings(md mod.Mod) *modSettingsView {
	v := &modSettingsView{id: md.ID, title: modName(md) + " · settings"}
	if md.Manifest == nil {
		return v
	}
	for _, key := range sortedSettingKeys(md.Manifest.Settings) {
		v.list.rows = append(v.list.rows, modSettingRow(md.ID, key, md.Manifest.Settings[key]))
	}
	return v
}

func sortedSettingKeys(s map[string]mod.Setting) []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// modSettingRow is setting key of mod id, as a settings row whose value
// is the last poll's.
func modSettingRow(id, key string, s mod.Setting) setting {
	label := key
	help := ""
	if s.Label != "" {
		help = oneLine(mod.Clean(s.Label, false)) + ". "
	}
	help += "Default " + fmt.Sprint(s.Default) + "."
	cur := func(m *dash) any {
		if md, ok := modByID(m, id); ok {
			return md.Settings[key]
		}
		return nil
	}
	set := func(m *dash, v any, msg string) tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			if err := src.SetModSetting(id, key, v); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{msg: msg}
		})
	}
	r := setting{label: label, help: help,
		value: func(m *dash) string { return fmt.Sprint(cur(m)) },
		unset: func(m *dash) tea.Cmd { return set(m, nil, key+": back to "+fmt.Sprint(s.Default)) }}
	switch s.Type {
	case mod.SettingBool:
		r.value = func(m *dash) string { b, _ := cur(m).(bool); return onOff(b) }
		r.change = func(m *dash) tea.Cmd {
			b, _ := cur(m).(bool)
			return set(m, !b, key+" "+onOff(!b))
		}
	case mod.SettingChoice:
		r.change = func(m *dash) tea.Cmd {
			if len(s.Options) == 0 {
				return nil
			}
			c, _ := cur(m).(string)
			next := s.Options[(slices.Index(s.Options, c)+1)%len(s.Options)]
			return set(m, next, key+": "+next)
		}
	case mod.SettingInt:
		r.adjust = func(m *dash, d int) tea.Cmd {
			n, _ := cur(m).(int64)
			next := n + int64(d)
			if (s.Min != nil && next < *s.Min) || (s.Max != nil && next > *s.Max) {
				return nil
			}
			return set(m, next, fmt.Sprintf("%s: %d", key, next))
		}
	case mod.SettingString:
		r.help += " A text setting can't be changed here yet."
	}
	return r
}

// modSettingsView is a mod's own settings.
type modSettingsView struct {
	id, title string
	list      settingsList
}

func (v *modSettingsView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if s := k.String(); s == "esc" || s == "q" {
		m.pop()
		return nil
	}
	cmd, _ := v.list.key(m, k)
	return cmd
}

func (v *modSettingsView) render(m *dash) string {
	w := m.inner(viewWidth)
	var head []string
	if md, ok := modByID(m, v.id); ok {
		for _, b := range md.BadSettings {
			for _, l := range wrapLines("Setting "+oneLine(b), w) {
				head = append(head, th().Warn.Render(l))
			}
		}
	}
	if len(v.list.rows) == 0 {
		return m.popup(box{title: v.title, head: head, body: []string{th().Faint.Render("It has no settings.")}, sel: -1, keys: "esc back"})
	}
	lines, sel, hits := v.list.lines(m, w)
	all := make([]int, len(head), len(head)+len(hits))
	for i := range all {
		all[i] = noHit
	}
	return m.popup(box{title: v.title, head: head, body: lines, sel: sel, hits: append(all, hits...),
		keys: "enter change · + - number · x default · ↑ ↓ move · esc back"})
}

func (v *modSettingsView) click(m *dash, item, col int, _ bool) tea.Cmd {
	if item == noHit {
		return nil
	}
	return v.list.click(m, item, col)
}

func (v *modSettingsView) wheel(m *dash, d int) { v.list.key(m, arrow(d)) }

// modLogMax is how much of a mod's log l shows: its end.
const modLogMax = 64 << 10

// textView is read-only text over the popup, scrollable; it opens at
// its end when bottom is set (a log).
type textView struct {
	title, body, note string
	scroll            int
	bottom            bool
}

func (m *dash) openModLog(md mod.Mod) {
	tv := &textView{title: modName(md) + " · log", bottom: true}
	text, cut, err := m.src.ModLog(md.ID, modLogMax)
	switch {
	case err != nil:
		tv.note = "can't read it: " + oneLine(err.Error())
	case strings.TrimSpace(text) == "":
		tv.note = "No log yet: a code mod logs once it runs. A data mod has none."
	default:
		tv.body, _ = libText("log", []byte(text), false)
		if cut {
			tv.note = "shows its last " + sizeWords(modLogMax)
		}
	}
	m.push(tv)
}

func (tv *textView) lines(w int) []string {
	var out []string
	if tv.body != "" {
		for _, l := range strings.Split(strings.TrimRight(tv.body, "\n"), "\n") {
			out = append(out, strings.Split(ansi.Hardwrap(l, max(w, 10), true), "\n")...)
		}
	}
	if tv.note != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, th().Faint.Render(tv.note))
	}
	return out
}

func (tv *textView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if d, ok := scrollKeys[k.String()]; ok {
		tv.scroll = clampScroll(tv.scroll+d, len(tv.lines(m.inner(viewWidth))))
		tv.bottom = false
		return nil
	}
	if s := k.String(); s == "esc" || s == "q" {
		m.pop()
	}
	return nil
}

func (tv *textView) wheel(m *dash, d int) { tv.key(m, arrow(d)) }

func (tv *textView) render(m *dash) string {
	lines := tv.lines(m.inner(viewWidth))
	b := box{title: tv.title, body: lines, sel: -1, scroll: tv.scroll, keys: "↑ ↓ scroll · esc back"}
	if tv.bottom {
		// From the end: as many lines as the box shows.
		tv.scroll = max(len(lines)-m.boxRows(b), 0)
		b.scroll = tv.scroll
	}
	return m.popup(b)
}

// modUpdate fetches the mod's update in the background; one that asks
// for more, or changes text an agent obeys, shows what changed and asks
// first (mods design §8, Update).
func (m *dash) modUpdate(md mod.Mod) tea.Cmd {
	name := modName(md)
	if md.Builtin {
		m.fail(fmt.Errorf("%s is built in: it updates with tm", name))
		return nil
	}
	src := m.src
	m.msg = "fetching " + name + "…"
	return m.act(func() actionMsg {
		st, err := src.FetchModUpdate(md.ID)
		switch {
		case err != nil:
			return actionMsg{err: fmt.Errorf("%s: %w", name, err)}
		case st == nil:
			return actionMsg{msg: name + " is up to date"}
		}
		ch := mod.Diff(md.Manifest, md.Dir, st.Manifest, st.Dir)
		to := st.Manifest.Version
		if !ch.Asks() {
			defer st.Close()
			if err := src.ReplaceMod(md.ID, st, nil); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{msg: "updated " + name + " to " + to}
		}
		return actionMsg{then: func(m *dash) { m.push(&updateView{md: md, st: st, ch: ch}) }}
	})
}

// updateView asks before an update that asks for more permissions, or
// changes a rules addendum or agent manifest: what changed, then
// "Update <name>?". A mod that is on gets the new permissions with y.
type updateView struct {
	md     mod.Mod
	st     *mod.Staged
	ch     mod.Changes
	scroll int
}

func (uv *updateView) lines(w int) []string {
	from := "?"
	if uv.md.Manifest != nil {
		from = uv.md.Manifest.Version
	}
	out := []string{th().Head.Render(fmt.Sprintf("%s %s → %s", modName(uv.md), from, uv.st.Manifest.Version)), ""}
	for _, l := range strings.Split(strings.TrimRight(uv.ch.Describe(uv.st.Manifest), "\n"), "\n") {
		out = append(out, strings.Split(ansi.Hardwrap(strings.ReplaceAll(l, "\t", "    "), max(w, 10), true), "\n")...)
	}
	return append(out, "", th().Head.Render("Update "+modName(uv.md)+"?"))
}

func (uv *updateView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if d, ok := scrollKeys[k.String()]; ok {
		uv.scroll = clampScroll(uv.scroll+d, len(uv.lines(m.inner(viewWidth))))
		return nil
	}
	switch k.String() {
	case "y":
		m.pop()
		var grant []string
		if uv.md.State == mod.Enabled {
			grant = uv.ch.NewPerms
		}
		src, st, id, name, to := m.src, uv.st, uv.md.ID, modName(uv.md), uv.st.Manifest.Version
		return m.act(func() actionMsg {
			defer st.Close()
			if err := src.ReplaceMod(id, st, grant); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{msg: "updated " + name + " to " + to}
		})
	case "n", "esc":
		m.pop()
		uv.st.Close()
		m.msg = "not updated"
	}
	return nil
}

func (uv *updateView) wheel(m *dash, d int) { uv.key(m, arrow(d)) }

func (uv *updateView) render(m *dash) string {
	return m.popup(box{title: "Update " + modName(uv.md), body: uv.lines(m.inner(viewWidth)), sel: -1, scroll: uv.scroll, keys: "y yes · n no · ↑ ↓ scroll · esc cancel"})
}

// examplesView shows where terminatr's example mods are and how to
// install each (mods design §13). It installs and enables nothing.
type examplesView struct{ scroll int }

func (ev *examplesView) lines(w int) []string {
	out := wrapLines("terminatr's example mods: one small, working mod for each place a mod can show up, to see it work and to get ideas to build and share. Nothing here is installed or turned on: install one by path, then turn it on in this tab after its consent.", w)
	out = append(out, "", th().Accent.Render(ansi.Truncate(mod.ExamplesURL, w, "…")), "")
	out = append(out, faintLines("From a checkout: git clone "+mod.ExamplesRepo+", then", w)...)
	for _, e := range mod.Examples {
		out = append(out, "")
		out = append(out, ansi.Truncate(th().Head.Render(e.ID)+"  "+e.Surface+th().Faint.Render(" · "+strings.Join(mod.Permissions{}.Words(e.Perms), " · ")), w, "…"))
		out = append(out, ansi.Truncate("  "+mod.InstallLine(e.ID), w, "…"))
	}
	return out
}

func (ev *examplesView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if d, ok := scrollKeys[k.String()]; ok {
		ev.scroll = clampScroll(ev.scroll+d, len(ev.lines(m.inner(viewWidth))))
		return nil
	}
	if s := k.String(); s == "esc" || s == "q" {
		m.pop()
	}
	return nil
}

func (ev *examplesView) wheel(m *dash, d int) { ev.key(m, arrow(d)) }

func (ev *examplesView) render(m *dash) string {
	return m.popup(box{title: "Example mods", body: ev.lines(m.inner(viewWidth)), sel: -1, scroll: ev.scroll, keys: "↑ ↓ scroll · esc back"})
}

// noteModFailures says once, in the footer, that a mod failed since the
// last poll (mods design §9, Failing); the Mods tab has the details. The
// coordinator is never told: a mod failing is the human's business.
func (m *dash) noteModFailures() {
	now := map[string]bool{}
	var fresh []string
	for _, md := range m.data.Mods {
		if modFailure(m, md) == "" {
			continue
		}
		now[md.ID] = true
		if !m.modsFailed[md.ID] {
			fresh = append(fresh, modName(md))
		}
	}
	m.modsFailed = now
	if len(fresh) > 0 {
		m.msg = "mod " + joinAnd(fresh) + " failed · , then 3 for details"
	}
}

// projectRows are project slug's own settings, and the Mods line when a
// mod is limited to some projects (mods design §9): the limited mods
// that run for this one. enter opens the Mods tab, where they change.
func (m *dash) projectRows(slug string) []setting {
	rows := projectSettings(slug)
	if !slices.ContainsFunc(m.data.Mods, func(md mod.Mod) bool { return len(md.Projects) > 0 }) {
		return rows
	}
	return append(rows, setting{label: "Mods",
		help: "The mods limited to some projects that run for this one; the others run for every project. , then 3 lists them all; tm mod enable <id> --project " + slug + " adds one here.",
		value: func(m *dash) string {
			var names []string
			for _, md := range m.data.Mods {
				if len(md.Projects) > 0 && md.Runs(slug) {
					names = append(names, modName(md))
				}
			}
			if len(names) == 0 {
				return "none limited to it"
			}
			return strings.Join(names, ", ")
		},
		change: func(m *dash) tea.Cmd {
			m.openSettings()
			if sv, ok := m.top().(*settingsView); ok {
				sv.tab = tabMods
			}
			return nil
		}})
}
