package server

// The agent tools (docs/SPEC.md §8.6, Mods; mods design §1 and §2, tier
// 5). A session's agent is offered typed tools: a thread its own four,
// for what it otherwise does through the shell (tm report, tm status,
// tm task steps, tm done), and both threads and coordinators the tools
// enabled user mods declare ([[provides.tools]]) for its role and
// project. tm mcp lists them with tool.list and serves each call with
// tool.run. The caller's pid says which session calls, so a call runs
// as that session, with no permission rule. A thread tool's input
// is checked here and turned into the command it stands for, which runs
// in this server as `tm` would through cli.run. A mod tool's input is
// checked against its schema and the call goes to the mod's host (the
// supervisor starts an idle one), whose handler answers it under the
// mod's grants. An agent without tm mcp keeps the shell commands.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// maxModTool bounds a tool call's body: a report and its framing.
const maxModTool = thread.MaxReport + 16<<10

// ModReportTool is the input of the report tool: the report's sections,
// which the server writes out in the format tm report takes.
type ModReportTool struct {
	PR       string   `json:"pr,omitempty"`
	Report   string   `json:"report"`
	Next     []string `json:"next"`
	Check    []string `json:"check,omitempty"`
	Remember []string `json:"remember,omitempty"`
	Attach   []string `json:"attach,omitempty"`
}

// ModStatusTool is the input of the status tool: tm status's flags.
type ModStatusTool struct {
	Percent  *int   `json:"percent,omitempty"`
	Activity string `json:"activity,omitempty"`
	NeedsYou string `json:"needs_you,omitempty"`
}

// ModStepsTool is the input of the steps tool: steps of the thread's
// task to check, uncheck and add, done in that order.
type ModStepsTool struct {
	Check   []int    `json:"check,omitempty"`
	Uncheck []int    `json:"uncheck,omitempty"`
	Add     []string `json:"add,omitempty"`
}

// ModDoneTool is the input of the done tool.
type ModDoneTool struct {
	Summary string `json:"summary,omitempty"`
}

// ModToolResult is what a tool call answers: the command's output, for
// the model; with a non-2xx status, why it was refused.
type ModToolResult struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// errModTool is a tool input that isn't one: the model fixes it.
var errModTool = errors.New("bad tool input")

func toolErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errModTool, fmt.Sprintf(format, a...))
}

// modToolTimeout bounds a mod tool's call, its host's start included.
const modToolTimeout = 60 * time.Second

func toolLine(description string, maxLen int) map[string]any {
	m := map[string]any{"type": "string", "minLength": 1, "description": description}
	if maxLen > 0 {
		m["maxLength"] = maxLen
	}
	return m
}

func toolLines(description string, maxLen int) map[string]any {
	return map[string]any{"type": "array", "items": toolLine("One line.", maxLen), "description": description}
}

func toolObject(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// ThreadTools are a thread's own tools, as tm mcp lists them.
var ThreadTools = []proto.ToolDef{
	{
		Name: "report",
		Description: "Hand in your report to the coordinator (what `tm report` does): whenever you finish or stop to wait. " +
			"Each field is a section of the report; the server checks the format and answers with the report number, or why it refused.",
		InputSchema: toolObject(map[string]any{
			"pr":       map[string]any{"type": "string", "pattern": "^https://", "description": "The PR URL, if you opened one: https://github.com/<owner>/<repo>/pull/<n>, https://dev.azure.com/<org>/<project>/_git/<repo>/pullrequest/<n> or https://gitlab.com/<group>/<project>/-/merge_requests/<n>."},
			"report":   map[string]any{"type": "string", "minLength": 1, "description": "The report itself, in Markdown: what you did, what you assumed, any repository outside the project you used. Subheadings are ### (## is reserved for the sections)."},
			"next":     toolLines("What happens next: one imperative action per item, at most 100 characters each.", 100),
			"check":    toolLines("Optional: how the user can see the change working (what to run, where to look), a few lines.", 0),
			"remember": toolLines("Optional: lessons for the project, one per item.", 0),
			"attach":   toolLines("Optional: files meant for the user, by path (relative to your worktree, or absolute).", 0),
		}, "report", "next"),
	},
	{
		Name: "status",
		Description: "Set your thread status (what `tm status` does). Use needs_you when you are blocked on the human; " +
			"percent and activity only when you have neither task steps nor a todo list.",
		InputSchema: toolObject(map[string]any{
			"needs_you": toolLine("The question or decision you are blocked on, for the human; one line, at most 300 characters.", 300),
			"percent":   map[string]any{"type": "integer", "minimum": 0, "maximum": 100, "description": "How far along you are, 0-100."},
			"activity":  toolLine("What you are doing now, one line, at most 100 characters.", 100),
		}),
	},
	{
		Name: "steps",
		Description: "Work your task's steps (what `tm task steps` does): check the ones you finished, uncheck one done too early, " +
			"or add your plan as steps when the task has none. Steps are numbered from 1; the server answers what changed.",
		InputSchema: toolObject(map[string]any{
			"check":   map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}, "description": "Step numbers to check, in order."},
			"uncheck": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}, "description": "Step numbers to uncheck."},
			"add":     toolLines("Steps to add at the end, one per item, in order.", 0),
		}),
	},
	{
		Name:        "done",
		Description: "Mark your task finished (what `tm done` does), once your report is in. It refuses when no report came since your last prompt.",
		InputSchema: toolObject(map[string]any{
			"summary": toolLine("Optional: a one-line summary, at most 80 characters.", 80),
		}),
	},
}

