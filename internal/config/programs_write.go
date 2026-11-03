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
