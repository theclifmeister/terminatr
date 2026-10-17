package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// The dashboard's sidebar: its tree, clicks and keys, and the preview
// of the row under its cursor (sidebar.go draws it).

// sideClick handles a click on the sidebar, from under any popup: its
// border starts a drag; a click on a row gives the sidebar the keyboard,
// and a project row shows
// its dashboard, the coordinator row opens the coordinator, a thread row
// attaches its session.
func (m *dash) sideClick(mo tea.Mouse) tea.Cmd {
	if mo.Button != tea.MouseLeft {
		return nil
	}
	ck := sideClickAt(m.tree(), m.sideW(), m.h, m.sideFoot(), m.needsCount(), m.geom().sideCol(mo.X), mo.Y)
	switch {
	case ck.need:
		m.focus = areaSide
		return m.sideDo(sideNeedStep(m.sideKeysIn(), treeRow{}, false))
	case ck.head:
		m.focus, m.sideSel = areaSide, headKey
		return tea.Batch(m.sendSideSel(), m.openTarget(Target{Project: allProjects}))
	}
	var cmds []tea.Cmd
	if ck.sel != "" {
		// A click on a row gives the sidebar the keyboard, its cursor on
		// the row; it keeps it in the session the row attaches.
		m.focus, m.sideSel = areaSide, ck.sel
		cmds = append(cmds, m.sendSideSel())
	}
	if ck.drag {
		m.sideDrag = true
	}
	cmds = append(cmds, m.sectionCmd(ck.section))
	if m.busy {
		return tea.Batch(cmds...)
	}
	if ck.refold != nil {
		cmds = append(cmds, m.refold(ck.refold))
	}
	if ck.cant {
		m.msg = ck.msg
	}
	if ck.open != nil {
		cmds = append(cmds, m.openTarget(*ck.open))
	}
	return tea.Batch(cmds...)
}

// tree is the sidebar's tree, from the last poll: the listed project is
// current, and its row the one you are on; the keyboard's row is marked
// while the sidebar has the focus.
func (m *dash) tree() []treeRow {
	if m.treeMemo != nil {
		return m.treeMemo
	}
	rows := m.buildSideTree()
	if m.treeHold {
		m.treeMemo = rows
	}
	return rows
}

// buildSideTree builds tree's rows afresh.
func (m *dash) buildSideTree() []treeRow {
	rows := buildTree(m.data.Projects, m.data.Sessions, treeIn{current: m.treeCurrent(), ctxHint: m.data.ContextHint,
		tree: sideTree(m.data.Projects, m.folds), cols: m.sideCols, cells: m.data.Cells, peers: m.data.Peers})
	rows = sideRows(rows, m.data.NeedsFirst)
	if bandFits(m.sideW(), m.h, m.sideFoot()) {
		rows = withSections(rows, m.band())
		markUndrawn(rows, m.sideW(), m.h, m.sideFoot(), m.sideSel, m.focus == areaSide)
	}
	if m.focus == areaSide {
		markCursor(rows, m.sideW(), m.sideSel)
	}
	return rows
}

// fullTree is the sidebar's whole tree: nothing folded, no filter (w W).
func (m *dash) fullTree() []treeRow {
	rows := buildTree(m.data.Projects, m.data.Sessions, treeIn{current: m.treeCurrent(), ctxHint: m.data.ContextHint, tree: unfoldedTree(m.data.Projects), cols: m.sideCols, cells: m.data.Cells, peers: m.data.Peers})
	if m.data.NeedsFirst {
		rows = needsFirstOrder(rows)
	}
	return rows
}

// sideHeadIn is how the sidebar's PROJECTS header is drawn: its ⚑ N,
// the keyboard's row while the sidebar has it there, the row you are on
// while every project's NEEDS YOU shows.
func (m *dash) sideHeadIn() sideHeadIn {
	_, _, remote := m.remote()
	return sideHeadIn{needs: m.needsCount(), cursor: m.focus == areaSide && m.sideSel == headKey,
		here: m.over == nil && !remote && m.allShown()}
}

// band is what the sidebar's band of mod sections is built from.
func (m *dash) band() bandIn {
	return bandIn{list: m.mv.list, project: listProject(m.data, m.current), folds: m.folds, w: m.sideW(),
		mods: m.data.Mods, noMods: m.data.NoMods, cache: &m.mv.band, order: m.layout.sideSections,
		projOrder: m.layout.projSections}
}

// sideFoot is the rows the sidebar's footer takes (sideFootRows).
func (m *dash) sideFoot() int {
	return sideFootRows(m.sideW(), m.h, m.data.Server, m.layout.compact())
}

