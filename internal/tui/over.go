package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// overState is the popup over a session (Over): the dashboard drawn as a
// popup on the session's screen, which a feed keeps live.
type overState struct {
	// over: a popup over a session (Over), nil for the dashboard; feed
	// follows the session under it, nil for none. leaving: a popup action
	// asked the view to show a session.
	over    *Over
	feed    *paneFeed
	leaving bool
	// awaitView: over a session for a mod's view (Over.View), the popup
	// waits for it until then.
	awaitView time.Time
}

// awaitView is how long a popup over a session waits for the view a mod
// opened (Over.View) to be listed.
const awaitView = 3 * time.Second

// feedMsg: the session under the popup drew something.
type feedMsg struct{}

// waitFeed waits for the session under the popup to change.
func (m *dash) waitFeed() tea.Cmd {
	f, done := m.feed, m.done
	if f == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-f.wake:
		case <-f.end:
		case <-done:
			return nil
		}
		time.Sleep(feedInterval) // a burst of output is one redraw
		return feedMsg{}
	}
}

// fromFeed draws the session's screen as it is now under the popup.
func (m *dash) fromFeed() tea.Cmd {
	if m.over == nil || m.feed == nil {
		return nil
	}
	if rows := m.feed.screen(); rows != nil {
		m.over.Screen = m.over.place(rows, len(m.over.Screen))
	}
	select {
	case <-m.feed.end:
		return nil // the last of it
	default:
		return m.waitFeed()
	}
}

// dropOver turns the popup over a session into the dashboard.
func (m *dash) dropOver() {
	if m.feed != nil {
		m.feed.close()
	}
	m.over, m.feed = nil, nil
}

// overDone says the popup over a session closed: its key ran and no
// overlay is left.
func (m *dash) overDone() bool {
	return m.over != nil && m.loaded && m.then == "" && m.top() == nil && !m.busy && time.Now().After(m.awaitView)
}

// Over is a popup opened over a session (docs/SPEC.md §4): prefix then
// a, i, t, ,, N or ? in a session, or a click on a server's header in
// its sidebar. The dashboard runs its key and draws the popup over the
// session's screen, dimmed, rather than over the list,
// and follows the session's output meanwhile (paneFeed); the view stays
// as it is. When the popup closes the dashboard ends with
// Attach set, and the caller attaches again. Should the view go back to
// its dashboard meanwhile (another console, prefix then d), it carries on
// as the dashboard.
type Over struct {
	Key     string   // the dashboard key that opens the popup
	Task    int      // with Key t: the task the Tasks tab shows (opened), 0 for none
	Server  string   // with Key N: the server the Network popup opens on (peerKey), "" for the first
	Project string   // the project it is about: the session's, or a sidebar row's
	Session string   // the session it is drawn over
	Title   string   // how the header names the session
	Screen  []string // the session's area as shown, plain, a line per row
	// Where the pane is in Screen: after X columns of padding, at rows
	// Y to Y+H, from its row Top. The session's output goes there.
	X, Y, H, Top int
	// View: a mod asked to open one of its views after a command run in
	// the session (attachcommands.go); the popup is that view, from the
	// mod's open requests after After.
	View  bool
	After uint64
}
