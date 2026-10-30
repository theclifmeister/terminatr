package project

// Designs (docs/SPEC.md §5.1, designs.json): what tm knows of each
// design beyond the tasks that build it (their design and after
// fields): its design page, the task that made it, and the answers the
// user decided. Written only through `tm design set`; the vault renders
// designs/<name>.md and designs/<name>.canvas from it and the board.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// designsFile is the store, in the project folder.
const designsFile = "designs.json"

// A design's limits: its title, the URL of its page, each answer, and
// how many answers it keeps.
const (
	maxDesignTitle  = 200
	maxDesignURL    = 500
	MaxDesignAnswer = 300
	MaxAnswers      = 200
)

// Design is one design's record.
type Design struct {
	Name     string `json:"name"`               // as designs/<name>.md, and a task's design field
	Title    string `json:"title,omitempty"`    // "Obsidian vault"
	Task     string `json:"task,omitempty"`     // the design task, T714
	Artifact string `json:"artifact,omitempty"` // the design page, an https URL
	// Answers are the user's decided answers, one line each, in the
	// order the coordinator gave them.
	Answers []string `json:"answers,omitempty"`
	// Archived is set by tm design archive: the design is hidden
	// wherever designs are listed unless a list asks for the archived.
	Archived bool `json:"archived,omitempty"`
}

// DesignEdit is a change to a design: nil leaves a field alone, "" (or
// an empty list) clears it.
type DesignEdit struct {
	Title, Task, Artifact *string
	Answers               *[]string
}

type designStore struct {
	Designs []Design `json:"designs"`
}

// Designs are the recorded designs, by name.
func (p *Project) Designs() ([]Design, error) {
	data, err := os.ReadFile(p.Path(designsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st, err := decodeDesigns(data)
	return st.Designs, err
}

func decodeDesigns(data []byte) (designStore, error) {
	var st designStore
	if len(data) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("%s: %w", designsFile, err)
	}
	return st, nil
}

// SetDesign changes design name, adding it when it is new, and returns
// it and whether anything changed.
func (p *Project) SetDesign(name string, e DesignEdit) (Design, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" || !tasks.ValidDesign(name) {
		return Design{}, false, refuse("invalid-design", "design %q must be a name of letters, digits, '.', '_' and '-' (as designs/<name>.md)", name)
	}
	var title, task, artifact string
	var answers []string
	var err error
	if e.Title != nil {
		if title, err = checkDesignTitle(*e.Title); err != nil {
			return Design{}, false, err
		}
	}
	if e.Task != nil {
		task = strings.TrimSpace(*e.Task)
		if task != "" && !tasks.ValidRef(task) {
			return Design{}, false, refuse("invalid-design", "task %q is not a task id like T12", task)
		}
	}
	if e.Artifact != nil {
		if artifact, err = CheckDesignURL(*e.Artifact); err != nil {
			return Design{}, false, err
		}
	}
	if e.Answers != nil {
		if answers, err = checkAnswers(*e.Answers); err != nil {
			return Design{}, false, err
		}
	}
	var got Design
	changed := false
	err = mdfile.Update(p.Path(designsFile), func(old []byte) ([]byte, error) {
		st, err := decodeDesigns(old)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(st.Designs, func(d Design) bool { return d.Name == name })
		if i < 0 {
			st.Designs = append(st.Designs, Design{Name: name})
			i, changed = len(st.Designs)-1, true
		}
		d := &st.Designs[i]
		set := func(field *string, v *string, to string) {
			if v != nil && *field != to {
				*field, changed = to, true
			}
		}
		set(&d.Title, e.Title, title)
		set(&d.Task, e.Task, task)
		set(&d.Artifact, e.Artifact, artifact)
		if e.Answers != nil && !slices.Equal(d.Answers, answers) {
			d.Answers, changed = answers, true
		}
		got = *d
		slices.SortFunc(st.Designs, func(a, b Design) int { return strings.Compare(a.Name, b.Name) })
		b, err := json.MarshalIndent(st, "", "  ")
		return append(b, '\n'), err
	})
	return got, changed, err
}

