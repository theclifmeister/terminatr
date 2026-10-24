package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// safetySettings are slug's workflow settings, or with slug "" the
// all-projects ones: the same rows, written to the project's table or
// to [defaults]. What threads may do is the Access tab's
// (accessSettings).
func safetySettings(slug string) []setting {
	sr := newSafetyRows(slug)
	rows := sr.scope(append(sr.sharedRows(), sr.historyRow()))
	if sr.all {
		// The cap across every project is all projects' only: no
		// project sets its own (config.DefaultsOnly). Under Parallel
		// threads.
		at := slices.IndexFunc(rows, func(r setting) bool { return r.label == "Parallel threads" }) + 1
		return slices.Insert(rows, at, sr.globalRow())
	}
	return append(rows, sr.projectOnlyRows()...)
}

// safetyRows builds the rows of a project's settings (slug), or of all
// projects' (all).
type safetyRows struct {
	slug string
	all  bool
	// table is where they are written; forWho and ofWho name whose they
	// are in messages ("for demo", "of each project").
	table, forWho, ofWho string
}

func newSafetyRows(slug string) safetyRows {
	if slug == "" {
		return safetyRows{all: true, table: config.DefaultsTable, forWho: "for all projects", ofWho: "of each project"}
	}
	return safetyRows{slug: slug, table: "projects." + slug, forWho: "for " + slug, ofWho: "of " + slug}
}

// safety are the settings as the console has them now.
func (sr safetyRows) safety(m *dash) config.Safety {
	if sr.all {
		if m.data.Defaults != nil {
			return *m.data.Defaults
		}
	} else if p := m.projectData(sr.slug); p != nil && p.Safety != nil {
		return *p.Safety
	}
	return config.Defaults
}

// set saves a setting and shows it at once, before the next poll, so
// quick presses of + build on each other.
func (sr safetyRows) set(m *dash, key string, value any, msg string) tea.Cmd {
	s := sr.safety(m)
	s.Set(key, value)
	if sr.all {
		m.data.Defaults = &s
	} else if p := m.projectData(sr.slug); p != nil {
		p.Safety = &s
		if !slices.Contains(p.Own, key) {
			p.Own = append(slices.Clip(p.Own), key)
		}
	}
	return m.setSetting(sr.table, key, value, msg)
}

// on is the bool setting key's value now.
func (sr safetyRows) on(m *dash, key string) bool {
	on, _ := sr.safety(m).Value(key).(bool)
	return on
}

// onOffWord is a bool setting's value word, for scope.
func onOffWord(key string) func(config.Safety) string {
	return func(s config.Safety) string {
		on, _ := s.Value(key).(bool)
		return onOff(on)
	}
}

// toggleRow is an on/off setting: enter flips it, and the footer says
// what ("pull request follow-up on for demo").
func (sr safetyRows) toggleRow(label, help, key, what string) setting {
	return setting{label: label, help: help, keys: []string{key}, word: onOffWord(key),
		value: func(m *dash) string { return onOff(sr.on(m, key)) },
		change: func(m *dash) tea.Cmd {
			on := !sr.on(m, key)
			return sr.set(m, key, on, what+" "+onOff(on)+" "+sr.forWho)
		}}
}

// toggles are the plain on/off settings, by key; sharedRows places them.
var toggles = map[string]struct{ label, help, what string }{
	"coordinator_approves": {"Coordinator approves", "The coordinator may answer its threads' in-scope permission prompts (allow once, never always); one to run outside the sandbox is always yours.", "coordinator approvals"},
	"pr_followup":          {"Pull request follow-up", "Prompt a thread when its pull request's checks fail, a reviewer asks for changes, or main moves past it.", "pull request follow-up"},
	"coordinator_remote_control": {"Remote control", "Coordinators keep remote control on, so you can continue them from another device: a new one starts with it, and tm turns it back on when it drops. prefix+r changes the running one; your off holds until it is started anew.",
		"remote control for coordinators"},
	"auto_clear": {"Auto-clear coordinator", "Clear the coordinator's conversation once its context reaches the hint (context_hint), only while it is idle with nothing waiting: no inbox item, queued prompt, open question or blocked thread. The project lives in files, so it reads tm context again. Off by default; each clear is journaled.",
		"auto-clear for coordinators"},
	"coordinator_merges": {"Coordinator may merge", "Let the coordinator merge pull requests with tm thread merge without the agent's auto mode refusing it: tm adds a permission allow rule for that command to the coordinator only. It merges only a PR whose checks are green on its current head and that the host calls mergeable, by the merge method below, on GitHub and Azure DevOps. Threads never get it, and the guard still refuses their merges. Only has an effect while merge is coordinator (the default). Off by default.",
		"the coordinator merging"},
	"fast_forward_checkout": {"Keep my checkout current", "Fast-forward your own checkout of each repository when its default branch is checked out, clean and only behind origin; else the overview says how far behind.", "keeping your checkout current"},
}

