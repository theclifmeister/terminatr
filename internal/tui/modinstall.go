package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// Installing a mod from the Mods tab (mods design §9), as tm mod install
// does: I asks for a folder or a git URL (the subfolder forms included),
// or the examples list (E) gives the selected example's; tm fetches and
// checks it in the background, showing how long it has taken, then draws
// its consent in full. Y installs it off, as the CLI does, then asks
// whether to turn it on: two answers, as tm mod install then tm mod
// enable are, so an install alone never runs a mod's code. Turning it on
// grants every permission it asks for, which the consent just showed for
// this very copy. tm refuses all of it inside an agent.

// installPrompt asks for the mod to install, starting from text.
func (m *dash) installPrompt(text string) {
	m.push(&inputView{title: "Install a mod",
		label:  "A folder, or a git URL: <git-url>[//<subfolder>][@<tag>], or a GitHub /tree/<ref>/<folder> link. It installs off; you see what it may do first.",
		text:   text,
		cancel: "nothing installed",
		submit: m.installMod})
}

// installMod fetches the mod at arg in the background, fetchView over
// the tab until it comes, then shows its consent.
func (m *dash) installMod(arg string) tea.Cmd {
	fv := &fetchView{arg: arg, start: time.Now()}
	m.push(fv)
	src := m.src
	return m.act(func() actionMsg {
		st, err := src.FetchMod(arg)
		out := actionMsg{then: func(m *dash) { fv.fetched(m, st, err) }}
		if err != nil {
			out.msg = "not installed: " + oneLine(err.Error())
		}
		return out
	})
}

// fetchView stands over the Mods tab while a mod is fetched and
// checked: what, and for how long; then why it can't be installed, if
// it can't. esc drops it: what comes is thrown away.
type fetchView struct {
	arg   string
	start time.Time
	err   error
}

// fetched is what the fetch brought: the consent when fv still waits
// for it, else nothing (the staged copy is removed).
func (fv *fetchView) fetched(m *dash, st *mod.Staged, err error) {
	if !slices.Contains(m.stack, overlay(fv)) {
		st.Close()
		return
	}
	if err != nil {
		fv.err = err
		return
	}
	m.close(fv)
	m.push(&installView{st: st})
}

func (fv *fetchView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		m.pop()
		if fv.err == nil {
			m.msg = "nothing installed"
		}
	case "enter":
		if fv.err != nil {
			m.pop()
			m.installPrompt(fv.arg)
		}
	}
	return nil
}

func (fv *fetchView) render(m *dash) string {
	w := m.inner(dialogWidth)
	body := wrapInput("", fv.arg, w)
	for i, l := range body {
		body[i] = th().Accent.Render(l)
	}
	body = append(body, "")
	if fv.err != nil {
		for _, l := range wrapLines("Can't install it: "+oneLine(fv.err.Error()), w) {
			body = append(body, th().Bad.Render(l))
		}
		return m.popup(box{title: "Install a mod", body: body, sel: -1, keys: "enter change it · esc back", dialog: true})
	}
	secs := int(time.Since(fv.start).Seconds())
	body = append(body, fmt.Sprintf("Fetching and checking it… %ds", secs))
	body = append(body, faintLines("A git URL is cloned, a folder copied; then it is checked as tm mod install checks it: its mod.toml, the mod API, at most 2000 files and 64 MB, no link leading out. Nothing is installed yet.", w)...)
	return m.popup(box{title: "Install a mod", body: body, sel: -1, keys: "esc cancel", dialog: true})
}

// installView is the consent of a fetched mod, as tm mod install shows
// it: Y installs it, off, then asks whether to turn it on.
type installView struct {
	st     *mod.Staged
	scroll int
}

// mod is the staged mod as the consent draws it: its files are the
// staged copy's.
func (iv *installView) mod() mod.Mod {
	return mod.Mod{ID: iv.st.Manifest.ID, Dir: iv.st.Dir, Manifest: iv.st.Manifest}
}

