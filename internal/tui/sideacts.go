package tui

import "github.com/theclifmeister/terminatr/internal/view"

// What the mouse does on the sidebar, decided once for the dashboard
// (dash.sideClick, dash.sideMenu) and a session (client.sideMouse,
// client.sideMenu): each runs the decision its own way, the dashboard
// with tea.Cmds, a session under its lock.

// sideClick is what a left click on the sidebar does (sideClickAt).
type sideClick struct {
	// need: the header's ⚑ N, the first row that needs you, as w.
	need bool
	// head: the PROJECTS header, every project's NEEDS YOU, the sidebar
	// keeping the keyboard on it.
	head bool
	// drag: the border, whose drag starts.
	drag bool
	// sel is the key of the row clicked, which gives the sidebar the
	// keyboard, its cursor on the row; "" for none. row is that row.
	sel string
	row treeRow
	// section is a band section's click, header or body: as enter on
	// its header (sectionStep); the zero step for none.
	section sideStep
	// refold folds or unfolds what the inactive divider or a paired
	// server's header does.
	refold func(*view.Tree)
	// cant: the row opens nothing, and msg says why.
	cant bool
	msg  string
	// open is where the row goes.
	open *Target
}

// sideClickAt is what a left click at sidebar cell (x, y) does, the
// sidebar drawn from all, w wide and h high with foot rows of footer,
// needs rows needing you.
func sideClickAt(all []treeRow, w, h, foot, needs, x, y int) sideClick {
	if sideFlagAt(all, w, h, foot, needs, x, y) {
		return sideClick{need: true}
	}
	if sideHeadAt(all, w, h, foot, x, y) {
		return sideClick{head: true}
	}
	r, ok, border := sideHitAt(all, w, h, foot, x, y)
	var c sideClick
	if ok && !border {
		c.sel, c.row = r.key(), r
	}
	t, can, why := r.target()
	switch {
	case border:
		c.drag = true
	case !ok:
	case r.kind == treeSection:
		c.section, _ = sectionStep(r, sideEnter)
	case r.kind == treeDivider:
		c.refold = foldDivider(r)
	case r.kind == treeServer && !r.link.local:
		c.refold = foldPeer(r, !r.fold)
	case !can:
		c.cant, c.msg = true, why
	default:
		c.open = &t
	}
	return c
}

// What a sidebar row's menu item does (sideMenuItem.act).
const (
	menuShow        = iota // show the project's dashboard
	menuCoordinator        // open the project's coordinator
	menuFold               // fold or unfold (sideMenuItem.fold)
	menuPopup              // the project popup, on sideMenuItem.popup's tab
	menuActive             // activate or deactivate the project
	menuPin                // pin the project to the top, or unpin it
	menuOpen               // go where the row points (its target)
	menuAttach             // attach the thread's session
)

// sideMenuItem is an item of a sidebar row's menu.
type sideMenuItem struct {
	label string
	// key is the sidebar's key that does the same, "" for none; enter
	// says enter on the row does too.
	key   string
	enter bool
	act   int
	fold  func(*view.Tree)
	// popup is the prefix command that opens the popup (a, t or i).
	popup string
}

// sideMenuItems are the right-click menu of sidebar row r, popups
// whether its project's popups may open from it.
func sideMenuItems(r treeRow, popups bool) []sideMenuItem {
	var out []sideMenuItem
	add := func(it sideMenuItem) { out = append(out, it) }
	fold := func(label, key string, fn func(*view.Tree)) {
		add(sideMenuItem{label: label, key: key, act: menuFold, fold: fn})
	}
	popup := func(label, key string) {
		if popups {
			add(sideMenuItem{label: label, act: menuPopup, popup: key})
		}
	}
	switch r.kind {
	case treeProject:
		add(sideMenuItem{label: "show its dashboard", act: menuShow})
		if r.inactive {
			label, key := expandLabel(r)
			fold(label, key, foldProject(r, !r.fold))
		} else {
			add(sideMenuItem{label: "open its coordinator", act: menuCoordinator})
			label, key := foldLabel(r)
			fold(label, key, foldProject(r, !r.fold))
		}
		popup("project popup", "A")
		popup("tasks", "T")
		popup("inbox", "I")
		add(sideMenuItem{label: activeLabel(r), key: "S", act: menuActive})
		add(sideMenuItem{label: pinLabel(r), key: "P", act: menuPin})
	case treeDivider:
		label, key := foldLabel(r)
		fold(label, key, foldDivider(r))
	case treeCoordinator:
		add(sideMenuItem{label: coordLabel(r), enter: true, act: menuOpen})
		popup("project popup", "A")
	case treePeerProject:
		// A paired server's: its read-only dashboard, and its fold.
		add(sideMenuItem{label: "show its dashboard", enter: true, act: menuOpen})
		if !r.stale {
			label, key := foldLabel(r)
			fold(label, key, foldPeer(r, !r.fold))
		}
	default:
		add(sideMenuItem{label: "attach", enter: true, act: menuAttach})
	}
	return out
}