// toggle is toggles[key]'s row.
func (sr safetyRows) toggle(key string) setting {
	t := toggles[key]
	return sr.toggleRow(t.label, t.help, key, t.what)
}

// scope fills in where each row's value comes from (rows with keys): on
// a project, its own value or all projects', which X goes back to; on
// all projects, the projects that set their own.
func (sr safetyRows) scope(rows []setting) []setting {
	slug := sr.slug
	for i := range rows {
		keys, word := rows[i].keys, rows[i].word
		if keys == nil {
			continue
		}
		if sr.all {
			rows[i].note = joinNotes(rows[i].note, func(m *dash) []string { return ownNote(m, keys) })
			continue
		}
		rows[i].from = func(m *dash) string {
			if p := m.projectData(slug); p != nil && !ownsAny(p.Own, keys) {
				return config.AllProjectsName
			}
			return ""
		}
		label := rows[i].label
		rows[i].unset = func(m *dash) tea.Cmd {
			p := m.projectData(slug)
			if p == nil || !ownsAny(p.Own, keys) {
				m.msg = "this setting of " + slug + " already follows all projects"
				return nil
			}
			msg := slug + " follows all projects in " + strings.ToLower(label)
			if m.data.Defaults != nil {
				msg += ": " + word(*m.data.Defaults)
			}
			src, table := m.src, sr.table
			return m.act(func() actionMsg {
				for _, k := range keys {
					if err := src.SetSetting(table, k, nil); err != nil {
						return actionMsg{err: settingsErr(err)}
					}
				}
				return actionMsg{msg: msg}
			})
		}
	}
	return rows
}

// sharedRows are the workflow settings a project and all projects both
// have, but Keep history (historyRow), in the popup's order.
func (sr safetyRows) sharedRows() []setting {
	rc := sr.toggle("coordinator_remote_control")
	rc.note = func(m *dash) []string {
		if sr.all {
			return nil
		}
		return remoteNote(sr.safety(m).CoordinatorRemoteControl, m.data.Sessions, sr.slug)
	}
	return []setting{
		sr.startRow(),
		sr.parallelRow(),
		sr.autoCloseRow(),
		sr.completeRow(),
		sr.toggle("pr_followup"),
		rc,
		sr.toggle("auto_clear"),
		sr.toggle("coordinator_merges"),
		sr.mergeMethodRow(),
		sr.toggle("fast_forward_checkout"),
		sr.agentRow("Thread agent", "The agent new threads run; the coordinator may still name another for one thread. Enter picks the next one tm knows.",
			"thread_agent", func(s config.Safety) string { return s.ThreadAgent }, "new threads "+sr.ofWho+" run "),
		sr.agentRow("Coordinator agent", "The agent a new coordinator runs; a running one keeps its agent until it is started anew. Enter picks the next one tm knows.",
			"coordinator_agent", func(s config.Safety) string { return s.CoordinatorAgent }, "new coordinators "+sr.ofWho+" run "),
		sr.modelsRow(),
		sr.coordModelRow(),
		sr.branchRow(),
		sr.linkLimitsRow(),
	}
}

