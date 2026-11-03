package config

import (
	"fmt"
	"slices"
)

// Writing a program's table (the programs design §3.3): only the TUI's
// program settings popup calls these, on the human's keypress, as it
// does the projects' settings (write.go); no socket method reaches them,
// so neither a director nor a coordinator can change a program's
// members or authority. The file is edited line by line, and the result
// must pass Load's program checks (one program per project, the forms
// and values) before it replaces the file.

// programKinds are the keys of a [programs.<slug>] table and the type of
// each one's value.
var programKinds = map[string]string{
	"projects": "list", "authority": "string", "parallel_threads": "int", "dependency_met": "string",
	"director_agent": "string", "director_model": "string",
	"auto_clear": "bool", "remote_control": "bool", "answer_facts": "bool", "search_children": "bool", "paused": "bool",
}

// ProgramTable is program slug's table in the settings file.
func ProgramTable(slug string) string { return "programs." + slug }

// SetProgram sets key of program slug's table to value (projects takes
// a []string, in the order the program lists them), checked as Load
// checks the file.
func SetProgram(slug, key string, value any) error {
	if err := checkProgramKey(slug, key); err != nil {
		return err
	}
	ok := false
	switch value.(type) {
	case []string:
		ok = programKinds[key] == "list"
	case string:
		ok = programKinds[key] == "string"
	case int:
		ok = programKinds[key] == "int"
	case bool:
		ok = programKinds[key] == "bool"
	}
	if !ok {
		return fmt.Errorf("%s takes a %s, not %T", key, programKinds[key], value)
	}
	return editPrograms(func(data []byte) ([]byte, error) { return Edit(data, ProgramTable(slug), key, value) })
}

// UnsetProgram removes key from program slug's table, so it takes its
// default again.
func UnsetProgram(slug, key string) error {
	if err := checkProgramKey(slug, key); err != nil {
		return err
	}
	return editPrograms(func(data []byte) ([]byte, error) {
		out := Remove(data, ProgramTable(slug), key)
		if slices.Equal(out, data) && sets(data, ProgramTable(slug), key) {
			return nil, ErrForm
		}
		return out, nil
	})
}

func checkProgramKey(slug, key string) error {
	if !ValidProgramSlug(slug) {
		return fmt.Errorf("%q is not a program slug (a-z, 0-9 and -, lower case)", slug)
	}
	if programKinds[key] == "" {
		return fmt.Errorf("unknown program setting %q", key)
	}
	return nil
}

// editPrograms is edit, refusing a result whose programs Load would
// refuse.
func editPrograms(fn func(data []byte) ([]byte, error)) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return edit(func(data []byte) ([]byte, error) {
		out, err := fn(data)
		if err != nil {
			return nil, err
		}
		c, err := parse(path, out)
		if c == nil {
			return nil, err
		}
		if err := c.checkPrograms(); err != nil {
			return nil, err
		}
		return out, nil
	})
}

// The child side's program keys (the programs design §9.1): a project's
// membership of a paired server's program, in its own [projects.<slug>]
// table. Only accepting that program's invite sets program (the
// TUI's Messages tab or tm program accept, the human's), and leaving
// unsets them all; the TUI's project settings change the other three,
// each with its own confirm. They aren't settings tm access or the
// generic setting writers reach (CheckSetting doesn't know them), so no
// agent sets them.

// ProgramLinkKeys are a project's program keys, program first.
var ProgramLinkKeys = []string{"program", "program_tasks", "program_starts", "program_status"}

// SetProgramLink writes project slug's membership l: all four keys.
func SetProgramLink(slug string, l ProgramLink) error {
	if alias, ps, ok := SplitMember(l.Program); !ok || alias == "" || !ValidProgramSlug(ps) {
		return fmt.Errorf("program %q: a paired server's program, <server>/<program>", l.Program)
	}
	return editPrograms(func(data []byte) ([]byte, error) {
		out := data
		for _, kv := range []struct {
			k string
			v any
		}{{"program", l.Program}, {"program_tasks", l.Tasks}, {"program_starts", l.Starts}, {"program_status", l.Status}} {
			var err error
			if out, err = Edit(out, "projects."+slug, kv.k, kv.v); err != nil {
				return nil, err
			}
		}
		return out, nil
	})
}

// SetProgramLinkKey changes one of program_tasks (a string),
// program_starts or program_status (bools) of project slug, which must
// be a member of a paired server's program.
func SetProgramLinkKey(slug, key string, value any) error {
	ok := false
	switch value.(type) {
	case string:
		ok = key == "program_tasks"
	case bool:
		ok = key == "program_starts" || key == "program_status"
	}
	if !ok {
		return fmt.Errorf("%s isn't a program key that takes %T", key, value)
	}
	return editPrograms(func(data []byte) ([]byte, error) {
		path, _ := Path()
		if c, _ := parse(path, data); c == nil {
			return nil, fmt.Errorf("the settings file doesn't read")
		} else if _, member := c.ProgramLink(slug); !member {
			return nil, fmt.Errorf("%s isn't in a paired server's program", slug)
		}
		return Edit(data, "projects."+slug, key, value)
	})
}

// UnsetProgramLink removes project slug's program keys: it leaves the
// program. ErrForm when the file sets one in a form the line editor
// doesn't change.
func UnsetProgramLink(slug string) error {
	return editPrograms(func(data []byte) ([]byte, error) {
		out := data
		for _, k := range ProgramLinkKeys {
			next := Remove(out, "projects."+slug, k)
			if slices.Equal(next, out) && sets(out, "projects."+slug, k) {
				return nil, ErrForm
			}
			out = next
		}
		return out, nil
	})
}
