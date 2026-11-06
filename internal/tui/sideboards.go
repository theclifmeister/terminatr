package tui

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Paired tasks in the sidebar's filter: while the filter shows, the open
// tasks of each paired project whose pages switch is on are read in the
// background (peer.pages-read), when it opens and now and then while it
// stays, never per keystroke. Typing searches only what was read, so a
// slow or offline server just shows fewer rows. The rows exist only among
// a filter's matches: the tree without a filter has no tasks. The same
// rules and limits as the Mac's palette (PaletteBoards, TerminatrKit).
// This server's tasks aren't here: F searches them, and nothing else
// reaches a paired server's.

const (
	// boardReadEvery is how long before a board is read again.
	boardReadEvery = 2 * time.Minute
	// boardsMax and boardMaxTasks are the most boards kept, and open
	// tasks kept of each, in board order.
	boardsMax     = 16
	boardMaxTasks = 200
)

// boardKey names a board by its server: the same project and task id on
// two servers are two boards.
type boardKey struct{ alias, project string }

// boardTask is what a task row shows of a task.
type boardTask struct {
	id                    int
	title, status         string
	stepsDone, stepsTotal int
}

type board struct {
	asked time.Time
	tasks []boardTask
}

// boardCache is what this console knows of paired servers' boards.
type boardCache struct {
	mu     sync.Mutex
	boards map[boardKey]*board
}

// sideBoards is this console's, as thisConsole is.
var sideBoards = &boardCache{}

// boardsReadable are the boards the filter may read: an up server's fresh
// projects with the pages switch on. An older tm, either side, has none:
// the same as the switch being off.
func boardsReadable(peers []proto.PeerMirror) []boardKey {
	var keys []boardKey
	for _, pm := range peers {
		if pm.State != proto.LinkUp {
			continue
		}
		for _, p := range pm.Projects {
			if p.Pages && !p.Stale && len(keys) < boardsMax {
				keys = append(keys, boardKey{pm.Alias, p.Project})
			}
		}
	}
	return keys
}

// due forgets the boards no longer readable and, while the filter shows
// (on), says which to read now, marked as asked so that none is read twice
// at once.
func (bc *boardCache) due(peers []proto.PeerMirror, on bool, now time.Time) []boardKey {
	readable := boardsReadable(peers)
	bc.mu.Lock()
	defer bc.mu.Unlock()
	for k := range bc.boards {
		if !slices.Contains(readable, k) {
			delete(bc.boards, k)
		}
	}
	if !on {
		return nil
	}
	var keys []boardKey
	for _, k := range readable {
		b := bc.boards[k]
		switch {
		case b == nil:
			if bc.boards == nil {
				bc.boards = map[boardKey]*board{}
			}
			bc.boards[k] = &board{asked: now}
		case now.Sub(b.asked) < boardReadEvery:
			continue
		default:
			b.asked = now
		}
		keys = append(keys, k)
	}
	return keys
}

// took takes a read's answer: its open tasks, cut down to what a row
// shows. A read that failed, or found the board no longer shared, loses
// its rows until the next read. One that lands after its board was
// forgotten stays out.
func (bc *boardCache) took(k boardKey, pages proto.PeerPages, err error) {
	var open []boardTask
	if err == nil {
		for _, t := range pages.Tasks {
			g := tasks.GroupOf(tasks.Status(t.Status))
			if g == tasks.DoneG || g == tasks.DroppedG {
				continue
			}
			if len(open) == boardMaxTasks {
				break
			}
			bt := boardTask{id: t.ID, title: oneLine(t.Title), status: oneLine(t.Status), stepsTotal: len(t.Steps)}
			for _, s := range t.Steps {
				if s.Done {
					bt.stepsDone++
				}
			}
			open = append(open, bt)
		}
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if b := bc.boards[k]; b != nil {
		b.tasks = open
	}
}

// tasksOf are the open tasks kept of a board.
func (bc *boardCache) tasksOf(k boardKey) []boardTask {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if b := bc.boards[k]; b != nil {
		return b.tasks
	}
	return nil
}

// reset forgets every board.
func (bc *boardCache) reset() {
	bc.mu.Lock()
	bc.boards = nil
	bc.mu.Unlock()
}

// sideBoardMsg says a board's read landed: the sidebar draws again.
type sideBoardMsg struct{}

// readSideBoards reads the boards due, each on its own so that a slow
// server holds up only its own rows.
func (m *dash) readSideBoards() tea.Cmd {
	keys := sideBoards.due(m.data.Peers, sideFilterNow().on(), time.Now())
	cmds := make([]tea.Cmd, 0, len(keys))
	src := m.src
	for _, k := range keys {
		cmds = append(cmds, func() tea.Msg {
			pages, err := src.PeerBoard(k.alias, k.project)
			sideBoards.took(k, pages, err)
			return sideBoardMsg{}
		})
	}
	return tea.Batch(cmds...)
}

// readSideBoards is the attach view's: a tick of its poll.
func (c *client) readSideBoards(peers []proto.PeerMirror) {
	src := &ServerSource{Paths: c.paths}
	for _, k := range sideBoards.due(peers, sideFilterNow().on(), time.Now()) {
		go func() {
			pages, err := src.PeerBoard(k.alias, k.project)
			sideBoards.took(k, pages, err)
			c.poke()
		}()
	}
}

// ref is a board task's name, as the thread working on it has it.
func (t boardTask) ref() string { return fmt.Sprintf("T%d", t.id) }

// withBoardTasks puts under each paired project that shares its pages the
// tasks kept of its board that match q (lower case), after its threads:
// a task whose thread matches too is that thread's row alone.
func withBoardTasks(rows []treeRow, q string) []treeRow {
	if q == "" {
		return rows
	}
	out := make([]treeRow, 0, len(rows))
	for i := 0; i < len(rows); i++ {
		r := rows[i]
		out = append(out, r)
		if r.kind != treePeerProject || !r.pages || r.stale || r.link.state != proto.LinkUp {
			continue
		}
		found := map[string]bool{}
		for i+1 < len(rows) && (rows[i+1].kind == treePeerCoord || rows[i+1].kind == treePeerThread) {
			i++
			out = append(out, rows[i])
			if rows[i].kind == treePeerThread && rows[i].task != "" && filterHit(rows[i], q) {
				found[rows[i].task] = true
			}
		}
		for _, t := range sideBoards.tasksOf(boardKey{r.peer, r.slug}) {
			if found[t.ref()] {
				continue
			}
			tr := treeRow{kind: treePeerTask, peer: r.peer, slug: r.slug, prog: r.prog, link: r.link,
				name: t.ref(), title: t.title, state: t.status, pct: -1}
			if t.stepsTotal > 0 {
				tr.pct = t.stepsDone * 100 / t.stepsTotal
			}
			if filterHit(tr, q) {
				out = append(out, tr)
			}
		}
	}
	return out
}

// taskDetails are the footer's two lines about a paired task's row.
func taskDetails(r treeRow) (string, string) {
	bits := []string{r.state}
	if r.pct >= 0 {
		bits = append(bits, fmt.Sprintf("%d%%", r.pct))
	}
	return r.server() + " / " + r.slug + " " + r.name + " " + r.title,
		strings.Join(append(bits, "read-only · its notes stay on "+r.server()), " · ")
}