// CoordinatorTools are a coordinator's own tools: reads it otherwise
// does through the shell.
var CoordinatorTools = []proto.ToolDef{
	{
		Name: "link_files",
		Description: "List the files sent with messages between this project and its linked projects (what `tm link files --json` does), newest first: " +
			"id, message, project, name, size, state and progress. A file offered to this project waits for the user to accept it; read a received one with `tm link read M-<n> --file NAME`.",
		InputSchema: toolObject(map[string]any{
			"project": toolLine("Optional: only the files with this linked project (a slug, or <server>/<slug>).", 100),
		}),
	},
}

// modToolRef is a mod tool a session is offered.
type modToolRef struct {
	mod  string
	tool mod.Tool
	def  proto.ToolDef
}

// sessionTools are the tools session rec's agent is offered: a thread's
// own, then the mod tools for its role and project, by name.
func (s *Server) sessionTools(rec SessionRecord) []proto.ToolDef {
	out := []proto.ToolDef{}
	if rec.Role == proto.RoleThread && thread.ValidID(rec.Thread) {
		out = append(out, ThreadTools...)
	}
	if rec.Role == proto.RoleCoordinator && rec.Project != "" {
		out = append(out, CoordinatorTools...)
	}
	if (rec.Role == proto.RoleThread && thread.ValidID(rec.Thread) || rec.Role == proto.RoleCoordinator) && rec.Project != "" {
		out = append(out, SearchTools...)
	}
	for _, t := range s.modToolsFor(rec) {
		out = append(out, t.def)
	}
	return out
}

// modToolsFor are the tools of the enabled code mods that run for rec's
// project and hold the tools grant, for its role, sorted by name; none
// while no mod hosts run, and only bundled mods' in safe mode. The registry is read anew within a second, so
// a call made after the human revokes the grant is refused.
func (s *Server) modToolsFor(rec SessionRecord) []modToolRef {
	if rec.Project == "" || (rec.Role != proto.RoleThread && rec.Role != proto.RoleCoordinator) || s.modSup.Load() == nil {
		return nil
	}
	reg := s.modRegistry()
	if reg == nil {
		return nil
	}
	var out []modToolRef
	for _, m := range reg.Mods {
		if m.Manifest == nil || !m.Manifest.IsCode() || !m.Runs(rec.Project) || !m.Granted(mod.PermTools) || s.noMods && !m.Builtin {
			continue
		}
		for _, t := range m.Manifest.Provides.Tools {
			if !t.For(rec.Role) {
				continue
			}
			out = append(out, modToolRef{mod: m.ID, tool: t, def: proto.ToolDef{
				Name:        mod.ToolName(m.ID, t.ID),
				Description: fmt.Sprintf("[mod: %s] %s", m.ID, strings.TrimSpace(mod.Clean(t.Description, true))),
				InputSchema: t.Schema(),
			}})
		}
	}
	slices.SortFunc(out, func(a, b modToolRef) int { return strings.Compare(a.def.Name, b.def.Name) })
	return out
}

