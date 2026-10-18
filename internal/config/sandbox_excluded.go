package config

import (
	"fmt"
	"maps"
	"slices"
)

// Excluded commands ask first or run without asking (T574 §4.6, case
// 1): one rule, one choice. A plain entry asks first, which the guard
// makes hold whatever other allow rules say (it answers the command
// with "ask"); a table with ask = false runs without asking, for which
// tm writes the Bash(...) allow rule itself:
//
//	excluded_commands = ["docker compose *", { command = "npx playwright *", ask = false }]
//
// A Bash(...) allow rule of the allow list that covers an excluded
// command, as before this choice, still makes it run without asking.

// ExcludedCommand is one excluded command and whether it asks first.
type ExcludedCommand struct {
	Command string
	Ask     bool
}

// excludedList is an excluded_commands list as config.toml has it.
type excludedList []ExcludedCommand

// UnmarshalTOML reads each entry: a command, or a table { command = …,
// ask = … }; any other form fails the file's load, as a wrong type does.
func (l *excludedList) UnmarshalTOML(v any) error {
	vs, ok := v.([]any)
	if ms, tables := v.([]map[string]any); tables {
		// A list of tables only.
		for _, m := range ms {
			vs = append(vs, m)
		}
		ok = true
	}
	if !ok {
		return fmt.Errorf("excluded_commands must be a list")
	}
	out := make(excludedList, 0, len(vs))
	for _, e := range vs {
		x, err := parseExcluded(e)
		if err != nil {
			return fmt.Errorf("excluded_commands: %w", err)
		}
		out = append(out, x)
	}
	*l = out
	return nil
}

// parseExcluded reads one entry of excluded_commands.
func parseExcluded(v any) (ExcludedCommand, error) {
	switch v := v.(type) {
	case string:
		return ExcludedCommand{Command: v, Ask: true}, nil
	case map[string]any:
		x := ExcludedCommand{Ask: true}
		for _, k := range slices.Sorted(maps.Keys(v)) {
			switch k {
			case "command":
				s, ok := v[k].(string)
				if !ok {
					return x, fmt.Errorf("command must be text, e.g. command = \"npx playwright *\"")
				}
				x.Command = s
			case "ask":
				b, ok := v[k].(bool)
				if !ok {
					return x, fmt.Errorf("ask must be true or false")
				}
				x.Ask = b
			default:
				return x, fmt.Errorf("an entry has an unknown key %q (command, ask)", k)
			}
		}
		if x.Command == "" {
			return x, fmt.Errorf("an entry needs command = …, e.g. { command = \"npx playwright *\", ask = false }")
		}
		return x, nil
	}
	return ExcludedCommand{}, fmt.Errorf("an entry is a command, or a table { command = …, ask = false }")
}

// commands are the list's commands, in its order.
func (l excludedList) commands() []string {
	out := make([]string, len(l))
	for i, x := range l {
		out[i] = x.Command
	}
	return out
}

// withoutAsking are the list's commands with ask = false.
func (l excludedList) withoutAsking() []string {
	var out []string
	for _, x := range l {
		if !x.Ask {
			out = append(out, x.Command)
		}
	}
	return out
}

// excludedOf is list as entries of excluded_commands: each command
// without asking when noAsk has it, else asking first.
func excludedOf(list, noAsk []string) []ExcludedCommand {
	out := make([]ExcludedCommand, len(list))
	for i, c := range list {
		out[i] = ExcludedCommand{Command: c, Ask: !slices.Contains(noAsk, c)}
	}
	return out
}

// WithoutAskingOf is one table's excluded commands that run without
// asking (ask = false): all projects' for slug "", else the project's
// own. None for a nil Config.
func (c *Config) WithoutAskingOf(slug string) []string {
	if c == nil {
		return nil
	}
	r := c.defaults.Sandbox
	if slug != "" {
		r = c.projects[slug].Sandbox
	}
	if r == nil || r.ExcludedCommands == nil {
		return nil
	}
	return r.ExcludedCommands.withoutAsking()
}

// SetExcludedAsk makes excluded command cmd of table's own list
// (DefaultsTable or "projects.<slug>") ask first, or run without
// asking. The list must have it.
func SetExcludedAsk(table, cmd string, ask bool) error {
	t, err := sandboxTable(table, SandboxExcluded)
	if err != nil {
		return err
	}
	return edit(func(data []byte) ([]byte, error) {
		r, err := rawSandboxIn(data, table)
		if err != nil {
			return nil, err
		}
		if r == nil || r.ExcludedCommands == nil || !slices.Contains(r.ExcludedCommands.commands(), cmd) {
			return nil, fmt.Errorf("%q isn't in this list of excluded commands", cmd)
		}
		list := slices.Clone(*r.ExcludedCommands)
		for i := range list {
			if list[i].Command == cmd {
				list[i].Ask = ask
			}
		}
		return Edit(data, t, SandboxExcluded, []ExcludedCommand(list))
	})
}
