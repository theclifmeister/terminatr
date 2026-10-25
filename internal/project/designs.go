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