// runTool runs tool name with input b as session record rec, and
// answers with an HTTP status that says how it went.
func (s *Server) runTool(rec SessionRecord, name string, b []byte) (int, ModToolResult) {
	if len(b) > maxModTool {
		return http.StatusBadRequest, ModToolResult{Error: toolErr("input is over %d bytes", maxModTool).Error()}
	}
	// The coordinator's own tool first: its name is in a mod tool's form.
	if rec.Role == proto.RoleCoordinator && name == "link_files" {
		return s.runLinkFiles(rec, b)
	}
	if isSearchTool(name) {
		if strings.TrimSpace(string(b)) == "" {
			b = []byte("{}")
		}
		return s.runSearchTool(rec, name, b)
	}
	if _, _, ok := mod.ParseToolName(name); ok {
		return s.runModTool(rec, name, b)
	}
	if rec.Role != proto.RoleThread || !thread.ValidID(rec.Thread) {
		return http.StatusForbidden, ModToolResult{Error: "these tools are a thread's"}
	}
	if s.opts.RunCLI == nil {
		return http.StatusServiceUnavailable, ModToolResult{Error: "this server runs no project commands"}
	}
	runs, err := toolRuns(name, b, func() string { return s.taskRef(rec.Project, rec.Thread) })
	if err != nil {
		code := http.StatusBadRequest
		if !errors.Is(err, errModTool) {
			code = http.StatusNotFound
		}
		return code, ModToolResult{Error: err.Error()}
	}
	c := caller.Caller{Kind: caller.Thread, Project: rec.Project, Thread: rec.Thread}
	var out strings.Builder
	defer s.kick()
	for _, p := range runs {
		p.Cwd = rec.Cwd
		res := s.opts.RunCLI(p, c)
		out.WriteString(res.Stdout)
		if res.Code != 0 {
			msg := strings.TrimSpace(res.Stderr)
			if msg == "" {
				msg = fmt.Sprintf("tm %s exited %d", p.Args[0], res.Code)
			}
			if done := strings.TrimSpace(out.String()); done != "" {
				msg = done + "\n" + msg
			}
			return http.StatusUnprocessableEntity, ModToolResult{Error: msg}
		}
	}
	return http.StatusOK, ModToolResult{Text: strings.TrimSpace(out.String())}
}

// runLinkFiles runs the coordinator's link_files tool: tm link files
// --json for its own project.
func (s *Server) runLinkFiles(rec SessionRecord, b []byte) (int, ModToolResult) {
	var in struct {
		Project string `json:"project"`
	}
	if len(strings.TrimSpace(string(b))) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return http.StatusBadRequest, ModToolResult{Error: toolErr("%v", err).Error()}
		}
	}
	if s.opts.RunCLI == nil {
		return http.StatusServiceUnavailable, ModToolResult{Error: "this server runs no project commands"}
	}
	args := []string{"link", "files", "--json"}
	if in.Project != "" {
		args = append(args, in.Project)
	}
	res := s.opts.RunCLI(proto.CLIRunParams{Args: args, Cwd: rec.Cwd}, caller.Caller{Kind: caller.Coordinator, Project: rec.Project})
	if res.Code != 0 {
		return http.StatusUnprocessableEntity, ModToolResult{Error: strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout))}
	}
	return http.StatusOK, ModToolResult{Text: strings.TrimSpace(res.Stdout)}
}