// ArchiveDesign sets design name's archived flag (tm design archive,
// unarchive), recording the design when only a task names it, and
// returns it and whether anything changed. A name neither recorded nor
// named by a task is unknown-design.
func (p *Project) ArchiveDesign(name string, archived bool) (Design, bool, error) {
	name = strings.TrimSpace(name)
	if !tasks.ValidDesign(name) {
		return Design{}, false, refuse("invalid-design", "design %q must be a name of letters, digits, '.', '_' and '-' (as designs/<name>.md)", name)
	}
	builds, err := p.BoardDesigns()
	if err != nil {
		return Design{}, false, err
	}
	if !slices.ContainsFunc(builds, func(d DesignBuild) bool { return d.Name == name }) {
		return Design{}, false, refuse("unknown-design", "no design %s: tm design list --all --archived lists them", name)
	}
	var got Design
	changed := false
	err = mdfile.Update(p.Path(designsFile), func(old []byte) ([]byte, error) {
		st, err := decodeDesigns(old)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(st.Designs, func(d Design) bool { return d.Name == name })
		if i < 0 && !archived {
			got = Design{Name: name}
			return old, nil
		}
		if i < 0 {
			st.Designs = append(st.Designs, Design{Name: name})
			i = len(st.Designs) - 1
		}
		d := &st.Designs[i]
		if d.Archived != archived {
			d.Archived, changed = archived, true
		}
		got = *d
		slices.SortFunc(st.Designs, func(a, b Design) int { return strings.Compare(a.Name, b.Name) })
		b, err := json.MarshalIndent(st, "", "  ")
		return append(b, '\n'), err
	})
	return got, changed, err
}

// Shown reports whether a list shows d: an archived design only when it
// asks for the archived ones, a done one only when it asks for all.
func (d DesignBuild) Shown(all, archived bool) bool {
	if d.Archived {
		return archived
	}
	return all || d.Status() != DesignDone
}

// DesignBuild is a design with what the boards say of it: its design
// task, when it is on one, the tasks that build it (those that name it,
// but the design task, by id) and the design task's open questions. The
// vault's design notes and the mod API's designs.list both come from it.
type DesignBuild struct {
	Design
	DesignTask *tasks.Task
	Build      []*tasks.Task
	Questions  []Question
}

// DesignBuilds are every design that designs.json records or a task of
// boards names, by name.
func (p *Project) DesignBuilds(boards ...*tasks.Board) ([]DesignBuild, error) {
	recs, err := p.Designs()
	if err != nil {
		return nil, err
	}
	qs, err := p.Questions()
	if err != nil {
		return nil, err
	}
	byName := map[string]*DesignBuild{}
	get := func(name string) *DesignBuild {
		if byName[name] == nil {
			byName[name] = &DesignBuild{Design: Design{Name: name}}
		}
		return byName[name]
	}
	for _, r := range recs {
		if tasks.ValidDesign(r.Name) {
			get(r.Name).Design = r
		}
	}
	refs := map[string]*tasks.Task{}
	for _, b := range boards {
		for _, t := range b.Tasks {
			refs[t.Ref()] = t
			if t.Design != "" {
				get(t.Design)
			}
		}
	}
	out := make([]DesignBuild, 0, len(byName))
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		d := byName[name]
		d.DesignTask = refs[d.Task]
		for _, b := range boards {
			for _, t := range b.Tasks {
				if t.Design == name && t.Ref() != d.Task {
					d.Build = append(d.Build, refs[t.Ref()])
				}
			}
		}
		slices.SortFunc(d.Build, func(a, b *tasks.Task) int { return a.ID - b.ID })
		if d.Task != "" {
			for _, q := range qs {
				if q.Task == d.Task {
					d.Questions = append(d.Questions, q)
				}
			}
		}
		out = append(out, *d)
	}
	return out, nil
}

