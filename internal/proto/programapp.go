package proto

// A program as the consoles read and act on it (the programs design §11,
// §12): its dashboard, the director's writes the human makes too (tm
// program route, task edit, send and read), the program question's
// answer, the umbrella's accept and send-back, and delete. Every one is
// the human's (the app is the human), and refused programs-off while
// programs are off, but program.delete, which clears a dormant program
// too. The route's request and the dashboard's result are package
// server's (they carry package program's records).

const (
	// MethodProgramGet reads a program's dashboard (server.ProgramDetail).
	MethodProgramGet = "program.get"
	// MethodProgramRoute files tasks in the program's members, as tm
	// program route (server.ProgramRouteParams, program.RouteResult).
	MethodProgramRoute = "program.route"
	// MethodProgramTaskEdit changes a member's task as tm program task
	// edit <project>/T<n> does (ProgramTaskEditParams,
	// ProgramTaskEditResult).
	MethodProgramTaskEdit = "program.task.edit"
	// MethodProgramSend writes on the program channel, as tm program
	// send (ProgramSendParams, ProgramSendResult).
	MethodProgramSend = "program.send"
	// MethodProgramRead reads a program channel message framed as data,
	// as tm program read (ProgramReadParams, ProgramReadResult).
	MethodProgramRead = "program.read"
	// MethodProgramAnswer answers the program's open question
	// (ProgramAnswerParams), which goes to every member question it
	// covers: the user's alone (program.Answer). The result is
	// program.AnswerResult.
	MethodProgramAnswer = "program.answer"
	// MethodProgramUmbrellaAccept marks an umbrella task in review done;
	// MethodProgramUmbrellaSendBack reopens it with the user's note
	// (ProgramUmbrellaParams, ProgramUmbrellaResult).
	MethodProgramUmbrellaAccept   = "program.umbrella.accept"
	MethodProgramUmbrellaSendBack = "program.umbrella.send-back"
	// MethodProgramDelete deletes a program, as tm program delete
	// (ProgramDeleteParams, ProgramDeleteResult): refused while its
	// director runs; programs on or off.
	MethodProgramDelete = "program.delete"
	// MethodProgramOpen opens a program's director, as tm program open
	// and the sidebar's director row do (since protocol 38): the running
	// one, else one started as server.OpenDirector starts it
	// (ProgramOpenParams, ProgramOpenResult). The human's; a start is
	// journaled in the program as the caller (director.start).
	MethodProgramOpen = "program.open"
)

// ProgramOpenParams are program.open's: the program, and the size of
// the pane a started director gets (80x24 when unset).
type ProgramOpenParams struct {
	Program string `json:"program"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
}

// ProgramOpenResult is the director's session, and whether this call
// started it.
type ProgramOpenResult struct {
	Session string `json:"session"`
	Started bool   `json:"started,omitempty"`
}

// ProgramParams name a program.
type ProgramParams struct {
	Program string `json:"program"`
}

// ProgramTaskEditParams are program.task.edit's: member task Ref
// ("acme-api/T12", "mini/acme-ios/T7") of Program. An empty field is
// left as it is; Note adds a note line. Title, Repo and Status (ready
// or open) only on a task the director filed, before it starts.
type ProgramTaskEditParams struct {
	Program  string   `json:"program"`
	Ref      string   `json:"ref"`
	Priority string   `json:"priority,omitempty"`
	After    []string `json:"after,omitempty"`
	Design   string   `json:"design,omitempty"`
	Title    string   `json:"title,omitempty"`
	Repo     string   `json:"repo,omitempty"`
	Status   string   `json:"status,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// ProgramTaskEditResult is what program.task.edit did: a local
// member's task Result (edited, unchanged), or for a paired member's
// the program-task message Sent, whose answer comes as an item.
type ProgramTaskEditResult struct {
	Result string `json:"result,omitempty"`
	Sent   string `json:"sent,omitempty"`
}

// ProgramSendParams are program.send's: from the program (From "" or
// its slug) to member To, or from member From to the program (To the
// program's slug), as the human for that side.
type ProgramSendParams struct {
	Program string `json:"program"`
	From    string `json:"from,omitempty"`
	To      string `json:"to"`
	Text    string `json:"text"`
	Re      string `json:"re,omitempty"`
}

// ProgramSendResult names the message sent (M-…), its state (sent, or
// queued for a paired member) and size.
type ProgramSendResult struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Bytes int    `json:"bytes"`
}

// ProgramReadParams are program.read's: message ID of the program's
// channel.
type ProgramReadParams struct {
	Program string `json:"program"`
	ID      string `json:"id"`
}

// ProgramReadResult is the message framed as data, as tm program read
// prints it.
type ProgramReadResult struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ProgramAnswerParams are program.answer's: the program's open question
// Ref ("Q2"), answered with Choices (its options' labels) or Other, the
// user's own words.
type ProgramAnswerParams struct {
	Program string   `json:"program"`
	Ref     string   `json:"ref"`
	Choices []string `json:"choices,omitempty"`
	Other   string   `json:"other,omitempty"`
}

// ProgramUmbrellaParams name umbrella Task ("T4") of Program's board,
// with the user's Note (send-back needs one).
type ProgramUmbrellaParams struct {
	Program string `json:"program"`
	Task    string `json:"task"`
	Note    string `json:"note,omitempty"`
}

// ProgramUmbrellaResult is the umbrella's task result (done, open).
type ProgramUmbrellaResult struct {
	Result string `json:"result"`
}

// ProgramDeleteParams are program.delete's: Confirm repeats the slug,
// the user's confirm.
type ProgramDeleteParams struct {
	Program string `json:"program"`
	Confirm string `json:"confirm"`
}

// ProgramDeleteResult says where the program's folder went ("" when it
// had only its settings).
type ProgramDeleteResult struct {
	Trash string `json:"trash,omitempty"`
}

// MethodProgramWake starts a member project's coordinator that isn't
// running, as enter on its coordinator row does, so what the director
// filed or sent isn't stuck until the user opens the project (T972):
// tm program wake. The program's own director's (under propose and
// delegate: waking starts no threads) and the human's. Refused while
// programs are off, for a project that isn't a local member (a paired
// member's is its user's), for an inactive or paused one, and for every
// member while the program is paused: those stay the user's. A start is journaled in the member and the program
// as the caller (coordinator.wake). The result is ProgramOpenResult's:
// the coordinator's session, and whether this call started it.
const MethodProgramWake = "program.wake"

// ProgramWakeParams are program.wake's: member Project of Program (a
// director's own when empty), and the size of the pane a started
// coordinator gets (80x24 when unset).
type ProgramWakeParams struct {
	Program string `json:"program,omitempty"`
	Project string `json:"project"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
}
