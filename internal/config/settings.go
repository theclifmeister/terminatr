package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// The safety settings' one table: every key a [projects.<slug>] or
// [defaults] table may set, in ProjectKeys order, with where it may be
// set, how the file's value is read into Safety and the one check that
// both Load (apply) and the writers (CheckSetting) run. ProjectKeys,
// ProjectOnly, DefaultsOnly, own, Value and CheckSetting all come from
// it, so a new setting is one row here, beside its fields in rawSafety
// and Safety.

// reach is which tables may set a setting.
type reach int

const (
	// everywhere: a project's table and [defaults].
	everywhere reach = iota
	// projectOnly: a project's own state, never all projects' (an
	// inactive, paused, archived, pinned or shared [defaults] would stop,
	// hide, pin or share every project). Own doesn't list them: they
	// never follow all projects.
	projectOnly
	// defaultsOnly: all projects' setting, never a project's (the global
	// cap counts the threads of every project).
	defaultsOnly
	// byHand: only the human sets it, editing the file; tm's writers
	// don't know the key (it isn't in ProjectKeys) and Own doesn't list
	// it.
	byHand
)

// settingRow is one row of settingRows.
type settingRow struct {
	key   string
	reach reach
	// isSet reports whether a table sets the key (Own).
	isSet func(r *rawSafety) bool
	// apply checks the table's value, if it sets one, and puts it in s;
	// the error says what the value must be, without the key.
	apply func(r *rawSafety, s *Safety) error
	// check checks a value about to be written: its type, then the same
	// check apply runs.
	check func(v any) error
	// value is the setting's value in s; nil for a key Safety has no
	// field for (auto_resolve).
	value func(s *Safety) any
	// put sets the setting in s to v, when v has its type.
	put func(s *Safety, v any)
}

// limit is a check's error that says what the value must be; apply adds
// what the file has instead.
type limit string

func (l limit) Error() string { return string(l) }

// oneOf checks a string is one of vals.
func oneOf(vals ...string) func(string) error {
	return func(v string) error {
		if slices.Contains(vals, v) {
			return nil
		}
		q := make([]string, len(vals))
		for i, v := range vals {
			q[i] = fmt.Sprintf("%q", v)
		}
		return limit("must be " + strings.Join(q[:len(q)-1], ", ") + " or " + q[len(q)-1])
	}
}

// between checks a number is lo to hi; lo 0 is no cap.
func between(lo, hi int) func(int) error {
	return func(n int) error {
		if n >= lo && n <= hi {
			return nil
		}
		if lo == 0 {
			return limit(fmt.Sprintf("must be 0 (no cap) to %d", hi))
		}
		return limit(fmt.Sprintf("must be %d to %d", lo, hi))
	}
}

// def is a setting whose file value raw reads and field holds in
// Safety; check (nil for none) limits its values, and words name it in
// CheckSetting's errors ("parallel threads").
func def[T any](key, words string, at reach, raw func(*rawSafety) *T, field func(*Safety) *T, check func(T) error) settingRow {
	if check == nil {
		check = func(T) error { return nil }
	}
	return settingRow{key: key, reach: at,
		isSet: func(r *rawSafety) bool { return raw(r) != nil },
		apply: func(r *rawSafety, s *Safety) error {
			v := raw(r)
			if v == nil {
				return nil
			}
			if err := check(*v); err != nil {
				if _, ok := err.(limit); ok {
					return fmt.Errorf("%w, not %#v", err, *v)
				}
				return err
			}
			val := *v
			switch l := any(val).(type) {
			case []string:
				val = any(slices.Clone(l)).(T)
			case []GuardAllow:
				val = any(slices.Clone(l)).(T)
			}
			*field(s) = val
			return nil
		},
		check: func(v any) error {
			t, ok := v.(T)
			if !ok {
				return fmt.Errorf("%s must be %s", key, typeWords[reflect.TypeFor[T]().Kind()])
			}
			if err := check(t); err != nil {
				return fmt.Errorf("%s %w", words, err)
			}
			return nil
		},
		value: func(s *Safety) any { return *field(s) },
		put: func(s *Safety, v any) {
			if t, ok := v.(T); ok {
				*field(s) = t
			}
		},
	}
}

