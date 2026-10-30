package proto

// A project's designs (docs/SPEC.md §5.1, Vault), read as the TUI's
// Designs tab and the mod API's designs.list read them, for a console
// that isn't on this machine's files (the Mac app, a paired device). The
// human's; read only. The server's hello lists CapDesigns.
const (
	// MethodDesignList lists a project's designs (DesignListParams): a
	// DesignListResult. This server's projects only (since protocol 28).
	MethodDesignList = "design.list"
)

// CapDesigns says the server answers design.list.
const CapDesigns = "designs"

// DesignListParams name the project.
type DesignListParams struct {
	Project string `json:"project"`
}

// DesignListResult is design.list's result: the designs by name.
type DesignListResult struct {
	Designs []Design `json:"designs"`
}

// Design is one of a project's designs: the record the mod API's
// designs.list gives (its name, title, status from its build, design
// task and that task's status, page, build task ids by id, how many are
// done, decided answers and open questions), and Build, its build tasks
// in after order with their title, status and after, which a console
// lists without the board's done and archived tasks. Archived (protocol
// 30): tm design archive hid it; a console lists it only when asked.
type Design struct {
	Name       string           `json:"name"`
	Title      string           `json:"title"`
	Status     string           `json:"status"`
	Task       string           `json:"task,omitempty"`
	TaskStatus string           `json:"task_status,omitempty"`
	Artifact   string           `json:"artifact,omitempty"`
	Tasks      []string         `json:"tasks"`
	TasksDone  int              `json:"tasks_done"`
	Answers    []string         `json:"answers"`
	Questions  []DesignQuestion `json:"questions"`
	Build      []DesignTask     `json:"build"`
	Archived   bool             `json:"archived,omitempty"`
}

// DesignQuestion is an open question of a design's task (tm ask).
type DesignQuestion struct {
	ID       string `json:"id"` // "Q3"
	Header   string `json:"header"`
	Question string `json:"question"`
}

// DesignTask is a build task: its id ("T12"), title, status, the tasks
// it comes after, and whether it is archived.
type DesignTask struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	After    []string `json:"after,omitempty"`
	Archived bool     `json:"archived,omitempty"`
}
