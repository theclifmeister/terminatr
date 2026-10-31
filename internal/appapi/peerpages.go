package appapi

import (
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// A paired server's project pages, read only (app API 35; the server's
// caps say peer-pages, docs/SPEC.md §16.17): a shared project's board,
// overview and library, while that server's project pages switch for
// this one is on for it. Any other answer is "not shared": pages-off
// (the switch is off), no-pages (its tm shares none), not-shared (the
// project isn't any more). Its memory never crosses.
type (
	// PeerPagesParams are peer.pages-read's and peer.library-list's: the
	// paired server's alias and its project.
	PeerPagesParams struct {
		Alias   string `json:"alias"`
		Project string `json:"project"`
	}
	// PeerLibraryReadParams are peer.library-read's: the file of thread
	// ID named Name, as peer.library-list gave it, the chunk from
	// Offset.
	PeerLibraryReadParams struct {
		Alias   string `json:"alias"`
		Project string `json:"project"`
		ID      string `json:"id"`
		Name    string `json:"name"`
		Offset  int64  `json:"offset,omitempty"`
	}
)

// PeerPages is peer.pages-read's result: the project's goal, PROJECT.md's
// body, how many repositories (never their paths), and its board's tasks
// in board order, done ones too, with their steps but never their notes.
// Truncated: the board was too long to cross whole (done tasks go
// first).
type PeerPages struct {
	Project   string `json:"project"`
	Goal      string `json:"goal,omitempty"`
	Body      string `json:"body,omitempty"`
	Repos     int    `json:"repos"`
	Tasks     []Task `json:"tasks"`
	Truncated bool   `json:"truncated,omitempty"`
}

func peerPagesParams(p PeerPagesParams) proto.PeerPagesReadParams {
	return proto.PeerPagesReadParams{Alias: p.Alias, Project: p.Project}
}

func peerLibraryReadParams(p PeerLibraryReadParams) proto.PeerLibraryReadReadParams {
	return proto.PeerLibraryReadReadParams{Alias: p.Alias, Project: p.Project, Thread: p.ID, Name: p.Name, Offset: p.Offset}
}

func peerPagesOf(p proto.PeerPages) PeerPages {
	out := PeerPages{Project: p.Project, Goal: p.Overview.Goal, Body: p.Overview.Body, Repos: p.Overview.Repos,
		Tasks: make([]Task, 0, len(p.Tasks)), Truncated: p.Truncated}
	for _, t := range p.Tasks {
		task := Task{ID: t.ID, Ref: (&tasks.Task{ID: t.ID}).Ref(), Title: t.Title, Status: t.Status, Thread: t.Thread,
			Created: t.Created, Updated: t.Updated}
		for i, s := range t.Steps {
			task.Steps = append(task.Steps, Step{N: i + 1, Text: s.Text, Done: s.Done})
		}
		out.Tasks = append(out.Tasks, task)
	}
	return out
}

// peerLibraryOf is a paired server's library as Library: its threads'
// files, so the app groups them as its own.
func peerLibraryOf(l proto.PeerLibrary) Library {
	out := Library{Files: make([]LibraryFile, 0, len(l.Files))}
	for _, f := range l.Files {
		out.Files = append(out.Files, LibraryFile{Thread: f.Thread, Task: f.Task, Title: f.Title, State: f.State,
			Name: f.Name, Size: f.Size, Time: f.Time})
	}
	return out
}

// peerDesignsOf is a paired server's designs (peer.design-list, app API
// 36) as design.list's, so the app draws them as its own: no page (its
// URL stays there; PageStays says it has one) and no open questions
// (they cross only as needs); Tasks are its build's ids.
func peerDesignsOf(r proto.PeerDesigns) Designs {
	out := Designs{Designs: make([]Design, 0, len(r.Designs))}
	for _, d := range r.Designs {
		ad := Design{Name: d.Name, Title: d.Title, Status: d.Status, Task: d.Task, TaskStatus: d.TaskStatus,
			PageStays: d.Page, Tasks: []string{}, TasksDone: d.TasksDone, Answers: append([]string{}, d.Answers...),
			Questions: []DesignQuestion{}, Build: []DesignTask{}, Archived: d.Archived, Working: d.Working}
		for _, t := range d.Build {
			ad.Tasks = append(ad.Tasks, t.ID)
			ad.Build = append(ad.Build, DesignTask{ID: t.ID, Title: t.Title, Status: t.Status,
				After: append([]string(nil), t.After...), Archived: t.Archived, Working: t.Working})
		}
		out.Designs = append(out.Designs, ad)
	}
	return out
}