// typeWords name a setting's type in errors.
var typeWords = map[reflect.Kind]string{reflect.Bool: "true or false", reflect.Int: "a number", reflect.String: "a string", reflect.Slice: "a list of names"}

// settingRows are the safety settings, in ProjectKeys order; the by-hand
// ones last.
var settingRows = []settingRow{
	def("start_threads", "start threads", everywhere, func(r *rawSafety) *string { return r.StartThreads }, func(s *Safety) *string { return &s.StartThreads }, oneOf(StartPropose, StartAuto)),
	def("yolo", "yolo", everywhere, func(r *rawSafety) *bool { return r.Yolo }, func(s *Safety) *bool { return &s.Yolo }, nil),
	def("coordinator_approves", "coordinator approves", everywhere, func(r *rawSafety) *bool { return r.CoordinatorApproves }, func(s *Safety) *bool { return &s.CoordinatorApproves }, nil),
	def("parallel_threads", "parallel threads", everywhere, func(r *rawSafety) *int { return r.ParallelThreads }, func(s *Safety) *int { return &s.ParallelThreads }, between(1, MaxParallelThreads)),
	def("global_parallel_threads", "global parallel threads", defaultsOnly, func(r *rawSafety) *int { return r.GlobalThreads }, func(s *Safety) *int { return &s.GlobalParallelThreads }, between(0, MaxGlobalThreads)),
	autoClose(def("auto_close", "auto-close", everywhere, func(r *rawSafety) *string { return r.AutoClose }, func(s *Safety) *string { return &s.AutoClose }, oneOf(CloseOff, CloseMerged, CloseDays))),
	def("auto_close_days", "auto-close days", everywhere, func(r *rawSafety) *int { return r.AutoCloseDays }, func(s *Safety) *int { return &s.AutoCloseDays }, between(1, MaxAutoCloseDays)),
	autoResolve,
	def("pr_followup", "PR follow-up", everywhere, func(r *rawSafety) *bool { return r.PRFollowup }, func(s *Safety) *bool { return &s.PRFollowup }, nil),
	def("pr_poll_seconds", "PR poll seconds", everywhere, func(r *rawSafety) *int { return r.PRPoll }, func(s *Safety) *int { return &s.PRPollSeconds }, between(MinPRPollSeconds, MaxPRPollSeconds)),
	// The removed "released" is read as CompleteUser (Removed lists
	// where), never written.
	def("complete_tasks", "complete tasks", everywhere, func(r *rawSafety) *string {
		if r.CompleteTasks != nil && *r.CompleteTasks == CompleteRemoved {
			user := CompleteUser
			return &user
		}
		return r.CompleteTasks
	}, func(s *Safety) *string { return &s.CompleteTasks }, oneOf(CompleteUser, CompleteMerged)),
	def("coordinator_remote_control", "coordinator remote control", everywhere, func(r *rawSafety) *bool { return r.CoordinatorRC }, func(s *Safety) *bool { return &s.CoordinatorRemoteControl }, nil),
	def("auto_clear", "auto-clear", everywhere, func(r *rawSafety) *bool { return r.AutoClear }, func(s *Safety) *bool { return &s.AutoClear }, nil),
	def("coordinator_merges", "coordinator merges", everywhere, func(r *rawSafety) *bool { return r.CoordMerges }, func(s *Safety) *bool { return &s.CoordinatorMerges }, nil),
	def("merge_method", "merge method", everywhere, func(r *rawSafety) *string { return r.MergeMethod }, func(s *Safety) *string { return &s.MergeMethod }, oneOf(MergeMethods...)),
	def("fast_forward_checkout", "fast-forward checkout", everywhere, func(r *rawSafety) *bool { return r.FastForward }, func(s *Safety) *bool { return &s.FastForwardCheckout }, nil),
	protection(def("protection", "protection", everywhere, func(r *rawSafety) *string {
		if r.Protection == nil && r.SandboxEsc != nil && !*r.SandboxEsc {
			strict := ProtectStrict
			return &strict
		}
		return r.Protection
	}, func(s *Safety) *string { return &s.Protection }, oneOf(ProtectLevels...))),
	def("thread_agent", "the agent", everywhere, func(r *rawSafety) *string { return r.ThreadAgent }, func(s *Safety) *string { return &s.ThreadAgent }, CheckAgent),
	def("coordinator_agent", "the agent", everywhere, func(r *rawSafety) *string { return r.CoordAgent }, func(s *Safety) *string { return &s.CoordinatorAgent }, CheckAgent),
	def("models", "models", everywhere, func(r *rawSafety) *[]string { return r.Models }, func(s *Safety) *[]string { return &s.Models }, CheckModels),
	def("coordinator_model", "the coordinator model", everywhere, func(r *rawSafety) *string { return r.CoordModel }, func(s *Safety) *string { return &s.CoordinatorModel }, checkCoordModel),
	def("active", "active", projectOnly, func(r *rawSafety) *bool { return r.Active }, func(s *Safety) *bool { return &s.Active }, nil),
	def("paused", "paused", projectOnly, func(r *rawSafety) *bool { return r.Paused }, func(s *Safety) *bool { return &s.Paused }, nil),
	def("archived", "archived", projectOnly, func(r *rawSafety) *bool { return r.Archived }, func(s *Safety) *bool { return &s.Archived }, nil),
	def("pinned", "pinned", projectOnly, func(r *rawSafety) *bool { return r.Pinned }, func(s *Safety) *bool { return &s.Pinned }, nil),
	def("share", "share", projectOnly, func(r *rawSafety) *bool { return r.Share }, func(s *Safety) *bool { return &s.Share }, nil),
	def("archive_tasks_days", "archive days", everywhere, func(r *rawSafety) *int { return r.ArchiveTasks }, func(s *Safety) *int { return &s.ArchiveTasksDays }, between(1, MaxArchiveDays)),
	def("archive_threads_days", "archive days", everywhere, func(r *rawSafety) *int { return r.ArchiveThreads }, func(s *Safety) *int { return &s.ArchiveThreadsDays }, between(1, MaxArchiveDays)),
	def("archive_inbox_days", "archive days", everywhere, func(r *rawSafety) *int { return r.ArchiveInbox }, func(s *Safety) *int { return &s.ArchiveInboxDays }, between(1, MaxArchiveDays)),
	def("archive_journal_days", "archive days", everywhere, func(r *rawSafety) *int { return r.ArchiveJournal }, func(s *Safety) *int { return &s.ArchiveJournalDays }, between(1, MaxArchiveDays)),
	def("branch_name", "branch names", everywhere, func(r *rawSafety) *string { return r.BranchName }, func(s *Safety) *string { return &s.BranchName }, CheckBranchName),
	def("guard", "guard", everywhere, func(r *rawSafety) *bool { return r.Guard }, func(s *Safety) *bool { return &s.Guard }, nil),
	def("guard_off", "guard off", everywhere, func(r *rawSafety) *[]string { return r.GuardOff }, func(s *Safety) *[]string { return &s.GuardOff }, checkGuardOff),
	def("guard_mode", "guard mode", everywhere, func(r *rawSafety) *string { return r.GuardMode }, func(s *Safety) *string { return &s.GuardMode }, oneOf(GuardModes...)),
	def("guard_allow", "guard allowances", everywhere, func(r *rawSafety) *[]GuardAllow { return r.GuardAllow }, func(s *Safety) *[]GuardAllow { return &s.GuardAllow }, checkGuardAllows),
	def("merge", "merge", byHand, func(r *rawSafety) *string { return r.Merge }, func(s *Safety) *string { return &s.Merge }, oneOf(MergeCoordinator, MergeThread)),
}