func (iv *installView) lines(w int) []string {
	md := iv.mod()
	out := consentLines(md, "Install "+modName(md)+"?", w)
	from := faintLines("From "+iv.st.Source.String()+". It installs off: tm asks next whether to turn it on.", w)
	n := len(out) - 1
	return append(out[:n:n], append(from, out[n])...)
}

func (iv *installView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if scrollKey(&iv.scroll, k, len(iv.lines(m.inner(viewWidth)))) {
		return nil
	}
	switch k.String() {
	case "Y":
		m.pop()
		src, st, md := m.src, iv.st, iv.mod()
		name, ver := modName(md), md.Manifest.Version
		return m.act(func() actionMsg {
			defer st.Close()
			if err := src.InstallMod(st); err != nil {
				return actionMsg{err: fmt.Errorf("%s: %w", name, err)}
			}
			return actionMsg{msg: "installed " + name + " " + ver + ", off", then: func(m *dash) {
				m.selectMod(md.ID)
				m.confirmNo("Turn it on", "Installed "+name+", off. Turn it on now? It runs with every permission it asks for, as its consent showed.",
					"installed "+name+", off: enter turns it on", func() tea.Cmd { return m.modAct(md.ID, "enable", name+" on") })
			}}
		})
	case "N", "esc":
		m.pop()
		iv.st.Close()
		m.msg = "nothing installed"
	}
	return nil
}

func (iv *installView) wheel(m *dash, d int) { iv.key(m, arrow(d)) }

func (iv *installView) render(m *dash) string {
	return m.popup(box{title: "Install " + modName(iv.mod()), body: iv.lines(m.inner(viewWidth)), sel: -1, scroll: iv.scroll,
		keys: "Y install · N no · ↑ ↓ scroll · esc cancel"})
}

// selectMod selects mod id in the Mods tab once the next poll lists it.
func (m *dash) selectMod(id string) {
	for _, o := range m.stack {
		if sv, ok := o.(*settingsView); ok {
			sv.mods.want = id
		}
	}
}

// examplesView lists terminatr's example mods (mods design §13), each
// with where it shows up, what it may do and its install line: I
// installs the selected one, after its consent, as the line would. A
// built-in one is installed already; its lines say how to copy it under
// another id.
type examplesView struct{ sel int }

func (ev *examplesView) lines(w int) (out []string, sel int) {
	out = wrapLines("terminatr's example mods: one small, working mod for each place a mod can show up, to see it work and to get ideas to build and share. Nothing here is installed or turned on: I installs the selected one, after its consent; it starts off.", w)
	out = append(out, "", th().Accent.Render(ansi.Truncate(mod.ExamplesURL, w, "…")))
	for i, e := range mod.Examples {
		out = append(out, "")
		head := e.ID + "  " + e.Surface
		if i == ev.sel {
			sel = len(out)
			out = append(out, th().Sel.Render(fit(head+" · "+strings.Join(e.Words(), " · "), w)))
		} else {
			out = append(out, ansi.Truncate(th().Head.Render(e.ID)+"  "+e.Surface+th().Faint.Render(" · "+strings.Join(e.Words(), " · ")), w, "…"))
		}
		for _, l := range mod.InstallLines(e.ID) {
			out = append(out, th().Faint.Render(ansi.Truncate("  "+l, w, "…")))
		}
	}
	return out, sel
}

func (ev *examplesView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if d, ok := scrollKeys[s]; ok {
		ev.sel = moveSel(ev.sel, d, len(mod.Examples))
		return nil
	}
	switch s {
	case "esc", "q":
		m.pop()
	case "I", "enter":
		e := mod.Examples[ev.sel]
		if mod.IsBundled(e.ID) {
			m.fail(fmt.Errorf("%s is built in already: to change it, copy it under another id, as its lines say", e.ID))
			return nil
		}
		return m.installMod(mod.ExampleSource(e.ID))
	}
	return nil
}

func (ev *examplesView) wheel(m *dash, d int) { ev.key(m, arrow(d)) }

func (ev *examplesView) render(m *dash) string {
	lines, sel := ev.lines(m.inner(viewWidth))
	return m.popup(box{title: "Example mods", body: lines, sel: sel, keys: "I install · ↑ ↓ move · esc back"})
}