// linkLimits are the link limits' settings (config.LinkKeys), as Link
// limits lists them: each with its default, its bound, the values enter
// steps through, and its value in words.
var linkLimits = []struct {
	key, label, help string
	def, most        int
	steps            []int
	words            func(int) string
	// least is the lowest value (1 when 0); free, a raise that asks
	// nothing first.
	least int
	free  bool
}{
	{"link_burst", "Messages at once", "How many messages the coordinator may send on a link at once before the rest wait their turn (each direction its own). Messages over the rate are queued and sent in order, never dropped. Raising it above the default asks first.",
		config.DefaultLinkBurst, config.MaxLinkBurst, []int{1, 2, 3, 5, 10, 20}, func(n int) string { return fmt.Sprintf("%d at once", n) }, 0, false},
	{"link_per_hour", "Messages an hour", "How many messages a link carries an hour each way after the burst; queued ones go as it allows. Raising it above the default asks first.",
		config.DefaultLinkPerHour, config.MaxLinkPerHour, []int{5, 10, 20, 30, 60, 120}, func(n int) string { return fmt.Sprintf("%d an hour", n) }, 0, false},
	{"link_max_kb", "Message size", "The most text one message may have; a longer one is refused, to be split into parts (which go in order) or, on this server, to attach a library file. Raising it above the default asks first.",
		config.DefaultLinkMaxKB, config.MaxLinkKB, []int{4, 8, 16, 32, 64}, func(n int) string { return fmt.Sprintf("%d KB", n) }, 0, false},
	{"link_file_mb", "File size", "The most a file sent with a message may have (tm link send --file), this side's limit whichever way it goes; a bigger one is refused. Each file still waits for the receiving user's accept unless their link takes files on its own. Raising it above the default asks first.",
		config.DefaultLinkFileMB, config.MaxLinkFileMB, []int{5, 10, 25, 50, 100, 200}, func(n int) string { return fmt.Sprintf("%d MB", n) }, 0, false},
	{"link_file_kbps", "File pace", "How fast this side sends a file to a paired server, in KiB a second; it slows on its own when the link's round trips rise, so panes stay quick.",
		config.DefaultLinkFileKbps, config.MaxLinkFileKbps, []int{64, 256, 512, 1024, 2048}, func(n int) string { return fmt.Sprintf("%d KiB/s", n) }, config.MinLinkFileKbps, true},
}

// linkLimitsWords is Link limits' value: "3 at once · 10 an hour · 8 KB ·
// files up to 25 MB".
func linkLimitsWords(s config.Safety) string {
	var out []string
	for _, l := range linkLimits {
		switch l.key {
		case "link_file_kbps": // the list shows it; the row stays short
		case "link_file_mb":
			out = append(out, "files up to "+l.words(s.Value(l.key).(int)))
		default:
			out = append(out, l.words(s.Value(l.key).(int)))
		}
	}
	return strings.Join(out, " · ")
}

// linkLimitRows are the link limits Link limits opens, each its own
// setting with its own scope. One raised above its default lets
// coordinators send more without the user: it asks first.
func (sr safetyRows) linkLimitRows() []setting {
	var rows []setting
	for _, l := range linkLimits {
		get := func(m *dash) int { n, _ := sr.safety(m).Value(l.key).(int); return n }
		put := func(m *dash, n int) tea.Cmd {
			n = min(max(n, l.least, 1), l.most)
			cur := get(m)
			if n == cur {
				return nil
			}
			msg := "links " + sr.ofWho + " take " + l.words(n)
			if n <= cur || n <= l.def || l.free {
				return sr.set(m, l.key, n, msg)
			}
			who := sr.slug + "'s coordinator"
			if sr.all {
				who = "the coordinators of " + followers(m, []string{l.key})
			}
			q := fmt.Sprintf("Raise %s %s to %s (the default is %s)? %s then reaches linked projects with more, without you.", strings.ToLower(l.label), sr.forWho, l.words(n), l.words(l.def), who)
			m.confirm("Raise "+strings.ToLower(l.label), q, func() tea.Cmd {
				return sr.set(m, l.key, n, msg)
			})
			return nil
		}
		rows = append(rows, setting{label: l.label, help: l.help,
			keys: []string{l.key}, word: func(s config.Safety) string { return l.words(s.Value(l.key).(int)) },
			value:  func(m *dash) string { return l.words(get(m)) },
			change: func(m *dash) tea.Cmd { return put(m, nextStep(l.steps, get(m))) },
			adjust: func(m *dash, d int) tea.Cmd { return put(m, get(m)+d) }})
	}
	return sr.scope(rows)
}