// BoardDesigns are DesignBuilds of the project's board and archive:
// what the vault's notes, the mod API's designs.list, the app API's
// design.list and the TUI's Designs tab show.
func (p *Project) BoardDesigns() ([]DesignBuild, error) {
	board, err := p.Tasks().Load()
	if err != nil {
		return nil, err
	}
	archive, err := p.Tasks().LoadArchive()
	if err != nil {
		return nil, err
	}
	return p.DesignBuilds(board, archive)
}

// The design statuses, from its build (DesignStatus).
const (
	DesignDesigning = "designing"
	DesignPlanned   = "planned"
	DesignBuilding  = "building"
	DesignDone      = "done"
)

// DesignStatus is a design's status from its build: done once every
// task is (but the dropped ones, which count as neither), building once
// one has started, planned before, designing with none.
func DesignStatus(build []*tasks.Task) string {
	if len(build) == 0 {
		return DesignDesigning
	}
	done, dropped, moving := 0, 0, false
	for _, t := range build {
		switch t.Status {
		case tasks.Done:
			done++
		case tasks.Dropped:
			dropped++
		case tasks.Started, tasks.Review, tasks.Blocked:
			moving = true
		}
	}
	switch {
	case done > 0 && done+dropped == len(build):
		return DesignDone
	case done > 0 || moving:
		return DesignBuilding
	}
	return DesignPlanned
}

// Status is d's status (DesignStatus).
func (d DesignBuild) Status() string { return DesignStatus(d.Build) }

// Done counts the build's done tasks.
func (d DesignBuild) Done() int {
	n := 0
	for _, t := range d.Build {
		if t.Status == tasks.Done {
			n++
		}
	}
	return n
}

func checkDesignTitle(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsFunc(s, isControl) {
		return "", refuse("invalid-design", "the title is one line")
	}
	if utf8.RuneCountInString(s) > maxDesignTitle {
		return "", refuse("invalid-design", "the title is over %d characters", maxDesignTitle)
	}
	return s, nil
}

// CheckDesignURL says whether s can be a design's page: an https URL
// that a Markdown link and a canvas link node hold as it is ("" clears).
func CheckDesignURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	switch {
	case len(s) > maxDesignURL:
		return "", refuse("invalid-design", "the URL is over %d characters", maxDesignURL)
	case err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil:
		return "", refuse("invalid-design", "%q is not an https URL", s)
	case strings.ContainsAny(s, " <>()[]{}\"'`\\|^") || strings.ContainsFunc(s, isControl):
		return "", refuse("invalid-design", "the URL %q has a character a link can't hold", s)
	}
	return s, nil
}

func checkAnswers(in []string) ([]string, error) {
	var out []string
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if strings.ContainsFunc(a, isControl) {
			return nil, refuse("invalid-design", "an answer is one line")
		}
		if n := utf8.RuneCountInString(a); n > MaxDesignAnswer {
			return nil, refuse("invalid-design", "an answer is %d characters; at most %d", n, MaxDesignAnswer)
		}
		out = append(out, a)
	}
	if len(out) > MaxAnswers {
		return nil, refuse("invalid-design", "%d answers; at most %d", len(out), MaxAnswers)
	}
	return out, nil
}

func isControl(r rune) bool { return r < ' ' || r == 0x7f }

// BuildDepths is each task's depth in the after graph among build: 0
// for one that comes after none of them, else one more than the deepest
// it comes after. A cycle (T1 after T2 after T1) is cut where it closes.
func BuildDepths(build []*tasks.Task) map[int]int {
	in := map[int]*tasks.Task{}
	for _, t := range build {
		in[t.ID] = t
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

// Ordered is d's build in after order: by depth in the after graph
// (BuildDepths), then id, as the vault's note and the consoles list it.
func (d DesignBuild) Ordered() []*tasks.Task {
	depth := BuildDepths(d.Build)
	out := slices.Clone(d.Build)
	slices.SortStableFunc(out, func(a, b *tasks.Task) int { return depth[a.ID] - depth[b.ID] })
	return out
}
