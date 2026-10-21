package tui

import (
	"cmp"

	tea "charm.land/bubbletea/v2"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The dashboard's sync with its view (docs/SPEC.md §5): the view's
// versions taken in (fromView), this console's changes sent in turn
// (call, inTurn), and the pending counts that keep a version from before
// one of them from undoing it.

// viewSync is what the dashboard knows of its view and of the changes on
// their way to it.
type viewSync struct {
	// The view (nil without one). viewSeq is its version when the
	// dashboard opened; selPending counts this console's selections on
	// their way, which the view's own doesn't overwrite meanwhile, and
	// treePending its changes of the folds (refold): a version of the
	// view from before one would fold back what was just unfolded.
	view     *ViewConn
	viewSeq  uint64
	viewTurn chan struct{} // closed once the last view call is answered (inTurn)
	viewSide SidebarLayout // the view's sidebar, as last sent or seen
	sideCols []string      // the view's sidebar columns (view.View.Columns)
	// wantCols, when set, is the column order this console's layout
	// ([ui] layout) asks the view for: sent once, if the view's differs.
	wantCols *[]string
	// wantCompact, when set, is the status bar's style this console's
	// layout asks the view for (view.status), sent once as wantCols.
	wantCompact *bool
	watch       <-chan struct{}
	stopWatch   func()
	selPending  int
	treePending int
	userSel     bool // the user moved the selection: tell the view
	// sideSent is the last sidebar keyboard row (dash.sideSel) sent; one
	// is on its way at a time (sideBusy), so they arrive in order, and
	// the view's own doesn't overwrite this console's meanwhile.
	sideSent string
	sideBusy bool
}

// viewMsg: the view has a new version. viewDoneMsg: a call on it
// answered.
type viewMsg struct{}

type viewDoneMsg struct {
	sel  bool // a selection
	side bool // a move of the sidebar's keyboard row
	tree bool // a change of what the sidebar's tree folds
	err  error
}

// waitView waits for the view's next version.
func (m *dash) waitView() tea.Cmd {
	if m.view == nil {
		return nil
	}
	watch, done := m.watch, m.done
	return func() tea.Msg {
		select {
		case <-watch:
			return viewMsg{}
		case <-done:
			return nil
		}
	}
}

// call runs a view action in the background.
func (m *dash) call(method string, p proto.ViewParams) tea.Cmd {
	vc, sel, side, tree := m.view, method == proto.MethodViewSelect, method == proto.MethodViewSideSel, method == proto.MethodViewTree
	if sel {
		m.selPending++
	}
	if tree {
		m.treePending++
	}
	if side {
		m.sideBusy, m.sideSent = true, p.Key
	}
	return m.inTurn(func() tea.Msg {
		_, err := vc.Do(method, p)
		return viewDoneMsg{sel: sel, side: side, tree: tree, err: err}
	})
}

// inTurn runs call, a view call, once the calls this console made before
// it are answered. Each runs in a command of its own, and those run
// concurrently: without turns a click on a project then one on its
// coordinator could reach the server as view.attach, then view.project,
// which takes the view back to the dashboard and ends the attach just
// begun (T283). Made in update, in the order of the keys and clicks.
func (m *dash) inTurn(call func() tea.Msg) tea.Cmd {
	prev, mine, done := m.viewTurn, make(chan struct{}), m.done
	m.viewTurn = mine
	return func() tea.Msg {
		defer close(mine)
		if prev != nil {
			select {
			case <-prev:
			case <-done:
				return nil
			}
		}
		return call()
	}
}

// fromView takes the view's new version: its selection (unless one of
// this console's is on its way), current project and sidebar. When it
// shows sessions now, from here or another console, the dashboard ends.
func (m *dash) fromView() tea.Cmd {
	v := m.view.View()
	if m.over != nil {
		switch {
		case v.Mode != view.ModeLayout:
			// Back to its dashboard, from another console: carry on as it.
			m.dropOver()
		case v.Focus != "" && v.Focus != m.over.Session:
			m.result.Attach = v.Focus
			return m.leave()
		}
	} else if v.Mode == view.ModeLayout && v.Focus != "" && v.Seq != m.viewSeq {
		m.result.Attach = cmp.Or(v.Focus, "view")
		return m.leave()
	}
	// The all-projects NEEDS YOU has no project row: "p:*" keeps the
	// list's own selection (showProject).
	if m.selPending == 0 && v.Selected != "" && v.Selected != "p:"+allProjects {
		m.sel = v.Selected
	}
	if m.current != v.Current {
		m.current, m.remoteTop = v.Current, 0
		m.rebuild()
	}
	if !m.sideBusy && m.sideSel == m.sideSent {
		m.sideSel, m.sideSent = v.SideSel, v.SideSel
	}
	if m.treePending == 0 {
		// Kept in the layout too, so that saving it keeps the view's.
		m.folds, m.layout.Tree = v.Tree, v.Tree
	}
	m.sideCols = v.Columns
	if !m.sideDrag && v.Sidebar != m.viewSide {
		m.layout.Sidebar, m.viewSide = v.Sidebar, v.Sidebar
		m.setWidth(m.winW)
	}
	// The info panel is the attach screen's; keep it so that saving the
	// layout here keeps it too.
	m.layout.Info = v.Info
	return m.waitView()
}

// sendSideSel tells the view where the sidebar's keyboard row is, when
// it moved since the last time and nothing is on its way.
func (m *dash) sendSideSel() tea.Cmd {
	if m.view == nil || m.sideBusy || m.sideSel == m.sideSent {
		return nil
	}
	return m.call(proto.MethodViewSideSel, proto.ViewParams{Key: m.sideSel})
}

// refold changes what the sidebar's tree folds: here at once, in the
// view, and in ui.json, as the tree new views start with. Folding never
// activates or deactivates a project (§5.1).
func (m *dash) refold(fn func(*view.Tree)) tea.Cmd {
	t := m.folds
	fn(&t)
	m.folds, m.layout.Tree = t, t
	m.saveLayout()
	if m.view == nil {
		return nil
	}
	return m.call(proto.MethodViewTree, proto.ViewParams{Tree: &t})
}

// foldSections folds (on) or unfolds the sidebar sections ids: here at
// once, in the view (view.sections, every console of it) and in ui.json,
// as refold. On a server of protocol 14 the folds stay this console's
// (ViewConn.Sections).
func (m *dash) foldSections(ids []string, on bool) tea.Cmd {
	t := m.folds
	if !t.FoldSections(ids, on) {
		return nil
	}
	m.folds, m.layout.Tree = t, t
	m.saveLayout()
	if m.view == nil {
		return nil
	}
	vc := m.view
	m.treePending++
	return m.inTurn(func() tea.Msg {
		_, err := vc.Sections(ids, on)
		return viewDoneMsg{tree: true, err: err}
	})
}