// linkLimitsRow is Link limits, which lists linkLimitRows.
func (sr safetyRows) linkLimitsRow() setting {
	rows := sr.linkLimitRows()
	return setting{label: "Link limits", help: "How much the coordinator may send on links to other projects (tm link): messages at once and an hour, each way, and a message's size. Messages over the rate wait queued and go in order. Between two projects here the lower of their limits holds; a paired server keeps its own. Enter lists them.",
		keys: config.LinkKeys, word: linkLimitsWords,
		value: func(m *dash) string { return linkLimitsWords(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			m.push(&historyView{title: "link limits " + sr.forWho, list: settingsList{rows: rows}, all: sr.all, unit: "one"})
			return nil
		}}
}

// branchPreview is the branch a thread would get from template: a
// thread t-0042 for task T42, "Fix login", and the same without a task.
func (sr safetyRows) branchPreview(template string) string {
	slug := cmp.Or(sr.slug, "demo")
	with := config.ExpandBranchName(template, slug, "t-0042", "T42", "fix-login")
	if !strings.Contains(template, "{task}") {
		return with
	}
	return with + " (" + config.ExpandBranchName(template, slug, "t-0042", "", "fix-login") + " without a task)"
}

func (sr safetyRows) branchRow() setting {
	return setting{label: "Branch names", help: "How new threads name their branch, for a company's branch policy. Use {project}, {id} (required, as t-0042), {task} (as T42, or empty) and {title}; for example feature/{task}-{title} or users/me/{id}. Existing threads keep their branches. Enter edits it; an empty one is the default.",
		keys: []string{"branch_name"}, word: func(s config.Safety) string { return s.BranchName },
		value: func(m *dash) string { return sr.safety(m).BranchName },
		note: func(m *dash) []string {
			return []string{"next branch, e.g. " + sr.branchPreview(sr.safety(m).BranchName)}
		},
		change: func(m *dash) tea.Cmd {
			// The default shows faint, never as the text: an empty one is it.
			text := sr.safety(m).BranchName
			if text == config.DefaultBranchName {
				text = ""
			}
			m.push(&inputView{title: "Branch names " + sr.forWho, keep: true, lineEdit: newLine(text),
				label: "Template ({project} {id} {task} {title}; {id} is required; empty is the default):", placeholder: config.DefaultBranchName,
				submit: func(t string) tea.Cmd {
					t = cmp.Or(strings.TrimSpace(t), config.DefaultBranchName)
					if err := config.CheckBranchName(t); err != nil {
						m.msg = "branch names " + err.Error() + ": it stays as it is"
						return nil
					}
					return sr.set(m, "branch_name", t, "new threads "+sr.ofWho+" get branches like "+sr.branchPreview(t))
				}})
			return nil
		}}
}

func startWords(s config.Safety) string {
	if s.StartThreads == config.StartAuto {
		return "automatically"
	}
	return "ask first"
}

func (sr safetyRows) startRow() setting {
	return setting{label: "Start threads", help: "Ask first: the coordinator proposes threads and starts them after your go-ahead. Automatically: it starts them itself.",
		keys: []string{"start_threads"}, word: startWords,
		value: func(m *dash) string { return startWords(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			next, word := config.StartAuto, "automatically"
			if sr.safety(m).StartThreads == config.StartAuto {
				next, word = config.StartPropose, "after asking you"
			}
			return sr.set(m, "start_threads", next, "threads "+sr.ofWho+" start "+word)
		}}
}

func (sr safetyRows) yoloRow() setting {
	return setting{label: "Yolo mode", help: "New threads run with no sandbox and no permission prompts; the guard keeps only its floor. Turning it on asks first.",
		keys: []string{"yolo"}, word: onOffWord("yolo"),
		value: func(m *dash) string { return onOff(sr.safety(m).Yolo) },
		change: func(m *dash) tea.Cmd {
			if sr.safety(m).Yolo {
				return sr.set(m, "yolo", false, "yolo mode off "+sr.forWho)
			}
			q := "Turn yolo mode on for " + sr.slug + "? Threads started from now on run with no sandbox and no permission prompts; the guard keeps only its floor."
			if sr.all {
				q = "Turn yolo mode on for all projects? Threads started from now on run with no sandbox and no permission prompts in " + followers(m, []string{"yolo"}) + "; the guard keeps only its floor."
			}
			m.confirm("Turn on yolo mode", q, func() tea.Cmd {
				return sr.set(m, "yolo", true, "yolo mode on "+sr.forWho+": new threads have no sandbox and no prompts")
			})
			return nil
		}}
}

// threads are a project's working threads, after its cap.
func (sr safetyRows) threads(m *dash) string {
	if sr.all {
		return ""
	}
	out := fmt.Sprintf(" · %d working now", m.workingThreads(sr.slug))
	if g := sr.safety(m).GlobalParallelThreads; g > 0 {
		out += fmt.Sprintf(" · %d of %d across projects", m.workingAll(), g)
	}
	return out
}

func (sr safetyRows) parallelRow() setting {
	put := func(m *dash, n int) tea.Cmd {
		return sr.set(m, "parallel_threads", n, fmt.Sprintf("up to %d threads %s work at once", n, sr.ofWho))
	}
	return setting{label: "Parallel threads", help: "The most threads working at once; beyond it the coordinator proposes and waits. Idle and done threads don't count. Enter or + and - change it.",
		keys: []string{"parallel_threads"}, word: func(s config.Safety) string { return fmt.Sprint(s.ParallelThreads) },
		value: func(m *dash) string {
			return fmt.Sprintf("%d", sr.safety(m).ParallelThreads) + sr.threads(m)
		},
		change: func(m *dash) tea.Cmd { return put(m, nextStep(capSteps, sr.safety(m).ParallelThreads)) },
		adjust: func(m *dash, d int) tea.Cmd {
			return put(m, min(max(sr.safety(m).ParallelThreads+d, 1), config.MaxParallelThreads))
		}}
}

func (sr safetyRows) autoCloseRow() setting {
	return setting{label: "Auto-close finished threads", help: "Close a thread when its pull request merges, or some days after it finishes (done, or its pull request merged); + and - change the days. One with uncommitted or unpushed work stays open, and the coordinator is told.",
		keys: []string{"auto_close", "auto_close_days"}, word: closeWords,
		value: func(m *dash) string { return closeWords(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			next := map[string]string{config.CloseOff: config.CloseMerged, config.CloseMerged: config.CloseDays}[sr.safety(m).AutoClose]
			if next == "" {
				next = config.CloseOff
			}
			s := sr.safety(m)
			s.AutoClose = next
			return sr.set(m, "auto_close", next, "auto-close "+sr.forWho+": "+closeWords(s))
		},
		adjust: func(m *dash, d int) tea.Cmd {
			s := sr.safety(m)
			n := min(max(s.AutoCloseDays+d, 1), config.MaxAutoCloseDays)
			msg := fmt.Sprintf("threads %s close %s after they finish", sr.ofWho, days(n))
			if s.AutoClose != config.CloseDays {
				msg = fmt.Sprintf("%s once auto-close is set to days after it finishes (enter)", days(n))
			}
			return sr.set(m, "auto_close_days", n, msg)
		}}
}

func (sr safetyRows) completeRow() setting {
	return setting{label: "Complete tasks", help: "By you, or on your standing acceptance: a task in review is done once its pull request merges. X sends it back.",
		keys: []string{"complete_tasks"}, word: func(s config.Safety) string { return completeWords(s.CompleteTasks) },
		value: func(m *dash) string { return completeWords(sr.safety(m).CompleteTasks) },
		change: func(m *dash) tea.Cmd {
			next := config.CompleteMerged
			if sr.safety(m).CompleteTasks == config.CompleteMerged {
				next = config.CompleteUser
			}
			msg := "tasks " + sr.ofWho + " are done " + completeWords(next)
			if next == config.CompleteUser {
				msg = "tasks " + sr.ofWho + " are done when you accept them"
			}
			return sr.set(m, "complete_tasks", next, msg)
		}}
}

// mergeMethodWords name merge_method's values.
var mergeMethodWords = map[string]string{config.MethodMerge: "merge commit", config.MethodSquash: "squash", config.MethodRebase: "rebase"}

func (sr safetyRows) mergeMethodRow() setting {
	word := func(s config.Safety) string { return cmp.Or(mergeMethodWords[s.MergeMethod], s.MergeMethod) }
	return setting{label: "Merge method", help: "How tm thread merge merges a pull request: a merge commit, a squash (one commit, the pull request's title as its subject) or a rebase (its commits onto the base, no merge commit). A repository that requires linear history refuses merge commits. Enter picks the next one.",
		keys: []string{"merge_method"}, word: word,
		value: func(m *dash) string { return word(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			ms := config.MergeMethods
			next := ms[(slices.Index(ms, sr.safety(m).MergeMethod)+1)%len(ms)]
			return sr.set(m, "merge_method", next, "pull requests "+sr.ofWho+" merge by "+mergeMethodWords[next])
		}}
}

// agentRow is an agent setting: enter picks the next agent tm knows
// (tm agent list), so the file only ever names a known one.
func (sr safetyRows) agentRow(label, help, key string, get func(config.Safety) string, msg string) setting {
	return setting{label: label, help: help,
		keys: []string{key}, word: func(s config.Safety) string { return cmp.Or(get(s), "auto") },
		value: func(m *dash) string { return agentWords(get(sr.safety(m)), m.agentList()) },
		change: func(m *dash) tea.Cmd {
			names := m.src.Agents() // the installed ones
			cur := get(sr.safety(m))
			switch {
			case len(names) == 0:
				m.msg = "no agent is installed: install one tm knows (tm agent list), then tm doctor"
				return nil
			case len(names) == 1 && names[0] == cur:
				m.msg = "only " + cur + " is installed; install another to choose between them (tm agent list)"
				return nil
			}
			next := names[(slices.Index(names, cur)+1)%len(names)]
			return sr.set(m, key, next, msg+next)
		},
		note: func(m *dash) []string {
			if cur, names := get(sr.safety(m)), m.agentList(); cur != "" && !slices.Contains(names, cur) {
				return []string{th().Warn.Render(cur + " isn't installed: nothing starts with it; enter picks an installed agent")}
			}
			return nil
		}}
}

func (sr safetyRows) modelsRow() setting {
	return setting{label: "Thread models", help: "The models the coordinator may pick for a thread, from the ones the installed agents offer (General: Models). Enter lists them to allow or leave out; with none left out it chooses from all, new ones too. A model it may not pick is refused.",
		keys: []string{"models"}, word: func(s config.Safety) string { return modelWords(s.Models) },
		value: func(m *dash) string { return modelWords(sr.safety(m).Models) },
		change: func(m *dash) tea.Cmd {
			names := catalogNames(m.src.Catalogs())
			m.push(&modelsView{all: names, allow: slices.Clone(sr.safety(m).Models),
				save: func(m *dash, list []string) tea.Cmd {
					// Every model allowed: all projects drops the line,
					// a project keeps the explicit list (X follows all).
					if sr.all && len(list) == len(names) && !slices.ContainsFunc(list, func(n string) bool { return !slices.Contains(names, n) }) {
						s := sr.safety(m)
						s.Models = nil
						m.data.Defaults = &s
						return m.setSetting(sr.table, "models", nil, "the coordinator may pick any model "+sr.forWho)
					}
					return sr.set(m, "models", list, "the coordinator may pick "+strings.Join(list, ", ")+" "+sr.forWho)
				}})
			return nil
		}}
}

// coordModels are the models the project's coordinator may run: its
// agent's, in the project's scope.
func (sr safetyRows) coordModels(m *dash) (string, []string, string) {
	s := sr.safety(m)
	name, why := resolvedAgent(s.CoordinatorAgent, m.agentList())
	if name == "" {
		return "", nil, why
	}
	for _, c := range m.catalogList() {
		if c.Agent != name {
			continue
		}
		if !c.Known {
			return name, nil, name + "'s models are unknown (" + c.Reason + ")"
		}
		var out []string
		for _, x := range c.InScope(s.Models) {
			out = append(out, x.Name)
		}
		return name, out, ""
	}
	return name, nil, name + " isn't installed"
}

func (sr safetyRows) coordModelRow() setting {
	slug := sr.slug
	return setting{label: "Coordinator model", help: "The model a new coordinator runs, from its agent's models in Thread models; a running one keeps its model until it is started anew. Enter picks the next one; the agent's default is the first.",
		keys: []string{"coordinator_model"}, word: func(s config.Safety) string { return cmp.Or(s.CoordinatorModel, "agent's default") },
		value: func(m *dash) string {
			if cur := sr.safety(m).CoordinatorModel; cur != "" {
				return cur
			}
			return "agent's default"
		},
		change: func(m *dash) tea.Cmd {
			name, list, why := sr.coordModels(m)
			if len(list) == 0 {
				m.msg = cmp.Or(why, "the coordinator's agent offers no models here") + ": the coordinator runs its agent's default"
				return nil
			}
			choices := append([]string{""}, list...)
			next := choices[(slices.Index(choices, sr.safety(m).CoordinatorModel)+1)%len(choices)]
			msg := "new coordinators " + sr.ofWho + " run " + next
			if next == "" {
				msg = "new coordinators " + sr.ofWho + " run " + name + "'s default"
			}
			save := sr.set(m, "coordinator_model", next, msg)
			// The running coordinators it reaches switch now, live
			// where their agent can (session.model); the ticker
			// catches any this misses.
			var ids []string
			for _, s := range m.data.Sessions {
				if s.Role == proto.RoleCoordinator && s.Agent != "" && (s.Project == slug || sr.all && !ownsModel(m, s.Project)) {
					ids = append(ids, s.ID)
				}
			}
			src := m.src
			return tea.Sequence(save, func() tea.Msg {
				for _, id := range ids {
					src.SyncModel(id)
				}
				return nil
			})
		},
		note: func(m *dash) []string {
			if !sr.all {
				for _, s := range m.data.Sessions {
					if s.Role == proto.RoleCoordinator && s.Project == slug && s.ModelPending {
						return []string{th().Warn.Render("the running coordinator runs " + modelWord(s.Model) + "; " + modelWord(s.ModelNext) + " on its next start (" + s.Agent + " can't switch models live)")}
					}
				}
			}
			cur := sr.safety(m).CoordinatorModel
			if cur == "" {
				return nil
			}
			name, list, why := sr.coordModels(m)
			switch {
			case why != "":
				return []string{th().Warn.Render(why + ": the coordinator runs its agent's default")}
			case !slices.Contains(list, cur):
				return []string{th().Warn.Render(name + " doesn't offer " + cur + " in this project's models now: the coordinator runs " + name + "'s default")}
			}
			return nil
		}}
}

// historyRows are the retention ages Keep history opens, each its own
// setting with its own scope.
func (sr safetyRows) historyRows() []setting {
	var ages []setting
	for _, a := range archiveAges {
		put := func(m *dash, n int) tea.Cmd {
			return sr.set(m, a.key, n, fmt.Sprintf("%s %s after %s", a.verb, sr.ofWho, days(n)))
		}
		ages = append(ages, setting{label: a.label, help: a.help,
			keys: []string{a.key}, word: func(s config.Safety) string { return days(s.ArchiveDays(a.key)) },
			value:  func(m *dash) string { return days(sr.safety(m).ArchiveDays(a.key)) },
			change: func(m *dash) tea.Cmd { return put(m, nextStep(archiveSteps, sr.safety(m).ArchiveDays(a.key))) },
			adjust: func(m *dash, d int) tea.Cmd {
				return put(m, min(max(sr.safety(m).ArchiveDays(a.key)+d, 1), config.MaxArchiveDays))
			}})
	}
	return sr.scope(ages)
}

// historyRow is Keep history, which lists historyRows.
func (sr safetyRows) historyRow() setting {
	ages := sr.historyRows()
	return setting{label: "Keep history", help: "How long done tasks, resolved threads, handled inbox items and journal lines stay before they move to compressed archives beside them; nothing is deleted, and a thread with unsaved work stays. Enter lists them.",
		keys: config.ArchiveKeys, word: historyWords,
		value: func(m *dash) string { return historyWords(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			m.push(&historyView{title: "keep history " + sr.forWho, list: settingsList{rows: ages}, all: sr.all})
			return nil
		}}
}

// globalRow is all projects' cap across every project.
func (sr safetyRows) globalRow() setting {
	global := func(m *dash, n int) tea.Cmd {
		msg := fmt.Sprintf("up to %d threads work at once across all projects", n)
		if n == 0 {
			msg = "no cap on threads across all projects"
		}
		return sr.set(m, "global_parallel_threads", n, msg)
	}
	return setting{label: "Threads across projects", help: "The most threads working at once in all projects together, beside each project's own cap; beyond it coordinators propose and wait. Off is no such cap. Idle and done threads don't count. Enter or + and - change it.",
		value: func(m *dash) string {
			return globalCapWords(sr.safety(m).GlobalParallelThreads) + fmt.Sprintf(" · %d working now", m.workingAll())
		},
		change: func(m *dash) tea.Cmd { return global(m, nextStep(globalCapSteps, sr.safety(m).GlobalParallelThreads)) },
		adjust: func(m *dash, d int) tea.Cmd {
			return global(m, min(max(sr.safety(m).GlobalParallelThreads+d, 0), config.MaxGlobalThreads))
		}}
}

// projectOnlyRows are the project's own state, never all projects':
// activate, pause, archive, delete.
func (sr safetyRows) projectOnlyRows() []setting {
	slug := sr.slug
	return []setting{
		{label: "Active", help: "Only an active project's coordinator and threads run, and only they come back when the server restarts. Turning it off stops them now, to resume when you turn it on again; asks first while they run. Space in the sidebar does the same.",
			value:  func(m *dash) string { return onOff(sr.safety(m).Active) },
			change: func(m *dash) tea.Cmd { return m.toggleActive(slug, !sr.safety(m).Active) }},
		{label: "Paused", help: "While paused the coordinator gets no nudges, threads get no pull request follow-up, and no new thread starts; the dashboard still follows their state.",
			value: func(m *dash) string { return onOff(sr.safety(m).Paused) },
			change: func(m *dash) tea.Cmd {
				verb := "pause"
				if sr.safety(m).Paused {
					verb = "resume"
				}
				if p := m.projectData(slug); p != nil {
					s := sr.safety(m)
					s.Paused = verb == "pause"
					p.Safety = &s
				}
				return m.lifecycle(slug, verb)
			}},
		{label: "Archive", help: "Hide the project from the sidebar, and stop all background work for it; tm project unarchive brings it back. Not while its coordinator or threads run. Asks first.",
			value: func(m *dash) string { return "enter archives" },
			change: func(m *dash) tea.Cmd {
				m.confirmNo("Archive "+slug, "Archive "+slug+"? It leaves the sidebar, and nothing runs for it until tm project unarchive "+slug+".", "not archived", func() tea.Cmd {
					m.pop() // the project popup
					return m.lifecycle(slug, "archive")
				})
				return nil
			}},
		{label: "Delete", help: "Move the project's folder to the trash; its worktrees and branches stay. Not while its coordinator or threads run. Asks first.",
			value: func(m *dash) string { return "enter deletes" },
			change: func(m *dash) tea.Cmd {
				m.confirmNo("Delete "+slug, "Delete "+slug+"? Its folder (tasks, memory, threads' reports) moves to the trash; its worktrees and branches stay.", "not deleted", func() tea.Cmd {
					m.pop() // the project popup
					return m.lifecycle(slug, "delete")
				})
				return nil
			}},
	}
}
