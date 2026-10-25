package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// A design and its build, on one board (T723, docs/SPEC.md §5.1,
// Vault): for each design that designs.json records or a task names,
// designs/<name>.md (its page, design task, answers, open questions and
// build) and designs/<name>.canvas, a JSON Canvas with the tasks in
// columns by their depth in the after graph. The canvas is the human's
// as much as tm's: tm keeps where they put its nodes and every node and
// edge of their own, and only adds, removes and recolours its own.

// designFront is a design note's properties.
type designFront struct {
	Name          string   `toml:"name" yaml:"name"`
	Title         string   `toml:"title" yaml:"title"`
	Project       string   `toml:"project" yaml:"project"`
	Status        string   `toml:"status" yaml:"status"`
	Task          string   `toml:"task,omitempty" yaml:"task,omitempty"`
	TaskStatus    string   `toml:"task_status,omitempty" yaml:"task_status,omitempty"`
	Artifact      string   `toml:"artifact,omitempty" yaml:"artifact,omitempty"`
	Tasks         int      `toml:"tasks" yaml:"tasks"`
	TasksDone     int      `toml:"tasks_done" yaml:"tasks_done"`
	Answers       int      `toml:"answers" yaml:"answers"`
	OpenQuestions int      `toml:"open_questions" yaml:"open_questions"`
	Canvas        string   `toml:"canvas" yaml:"canvas"`
	Tags          []string `toml:"tags" yaml:"tags"`
	TM            string   `toml:"tm" yaml:"tm"`
	CSSClasses    []string `toml:"cssclasses" yaml:"cssclasses"`
}

// boardTask is a task with where it is: on the board or archived.
type boardTask struct {
	*tasks.Task
	archived bool
}

// design is what a design's note and canvas come from.
type design struct {
	project.Design
	task      *boardTask         // the design task, when it is on a board
	build     []boardTask        // the tasks that name it, but the design task, by id
	depth     map[int]int        // each build task's depth in the after graph
	questions []project.Question // the design task's open questions
}

// DesignNote and DesignCanvas are the paths of design name's note and
// canvas.
func DesignNote(name string) string   { return "designs/" + name + ".md" }
func DesignCanvas(name string) string { return "designs/" + name + ".canvas" }

// designNotes renders the note and canvas of every design.
func designNotes(p *project.Project, boards ...*tasks.Board) ([]Note, error) {
	recs, err := p.Designs()
	if err != nil {
		return nil, err
	}
	qs, err := p.Questions()
	if err != nil {
		return nil, err
	}
	byName := map[string]*design{}
	get := func(name string) *design {
		if byName[name] == nil {
			byName[name] = &design{Design: project.Design{Name: name}}
		}
		return byName[name]
	}
	for _, r := range recs {
		if tasks.ValidDesign(r.Name) {
			get(r.Name).Design = r
		}
	}
	refs := map[string]*boardTask{}
	for i, b := range boards {
		for _, t := range b.Tasks {
			bt := &boardTask{t, i > 0}
			refs[t.Ref()] = bt
			if t.Design != "" {
				get(t.Design)
			}
		}
	}
	var notes []Note
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		d := byName[name]
		d.task = refs[d.Task]
		for _, b := range boards {
			for _, t := range b.Tasks {
				if t.Design == name && t.Ref() != d.Task {
					d.build = append(d.build, *refs[t.Ref()])
				}
			}
		}
		slices.SortFunc(d.build, func(a, b boardTask) int { return a.ID - b.ID })
		d.depth = depths(d.build)
		if d.Task != "" {
			for _, q := range qs {
				if q.Task == d.Task {
					d.questions = append(d.questions, q)
				}
			}
		}
		n, err := designNote(p.Slug, d)
		if err != nil {
			return nil, fmt.Errorf("design %s: %w", name, err)
		}
		notes = append(notes, n)
		if c, ok := designCanvas(p, d); ok {
			notes = append(notes, c)
		}
	}
	return notes, nil
}

// depths is each task's depth in the after graph among build: 0 for one
// that comes after none of them, else one more than the deepest it comes
// after. A cycle (T1 after T2 after T1) is cut where it closes.
func depths(build []boardTask) map[int]int {
	in := map[int]*tasks.Task{}
	for _, t := range build {
		in[t.ID] = t.Task
	}
	depth := map[int]int{}
	onPath := map[int]bool{}
	var visit func(id int) int
	visit = func(id int) int {
		if d, ok := depth[id]; ok {
			return d
		}
		if onPath[id] {
			return -1
		}
		onPath[id] = true
		d := 0
		for _, a := range in[id].After {
			if in[a] != nil {
				d = max(d, visit(a)+1)
			}
		}
		onPath[id] = false
		depth[id] = d
		return d
	}
	for _, t := range build {
		visit(t.ID)
	}
	return depth
}