// sectionCmd runs what a key or a click on a band section does: folds
// or unfolds sections in the view (view.sections), or sends the mod the
// ui.click that opens its view.
func (m *dash) sectionCmd(st sideStep) tea.Cmd {
	var cmds []tea.Cmd
	if ids := st.sectionIDs(m.folds); len(ids) > 0 {
		cmds = append(cmds, m.foldSections(ids, st.foldOn))
	}
	if s := st.open; s != nil {
		src, p := m.src, sectionClick(s)
		cmds = append(cmds, func() tea.Msg { return modInputMsg{err: src.ModViewInput(p)} })
	}
	return tea.Batch(cmds...)
}

// sideKeyboard runs a key while the sidebar has the focus; ok is false
// for a key that isn't the sidebar's, which goes to the list's actions.
// A move previews the row the cursor rests on (previewSoon); enter or →
// goes into it, and the keyboard follows.
func (m *dash) sideKeyboard(key string) (tea.Cmd, bool) {
	st, ok := sideKeyFor(m.sideKeysIn(), key)
	if !ok {
		return nil, false
	}
	return m.sideDo(st), true
}

// sideKeysIn is what a sidebar key works on, here.
func (m *dash) sideKeysIn() sideKeysIn {
	return sideKeysIn{tree: m.tree, full: m.fullTree, w: m.sideW(), h: m.h, sel: m.sideSel}
}

// sideDo does what a sidebar key decided (sideKeyFor).
func (m *dash) sideDo(st sideStep) tea.Cmd {
	var cmds []tea.Cmd
	if st.msg != "" {
		m.msg = st.msg
	}
	if st.help {
		// A plain key that is a prefix command does what it does.
		return m.prefixCommand(boundKey("?"))
	}
	if st.back {
		m.focus = areaMain
	}
	if st.sel != "" && st.sel != m.sideSel {
		m.sideSel = st.sel
		cmds = append(cmds, m.sendSideSel(), m.previewSoon())
	}
	if st.refold != nil {
		cmds = append(cmds, m.refold(st.refold))
	}
	cmds = append(cmds, m.sectionCmd(st))
	var target *Target
	if t := st.target; t != nil {
		// Into the pane: the list, or the session attached.
		m.focus = areaMain
		if !st.here {
			target = t
		}
	}
	if target != nil && !targetActs(*target) {
		cmds = append(cmds, m.openTarget(*target))
		target = nil
	}
	if target == nil && st.toggle == "" && st.pin == "" && st.saveClear == "" {
		return tea.Batch(cmds...)
	}
	// What the key starts is an action: while one runs, it waits its
	// turn, on the row it was pressed on, as the settings keys do; the
	// moves above are this console's alone and happen at once.
	run := func(m *dash) tea.Cmd {
		switch {
		case target != nil:
			return m.openTarget(*target)
		case st.toggle != "":
			return m.toggleActive(st.toggle, st.activate)
		case st.saveClear != "":
			return m.saveClear(st.saveClear)
		}
		return m.setPinned(st.pin, st.pinOn)
	}
	if m.busy {
		m.queued = append(m.queued, run)
	} else {
		cmds = append(cmds, run(m))
	}
	return tea.Batch(cmds...)
}

// previewMsg: the sidebar's cursor may have rested on its row for
// previewDelay.
type previewMsg struct{ seq uint64 }

// previewSoon previews the sidebar's row once its cursor has rested on
// it for previewDelay.
func (m *dash) previewSoon() tea.Cmd {
	m.previewSeq++
	seq := m.previewSeq
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return previewMsg{seq} })
}

// preview shows what the sidebar's cursor rests on, the keyboard staying
// in the sidebar: a project's dashboard, or a session, which ends the
// dashboard (the session keeps the sidebar's keyboard, Result.SideFocus).
func (m *dash) preview(seq uint64) tea.Cmd {
	if seq != m.previewSeq || m.focus != areaSide || m.top() != nil || m.busy {
		return nil
	}
	r, ok := sideRowOf(shownRows(m.tree(), m.sideW()), m.sideSel, m.sideHeadIn().here)
	if !ok {
		return nil
	}
	t, ok := r.preview()
	switch {
	case !ok:
	case t.Session != "":
		return m.openTarget(t)
	case t.Project != m.current:
		return m.showProject(t.Project)
	}
	return nil
}

// setPinned pins a project to the top of the sidebar, or unpins it:
// here at once, and in its setting.
func (m *dash) setPinned(slug string, on bool) tea.Cmd {
	pinIn(m.data.Projects, slug, on)
	return m.lifecycle(slug, map[bool]string{true: "pin", false: "unpin"}[on])
}