// runModTool runs mod tool name (<mod>_<tool>) for session rec: the
// tool must be one rec is offered, its input must fit its schema, and
// the mod's host answers.
func (s *Server) runModTool(rec SessionRecord, name string, b []byte) (int, ModToolResult) {
	tools := s.modToolsFor(rec)
	i := slices.IndexFunc(tools, func(t modToolRef) bool { return t.def.Name == name })
	if i < 0 {
		return http.StatusNotFound, ModToolResult{Error: fmt.Sprintf("no tool %q for this session: its mod isn't enabled for %s or lacks the tools grant, or doesn't offer it to a %s", name, rec.Project, rec.Role)}
	}
	t := tools[i]
	input, err := checkToolInput(t.tool, b)
	if err != nil {
		return http.StatusBadRequest, ModToolResult{Error: err.Error()}
	}
	sup := s.modSup.Load()
	if sup == nil {
		return http.StatusServiceUnavailable, ModToolResult{Error: "this server runs no mods"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), modToolTimeout)
	defer cancel()
	start := time.Now()
	res, err := sup.CallTool(ctx, t.mod, modproto.ToolCall{Tool: t.tool.ID, Input: input,
		Project: rec.Project, Role: rec.Role, Thread: rec.Thread, Session: rec.ID})
	who := rec.Role + " of " + rec.Project
	if rec.Thread != "" {
		who = "thread " + rec.Thread + " of " + rec.Project
	}
	outcome := "ok"
	if err != nil {
		outcome = "error: " + err.Error()
	}
	s.modLog(t.mod, fmt.Sprintf("tool %s for %s (%s) in %s: %s", t.tool.ID, who, rec.ID, time.Since(start).Round(time.Millisecond), firstLine(outcome)))
	var perr *modproto.Error
	switch {
	case err == nil:
		return http.StatusOK, ModToolResult{Text: res.Text}
	case errors.As(err, &perr) && perr.Code == modproto.CodeToolFailed:
		return http.StatusUnprocessableEntity, ModToolResult{Error: perr.Message}
	case errors.As(err, &perr):
		return http.StatusBadGateway, ModToolResult{Error: fmt.Sprintf("mod %s: %s", t.mod, perr.Message)}
	}
	return http.StatusServiceUnavailable, ModToolResult{Error: err.Error()}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// checkToolInput checks body against tool t's input schema: a JSON object
// (empty is {}) with only its properties, each of its type, and the
// required ones there. It returns the object as the mod gets it.
func checkToolInput(t mod.Tool, body []byte) (json.RawMessage, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		body = []byte("{}")
	}
	var in map[string]json.RawMessage
	if err := json.Unmarshal(body, &in); err != nil || in == nil {
		return nil, toolErr("the input is not a JSON object")
	}
	for _, k := range slices.Sorted(maps.Keys(in)) {
		spec, ok := t.Input.Properties[k]
		if !ok {
			return nil, toolErr("%s is not one of the tool's inputs", k)
		}
		want, _ := spec["type"].(string)
		if got := jsonType(in[k]); got != want && !(want == "number" && got == "integer") {
			return nil, toolErr("%s: want %s, not %s", k, want, got)
		}
	}
	for _, k := range t.Input.Required {
		if _, ok := in[k]; !ok {
			return nil, toolErr("%s is required", k)
		}
	}
	return json.RawMessage(body), nil
}

// jsonType is JSON schema's name for raw's type: string, integer,
// number, boolean, array, object or null.
func jsonType(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "invalid"
	}
	switch x := v.(type) {
	case string:
		return "string"
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) {
			return "integer"
		}
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "null"
}

