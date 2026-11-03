package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Renames and removals that keep the programs' tables right (the
// programs design §3.4, Rename and delete): tm project rename renames a
// member in the [programs.*] projects that list it, tm program rename
// renames a program's table, and archiving or deleting a member project
// takes it out of its program. These run whatever [features] programs
// says: they keep dormant tables naming projects that exist, and write
// no new form.

// ErrProgramSet means config.toml already has a table for the program
// a rename would name.
var ErrProgramSet = errors.New("config.toml already has settings for that program")

// programLists reads the [programs.*] projects of data: program slug →
// its members, for the tables that set projects.
func programLists(data []byte) (map[string][]string, error) {
	var raw struct {
		Programs map[string]struct {
			Projects *[]string `toml:"projects"`
		} `toml:"programs"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("the settings can't be read: %w", err)
	}
	out := map[string][]string{}
	for slug, t := range raw.Programs {
		if t.Projects != nil {
			out[slug] = *t.Projects
		}
	}
	return out, nil
}

// RenameMembers returns data with member from named to in every
// [programs.*] projects that lists it, in its place: tm project rename
// of a local member (from and to are project slugs). A file whose
// programs don't list from comes back as it was. ErrForm when a table
// sets projects in a form the line editor doesn't change.
func RenameMembers(data []byte, from, to string) ([]byte, error) {
	return editMembers(data, func(list []string) []string {
		out := slices.Clone(list)
		for i, m := range out {
			if m == from {
				out[i] = to
			}
		}
		return out
	}, from)
}

// RemoveMemberIn returns data with member left out of every [programs.*]
// projects that lists it, and the programs it was in.
func RemoveMemberIn(data []byte, member string) ([]byte, []string, error) {
	lists, err := programLists(data)
	if err != nil {
		return nil, nil, err
	}
	var in []string
	for slug, l := range lists {
		if slices.Contains(l, member) {
			in = append(in, slug)
		}
	}
	slices.Sort(in)
	out, err := editMembers(data, func(list []string) []string {
		return slices.DeleteFunc(slices.Clone(list), func(m string) bool { return m == member })
	}, member)
	return out, in, err
}

// RemoveMember takes member (a project's slug) out of the program that
// lists it, archiving or deleting it (§3.4); it returns that program, ""
// when none listed it.
func RemoveMember(member string) (string, error) {
	var in []string
	err := edit(func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, nil
		}
		out, programs, err := RemoveMemberIn(data, member)
		in = programs
		return out, err
	})
	if err != nil || len(in) == 0 {
		return "", err
	}
	return in[0], nil
}

// editMembers rewrites the projects of each program whose list names
// member with fn, checking that only those lists changed.
func editMembers(data []byte, fn func([]string) []string, member string) ([]byte, error) {
	lists, err := programLists(data)
	if err != nil {
		return nil, err
	}
	out := data
	want := map[string][]string{}
	for slug, l := range lists {
		want[slug] = l
		if !slices.Contains(l, member) {
			continue
		}
		next := fn(l)
		want[slug] = next
		if out, err = Edit(out, ProgramTable(slug), "projects", next); err != nil {
			return nil, err
		}
	}
	if len(out) == len(data) && string(out) == string(data) {
		return data, nil
	}
	got, err := programLists(out)
	if err != nil || !reflect.DeepEqual(normal(got), normal(want)) {
		return nil, ErrForm
	}
	return out, nil
}

// normal is lists with empty lists as nil, so DeepEqual compares
// members only.
func normal(lists map[string][]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range lists {
		if len(v) == 0 {
			v = nil
		}
		out[k] = v
	}
	return out
}

// RenameProgramTable returns data with program from's table, and any
// table below it, named for program to: tm program rename. Only the
// header lines change. A file without from's table comes back as it
// was; ErrProgramSet when to has one already, ErrForm when from's is
// set in a form the line editor doesn't change (dotted keys, an inline
// table).
func RenameProgramTable(data []byte, from, to string) ([]byte, error) {
	var before map[string]any
	if _, err := toml.Decode(string(data), &before); err != nil {
		return nil, fmt.Errorf("the settings can't be read: %w", err)
	}
	programs, _ := before["programs"].(map[string]any)
	if _, ok := programs[to]; ok {
		return nil, ErrProgramSet
	}
	if _, ok := programs[from]; !ok {
		return data, nil
	}
	lines := statements(data)
	for i, l := range lines {
		h, ok := header(l)
		if !ok || len(h) < 2 || h[0] != "programs" || h[1] != from {
			continue
		}
		m := headerRE.FindStringSubmatchIndex(strings.TrimRight(l, "\r\n"))
		lines[i] = l[:m[2]] + strings.Join(append([]string{"programs", to}, h[2:]...), ".") + l[m[3]:]
	}
	res := []byte(strings.Join(lines, ""))
	var after map[string]any
	if _, err := toml.Decode(string(res), &after); err != nil {
		return nil, ErrForm
	}
	want := map[string]any{}
	for k, v := range before {
		want[k] = v
	}
	moved := map[string]any{}
	for k, v := range programs {
		moved[k] = v
	}
	moved[to] = moved[from]
	delete(moved, from)
	want["programs"] = moved
	if !reflect.DeepEqual(after, want) {
		return nil, ErrForm
	}
	return res, nil
}

// RenameProgram renames program from's table to to's (RenameProgramTable),
// checked as Load checks the file.
func RenameProgram(from, to string) error {
	return editPrograms(func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, nil
		}
		return RenameProgramTable(data, from, to)
	})
}