// status is a design's status from its build: done once every task is,
// building once one has started, planned before, designing with none.
func (d *design) status() string {
	if len(d.build) == 0 {
		return "designing"
	}
	done, moving := 0, false
	for _, t := range d.build {
		switch t.Status {
		case tasks.Done:
			done++
		case tasks.Started, tasks.Review, tasks.Blocked:
			moving = true
		}
	}
	switch {
	case done == len(d.build):
		return "done"
	case done > 0 || moving:
		return "building"
	}
	return "planned"
}

func (d *design) title() string {
	if d.Title != "" {
		return d.Title
	}
	return d.Name
}

func (d *design) done() int {
	n := 0
	for _, t := range d.build {
		if t.Status == tasks.Done {
			n++
		}
	}
	return n
}

func designNote(slug string, d *design) (Note, error) {
	link := func(t *tasks.Task) string { return vaultlink.Link(slug, vaultlink.Task, t.Ref(), "") }
	f := designFront{
		Name: d.Name, Title: d.title(), Project: vaultlink.Link(slug, vaultlink.Project, slug, ""),
		Status: d.status(), Artifact: d.Artifact, Tasks: len(d.build), TasksDone: d.done(),
		Answers: len(d.Answers), OpenQuestions: len(d.questions),
		Canvas: vaultlink.Link(slug, vaultlink.Design, d.Name+".canvas", d.Name+".canvas"),
		Tags:   []string{"design"}, TM: "owned", CSSClasses: []string{"tm-owned"},
	}
	if len(d.questions) > 0 {
		f.Tags = append(f.Tags, "needs-you")
	}
	if d.Task != "" {
		f.Task = vaultlink.Link(slug, vaultlink.Task, d.Task, "")
	}
	if d.task != nil {
		f.TaskStatus = string(d.task.Status)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", heading(d.title()))
	var about []string
	if d.Task != "" {
		s := "Design task " + f.Task
		if d.task != nil {
			s += " · " + string(d.task.Status)
			if d.task.Thread != "" {
				s += " · " + vaultlink.Link(slug, vaultlink.Thread, d.task.Thread, "")
			}
		}
		about = append(about, s)
	}
	if d.Artifact != "" {
		about = append(about, "[Design page]("+d.Artifact+")")
	}
	about = append(about, "Canvas "+f.Canvas)
	b.WriteString(strings.Join(about, "\n") + "\n")
	if len(d.Answers) > 0 {
		b.WriteString("\n## Answers\n\n")
		for i, a := range d.Answers {
			fmt.Fprintf(&b, "%d. %s\n", i+1, line(slug, a))
		}
	}
	if len(d.questions) > 0 {
		b.WriteString("\n## Open questions\n\n")
		for _, q := range d.questions {
			fmt.Fprintf(&b, "- %s: %s\n", q.ID, line(slug, q.Question))
		}
	}
	if len(d.build) > 0 {
		fmt.Fprintf(&b, "\n## Build\n\n%d of %d done.\n\n", f.TasksDone, f.Tasks)
		for _, t := range d.ordered() {
			fmt.Fprintf(&b, "- %s %s · %s", link(t.Task), heading(t.Title), t.Status)
			var after []string
			for _, a := range t.After {
				after = append(after, vaultlink.Link(slug, vaultlink.Task, "T"+strconv.Itoa(a), ""))
			}
			if len(after) > 0 {
				b.WriteString(" · after " + strings.Join(after, ", "))
			}
			b.WriteString("\n")
		}
	}
	data, err := mdfile.Join(&f, []byte(b.String()))
	return Note{Rel: DesignNote(d.Name), Data: data, Guard: true}, err
}

// line is one line of someone's words (an answer, a question) as a
// design note or a canvas card shows it: guarded, its ids linked.
func line(slug, s string) string {
	return vaultlink.Text(slug, textsafe.Vault(textsafe.Line(s, 0)))
}

// ordered is the build by column, then id.
func (d *design) ordered() []boardTask {
	out := slices.Clone(d.build)
	slices.SortStableFunc(out, func(a, b boardTask) int { return d.depth[a.ID] - d.depth[b.ID] })
	return out
}

// The canvas's layout: the first column holds the design note, the
// artifact and the open questions; each depth of the build has a column
// after it. Sizes in canvas pixels.
const (
	firstW, noteH, linkH, questionH = 360, 220, 240, 160
	taskW, taskH                    = 280, 120
	gapX, gapY, pad                 = 80, 40, 40
	maxCanvas                       = 8 << 20
)

// JSON Canvas preset colours.
const (
	red, yellow, green, cyan = "1", "3", "4", "5"
)

// statusColor is a task node's colour: done green, started blue (the
// presets' cyan), review yellow, blocked red (needs you), else none.
func statusColor(s tasks.Status) string {
	switch s {
	case tasks.Done:
		return green
	case tasks.Started:
		return cyan
	case tasks.Review:
		return yellow
	case tasks.Blocked:
		return red
	}
	return ""
}

// node is one of tm's canvas nodes.
type node struct {
	id, typ, file, url, text, label, color string
	x, y, w, h                             float64
}

func (n node) fields() map[string]any {
	m := map[string]any{"id": n.id, "type": n.typ, "x": n.x, "y": n.y, "width": n.w, "height": n.h}
	for k, v := range map[string]string{"file": n.file, "url": n.url, "text": n.text, "label": n.label, "color": n.color} {
		if v != "" {
			m[k] = v
		}
	}
	return m
}

// edge is one of tm's canvas edges.
type edge struct{ id, from, to string }

// layout is where tm puts d's nodes on a new canvas, its group first,
// and its edges.
func layout(slug string, d *design) ([]node, []edge) {
	label := d.title()
	if d.Task != "" {
		label += " (" + d.Task + ")"
	}
	if n := len(d.Answers) + len(d.questions); n > 0 {
		label += fmt.Sprintf(" · decided %d/%d", len(d.Answers), n)
	}
	nodes := []node{{}, {id: "tm-note", typ: "file", file: slug + "/" + DesignNote(d.Name), w: firstW, h: noteH}}
	y := float64(noteH + gapY)
	if d.Artifact != "" {
		nodes = append(nodes, node{id: "tm-artifact", typ: "link", url: d.Artifact, y: y, w: firstW, h: linkH})
		y += linkH + gapY
	}
	for _, q := range d.questions {
		text := fmt.Sprintf("**%s** · open question\n\n%s", q.ID, line(slug, q.Question))
		nodes = append(nodes, node{id: "tm-" + q.ID, typ: "text", text: text, color: red, y: y, w: firstW, h: questionH})
		y += questionH + gapY
	}
	rows := map[int]int{}
	var edges []edge
	for _, t := range d.ordered() {
		col := d.depth[t.ID]
		id := "tm-" + t.Ref()
		nodes = append(nodes, node{
			id: id, typ: "file", file: slug + "/" + TaskNote(t.Ref(), t.archived), color: statusColor(t.Status),
			x: firstW + gapX + float64(col*(taskW+gapX)), y: float64(rows[col] * (taskH + gapY)), w: taskW, h: taskH,
		})
		rows[col]++
		from := 0
		for _, a := range t.After {
			if _, ok := d.depth[a]; ok {
				edges = append(edges, edge{fmt.Sprintf("tm-e-T%d-%s", a, t.Ref()), "tm-T" + strconv.Itoa(a), id})
				from++
			}
		}
		if from == 0 {
			edges = append(edges, edge{"tm-e-note-" + t.Ref(), "tm-note", id})
		}
	}
	right, bottom := 0.0, 0.0
	for _, n := range nodes[1:] {
		right, bottom = max(right, n.x+n.w), max(bottom, n.y+n.h)
	}
	nodes[0] = node{id: "tm-group", typ: "group", label: label, x: -pad, y: -pad, w: right + 2*pad, h: bottom + 2*pad}
	return nodes, edges
}

// designCanvas renders d's canvas over the one there: false when that
// can't be read as a canvas, which tm then leaves alone.
func designCanvas(p *project.Project, d *design) (Note, bool) {
	rel := DesignCanvas(d.Name)
	var old []byte
	if fi, err := os.Lstat(p.Path("designs", d.Name+".canvas")); err == nil && fi.Mode().IsRegular() {
		if fi.Size() > maxCanvas {
			return Note{}, false
		}
		if old, err = os.ReadFile(p.Path("designs", d.Name+".canvas")); err != nil {
			return Note{}, false
		}
	}
	nodes, edges := layout(p.Slug, d)
	data, err := mergeCanvas(old, nodes, edges)
	if err != nil {
		return Note{}, false
	}
	return Note{Rel: rel, Data: data}, true
}

type rawObject = map[string]json.RawMessage

func str(o rawObject, key string) string {
	var s string
	json.Unmarshal(o[key], &s)
	return s
}

func num(o rawObject, key string) (float64, bool) {
	var f float64
	err := json.Unmarshal(o[key], &f)
	return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}

// mergeCanvas puts tm's nodes and edges into canvas old (none when
// empty). A node of tm's that is there (by id, or a file node by its
// file, which tm then takes over) keeps its place and size; a new one
// goes where the layout puts it, moved as far as the human moved the
// group, which grows to take it. Every other node and edge stays as it
// is, but an edge whose end is gone; tm's that it no longer draws go.
func mergeCanvas(old []byte, nodes []node, edges []edge) ([]byte, error) {
	top := rawObject{}
	var oldNodes, oldEdges []rawObject
	if len(bytes.TrimSpace(old)) > 0 {
		if err := json.Unmarshal(old, &top); err != nil {
			return nil, err
		}
		if raw, ok := top["nodes"]; ok {
			if err := json.Unmarshal(raw, &oldNodes); err != nil {
				return nil, err
			}
		}
		if raw, ok := top["edges"]; ok {
			if err := json.Unmarshal(raw, &oldEdges); err != nil {
				return nil, err
			}
		}
	}
	mine := map[string]int{} // tm's node id → its index in nodes
	byFile := map[string]int{}
	for i, n := range nodes {
		mine[n.id] = i
		if n.file != "" {
			byFile[n.file] = i
		}
	}
	// Match the canvas's nodes to tm's: by id, then a file node by file.
	at := make([]int, len(oldNodes)) // tm's index, or -1
	found := make([]bool, len(nodes))
	renamed := map[string]string{}
	for j, o := range oldNodes {
		at[j] = -1
		if i, ok := mine[str(o, "id")]; ok && !found[i] {
			at[j], found[i] = i, true
		}
	}
	for j, o := range oldNodes {
		if at[j] >= 0 || strings.HasPrefix(str(o, "id"), "tm-") || str(o, "type") != "file" {
			continue
		}
		if i, ok := byFile[str(o, "file")]; ok && !found[i] {
			at[j], found[i] = i, true
			renamed[str(o, "id")] = nodes[i].id
		}
	}
	// Where the human moved the group, new nodes move too.
	var dx, dy float64
	for j, o := range oldNodes {
		if at[j] == 0 {
			x, okx := num(o, "x")
			y, oky := num(o, "y")
			if okx && oky {
				dx, dy = x-nodes[0].x, y-nodes[0].y
			}
		}
	}
	placed := slices.Clone(nodes)
	for i := range placed {
		if !found[i] {
			placed[i].x += dx
			placed[i].y += dy
		}
	}
	for j, o := range oldNodes {
		i := at[j]
		if i < 0 {
			continue
		}
		n := &placed[i]
		x, okx := num(o, "x")
		y, oky := num(o, "y")
		w, okw := num(o, "width")
		h, okh := num(o, "height")
		if okx && oky && okw && okh && w > 0 && h > 0 {
			if i == 0 {
				w, h = max(w, n.w), max(h, n.h)
			}
			n.x, n.y, n.w, n.h = x, y, w, h
		}
	}
	// The group first (behind the rest), then the canvas's nodes in
	// their order, then tm's new ones.
	var outNodes []any
	ids := map[string]bool{}
	add := func(v any, id string) {
		outNodes = append(outNodes, v)
		ids[id] = true
	}
	add(placed[0].fields(), placed[0].id)
	for j, o := range oldNodes {
		id := str(o, "id")
		switch i := at[j]; {
		case i == 0:
		case i > 0:
			add(placed[i].fields(), placed[i].id)
		case strings.HasPrefix(id, "tm-") || id == "" || ids[id]:
			// tm's, no longer drawn; or no id, or a second one.
		default:
			add(o, id)
		}
	}
	for i := 1; i < len(placed); i++ {
		if !found[i] {
			add(placed[i].fields(), placed[i].id)
		}
	}
	var outEdges []any
	edgeIDs := map[string]bool{}
	for _, o := range oldEdges {
		id := str(o, "id")
		if strings.HasPrefix(id, "tm-") || id == "" || edgeIDs[id] {
			continue
		}
		for _, end := range []string{"fromNode", "toNode"} {
			if to, ok := renamed[str(o, end)]; ok {
				o[end], _ = json.Marshal(to)
			}
		}
		if ids[str(o, "fromNode")] && ids[str(o, "toNode")] {
			outEdges = append(outEdges, o)
			edgeIDs[id] = true
		}
	}
	for _, e := range edges {
		outEdges = append(outEdges, map[string]any{"id": e.id, "fromNode": e.from, "fromSide": "right", "toNode": e.to, "toSide": "left"})
	}
	if outEdges == nil {
		outEdges = []any{}
	}
	var err error
	if top["nodes"], err = encode(outNodes); err != nil {
		return nil, err
	}
	if top["edges"], err = encode(outEdges); err != nil {
		return nil, err
	}
	return encode(top)
}

// encode is v as JSON with tabs, as Obsidian writes a canvas, without
// escaping < > &.
func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	err := enc.Encode(v)
	return b.Bytes(), err
}