// callerSession is the live session of the caller with peer pid: a
// thread's or a coordinator's.
func (s *Server) callerSession(c caller.Caller) (SessionRecord, *proto.Error) {
	if c.Kind != caller.Thread && c.Kind != caller.Coordinator {
		return SessionRecord{}, proto.Errorf(proto.ErrRefused, "these tools are a thread's or a coordinator's")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.records {
		if _, live := s.sessions[id]; !live || r.Project != c.Project {
			continue
		}
		if c.Kind == caller.Thread && r.Role == proto.RoleThread && r.Thread == c.Thread ||
			c.Kind == caller.Coordinator && r.Role == proto.RoleCoordinator {
			return r, nil
		}
	}
	if c.Kind == caller.Thread {
		return SessionRecord{}, proto.Errorf(proto.ErrRefused, "thread %s has no live session", c.Thread)
	}
	return SessionRecord{}, proto.Errorf(proto.ErrRefused, "%s's coordinator has no live session", c.Project)
}

// toolRunRPC serves tool.run: the session is the caller's, found from
// the peer pid, and gives the cwd.
func (s *Server) toolRunRPC(p proto.ToolRunParams, who caller.Caller) (any, *proto.Error) {
	rec, perr := s.callerSession(who)
	if perr != nil {
		return nil, perr
	}
	_, res := s.runTool(rec, p.Name, p.Input)
	return proto.ToolRunResult{Text: firstNonEmpty(res.Error, res.Text), IsError: res.Error != ""}, nil
}

// toolListRPC serves tool.list: the caller's session's tools.
func (s *Server) toolListRPC(who caller.Caller) (any, *proto.Error) {
	rec, perr := s.callerSession(who)
	if perr != nil {
		return nil, perr
	}
	return proto.ToolListResult{Tools: s.sessionTools(rec)}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// toolRuns checks tool name's input and returns the commands it stands
// for; task gives the thread's task ref, for steps. An unknown name is
// a plain error, a bad input errModTool.
func toolRuns(name string, body []byte, task func() string) ([]proto.CLIRunParams, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	decode := func(v any) error {
		if err := dec.Decode(v); err != nil {
			return toolErr("%v", err)
		}
		return nil
	}
	args := func(a ...string) proto.CLIRunParams { return proto.CLIRunParams{Args: a} }
	switch name {
	case "report":
		var in ModReportTool
		if err := decode(&in); err != nil {
			return nil, err
		}
		text, err := reportText(in)
		if err != nil {
			return nil, err
		}
		p := args("report")
		for _, a := range in.Attach {
			if strings.TrimSpace(a) == "" {
				return nil, toolErr("attach: an empty path")
			}
			p.Args = append(p.Args, "--attach="+a)
		}
		p.Stdin = []byte(text)
		return []proto.CLIRunParams{p}, nil
	case "status":
		var in ModStatusTool
		if err := decode(&in); err != nil {
			return nil, err
		}
		p := args("status")
		if in.Percent != nil {
			if *in.Percent < 0 || *in.Percent > 100 {
				return nil, toolErr("percent takes 0-100")
			}
			p.Args = append(p.Args, "--percent="+strconv.Itoa(*in.Percent))
		}
		if in.Activity != "" {
			p.Args = append(p.Args, "--activity="+in.Activity)
		}
		if in.NeedsYou != "" {
			p.Args = append(p.Args, "--needs-you="+in.NeedsYou)
		}
		if len(p.Args) == 1 {
			return nil, toolErr("give percent, activity or needs_you")
		}
		return []proto.CLIRunParams{p}, nil
	case "steps":
		var in ModStepsTool
		if err := decode(&in); err != nil {
			return nil, err
		}
		if len(in.Check)+len(in.Uncheck)+len(in.Add) == 0 {
			return nil, toolErr("give check, uncheck or add")
		}
		ref := task()
		if ref == "" {
			return nil, toolErr("this thread has no task")
		}
		var runs []proto.CLIRunParams
		for _, verb := range []struct {
			name string
			ns   []int
		}{{"check", in.Check}, {"uncheck", in.Uncheck}} {
			for _, n := range verb.ns {
				if n < 1 {
					return nil, toolErr("%s: %d is not a step number", verb.name, n)
				}
				runs = append(runs, args("task", "steps", ref, verb.name, strconv.Itoa(n)))
			}
		}
		for _, t := range in.Add {
			if strings.TrimSpace(t) == "" || strings.ContainsAny(t, "\r\n") {
				return nil, toolErr("add: a step is one non-empty line")
			}
			runs = append(runs, args("task", "steps", ref, "add", "--", t))
		}
		return runs, nil
	case "done":
		var in ModDoneTool
		if err := decode(&in); err != nil {
			return nil, err
		}
		p := args("done")
		if in.Summary != "" {
			p.Args = append(p.Args, "--", in.Summary)
		}
		return []proto.CLIRunParams{p}, nil
	}
	return nil, fmt.Errorf("no tool %q", name)
}

// reportText writes the report tool's sections out as tm report takes
// them; thread.Validate checks the rest once it runs.
func reportText(in ModReportTool) (string, error) {
	if strings.TrimSpace(in.Report) == "" {
		return "", toolErr("report: empty")
	}
	if in.PR != "" {
		if _, _, ok := codehost.ParsePRURL(in.PR); !ok {
			return "", toolErr("pr: want https://github.com/<owner>/<repo>/pull/<n>, https://dev.azure.com/<org>/<project>/_git/<repo>/pullrequest/<n> or https://gitlab.com/<group>/<project>/-/merge_requests/<n>")
		}
	}
	for _, l := range strings.Split(in.Report, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "## ") {
			return "", toolErr("report: a line starts with ## (the sections are the tool's other fields; use ### inside the report)")
		}
	}
	var b strings.Builder
	if in.PR != "" {
		fmt.Fprintf(&b, "PR: %s\n\n", strings.TrimSpace(in.PR))
	}
	b.WriteString("## Report\n\n" + strings.TrimSpace(in.Report) + "\n")
	for _, sec := range []struct {
		name  string
		lines []string
		need  bool
	}{{"Next", in.Next, true}, {"Check", in.Check, false}, {"Remember", in.Remember, false}} {
		if len(sec.lines) == 0 && !sec.need {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", sec.name)
		for _, l := range sec.lines {
			l = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(l), "- "), "* "))
			if l == "" || strings.ContainsAny(l, "\r\n") || strings.HasPrefix(l, "## ") {
				return "", toolErr("%s: each item is one non-empty line", strings.ToLower(sec.name))
			}
			b.WriteString("- " + l + "\n")
		}
	}
	return b.String(), nil
}