// autoClose is auto_close's row: a table sets it with the older
// auto_resolve too.
func autoClose(s settingRow) settingRow {
	s.isSet = func(r *rawSafety) bool { return r.AutoClose != nil || r.AutoResolve != nil }
	return s
}

// protection is protection's row: a table sets it with the older
// sandbox_escapes = false too.
func protection(s settingRow) settingRow {
	s.isSet = func(r *rawSafety) bool { return r.Protection != nil || r.SandboxEsc != nil && !*r.SandboxEsc }
	return s
}

// autoResolve is auto_close's older form: true is "merged", false
// "off"; auto_close wins when both are set. Own lists it as auto_close
// and Safety has no field for it.
var autoResolve = settingRow{key: "auto_resolve", reach: everywhere,
	isSet: func(*rawSafety) bool { return false },
	apply: func(r *rawSafety, s *Safety) error {
		if r.AutoResolve != nil && r.AutoClose == nil {
			s.AutoClose = map[bool]string{true: CloseMerged, false: CloseOff}[*r.AutoResolve]
		}
		return nil
	},
	check: func(v any) error {
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("auto_resolve must be %s", typeWords[reflect.Bool])
		}
		return nil
	},
	value: func(*Safety) any { return nil },
	put:   func(*Safety, any) {},
}

// checkCoordModel: "" is the agent's default model.
func checkCoordModel(name string) error {
	if name == "" {
		return nil
	}
	if err := CheckModelName(name); err != nil {
		return fmt.Errorf("is refused: %w", err)
	}
	return nil
}

// checkGuardOff: each a rule of GuardRules.
func checkGuardOff(ids []string) error {
	for _, id := range ids {
		if !slices.Contains(GuardRules, id) {
			return fmt.Errorf("names an unknown rule %q (rules: %s)", id, strings.Join(GuardRules, ", "))
		}
	}
	return nil
}

// keysWhere lists the keys of the settings ok takes, in table order.
func keysWhere(ok func(settingRow) bool) []string {
	var out []string
	for _, s := range settingRows {
		if ok(s) {
			out = append(out, s.key)
		}
	}
	return out
}

// lookup is the setting key names, by hand ones included.
func lookup(key string) (settingRow, bool) {
	i := slices.IndexFunc(settingRows, func(s settingRow) bool { return s.key == key })
	if i < 0 {
		return settingRow{}, false
	}
	return settingRows[i], true
}

// ProjectKeys are the settings of a [projects.<slug>] table that tm
// writes; all but ProjectOnly are also those of [defaults], and
// DefaultsOnly are only those of [defaults].
var ProjectKeys = keysWhere(func(s settingRow) bool { return s.reach != byHand })

// ProjectOnly are a project's own state, never all projects': an
// inactive, paused, archived, pinned or shared [defaults] would stop,
// hide, pin or share every project.
var ProjectOnly = keysWhere(func(s settingRow) bool { return s.reach == projectOnly })

// DefaultsOnly are all projects' settings, never a project's: the
// global cap counts the threads of every project.
var DefaultsOnly = keysWhere(func(s settingRow) bool { return s.reach == defaultsOnly })

// validKey reports whether key is a project setting tm writes.
func validKey(key string) bool { return slices.Contains(ProjectKeys, key) }

// CheckSetting checks value against the project setting key's type
// and limits, as SetProject and SetDefaults do before they write it.
// Load runs the same checks on the file's values.
func CheckSetting(key string, value any) error {
	s, ok := lookup(key)
	if !ok || s.reach == byHand {
		return fmt.Errorf("unknown setting %q", key)
	}
	return s.check(value)
}

// own lists the keys r sets (ProjectKeys order), auto_resolve as
// auto_close; never a project's own state or a by-hand setting, which
// don't follow all projects.
func (r rawSafety) own() []string {
	return keysWhere(func(s settingRow) bool {
		return (s.reach == everywhere || s.reach == defaultsOnly) && s.isSet(&r)
	})
}

// Value is the setting key's value in s (a bool, an int, a string or a
// []string), nil for a key Safety doesn't have.
func (s Safety) Value(key string) any {
	if st, ok := lookup(key); ok {
		return st.value(&s)
	}
	return nil
}

// Set sets the setting key in s to v, as a write of it does (the TUI
// shows a change before it is read back); a key Safety has no field
// for, or a v of another type, changes nothing.
func (s *Safety) Set(key string, v any) {
	if st, ok := lookup(key); ok {
		st.put(s, v)
	}
}

// apply sets in s every setting r names, checked; table names it in
// errors ("projects.demo", "defaults").
func (r rawSafety) apply(s *Safety, path, table string) error {
	for _, st := range settingRows {
		if err := st.apply(&r, s); err != nil {
			return fmt.Errorf("%s: %s.%s %w", path, table, st.key, err)
		}
	}
	return nil
}
