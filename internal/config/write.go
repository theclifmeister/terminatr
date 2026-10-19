package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// Writing settings (docs/SPEC.md §11.2). Only the TUI's settings popups
// call these, on the human's keypress, and the human-only tm project
// pause|resume|archive|unarchive: no socket method reaches them, so
// agents can't change safety settings. The file is
// edited line by line, so the user's comments, order and formatting stay
// as they were; the result is parsed before it replaces the file, which
// happens atomically under the file's lock. A line is a whole statement
// (statements): a value that spans lines, such as an array written one
// item per line, is read, replaced and removed as one.

// ErrForm means the file sets the setting in a form the line editor
// doesn't change (a dotted key, a key inside an inline table); the user
// edits it by hand.
var ErrForm = errors.New("this setting is set in a form tm can't change here")

// SetProject sets one of a project's safety settings by its key
// (start_threads, yolo, …), checked against the setting's type.
func SetProject(slug, key string, value any) error {
	if slices.Contains(DefaultsOnly, key) {
		return fmt.Errorf("%s is all projects' setting, not a project's", key)
	}
	return setSafety("projects."+slug, key, value)
}

// SetDefaults sets one of the all-projects settings ([defaults]): every
// project that doesn't set key itself follows it.
func SetDefaults(key string, value any) error {
	if slices.Contains(ProjectOnly, key) {
		return fmt.Errorf("%s is a project's own setting, not all projects'", key)
	}
	return setSafety(DefaultsTable, key, value)
}

// DefaultsTable is the all-projects settings' table.
const DefaultsTable = "defaults"

// UnsetProject removes a project's own value of key, so it follows all
// projects again; for auto_close the older auto_resolve goes too. ErrForm
// when the file sets it in a form the line editor doesn't change.
func UnsetProject(slug, key string) error { return unset("projects."+slug, key) }

// UnsetDefaults removes an all-projects setting's value ([defaults]), so
// every project follows the built-in default again.
func UnsetDefaults(key string) error {
	if slices.Contains(ProjectOnly, key) {
		return fmt.Errorf("%s is a project's own setting, not all projects'", key)
	}
	return unset(DefaultsTable, key)
}

func unset(table, key string) error {
	if !validKey(key) {
		return fmt.Errorf("unknown setting %q", key)
	}
	keys := []string{key}
	if key == "auto_close" {
		keys = append(keys, "auto_resolve")
	}
	return Unset(table, keys...)
}

// Unset removes keys from table ("mods.<id>", …), everything else kept;
// ErrForm when the file sets one in a form the line editor doesn't
// change.
func Unset(table string, keys ...string) error {
	return edit(func(data []byte) ([]byte, error) {
		for _, k := range keys {
			out := Remove(data, table, k)
			if bytes.Equal(out, data) && sets(data, table, k) {
				return nil, ErrForm
			}
			data = out
		}
		return data, nil
	})
}

// sets reports whether data sets key in table, in any form.
func sets(data []byte, table, key string) bool {
	var got map[string]any
	if _, err := toml.Decode(string(data), &got); err != nil {
		return false
	}
	v := any(got)
	for _, k := range append(splitTable(table), key) {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		if v, ok = m[k]; !ok {
			return false
		}
	}
	return true
}

// setSafety sets a safety setting in table, a project's or [defaults].
func setSafety(table, key string, value any) error {
	if err := CheckSetting(key, value); err != nil {
		return err
	}
	if key == "coordinator_agent" && table == DefaultsTable {
		// [defaults] coordinator_agent replaces the older top-level
		// default_agent: it goes in the same write.
		return edit(func(data []byte) ([]byte, error) {
			out, err := Edit(data, table, key, value)
			if err != nil {
				return nil, err
			}
			return Remove(out, "", "default_agent"), nil
		})
	}
	if key == "auto_close" {
		// auto_close replaces the older auto_resolve: its line goes in the
		// same write, so nothing obsolete is left ignored in the file.
		return edit(func(data []byte) ([]byte, error) {
			out, err := Edit(data, table, key, value)
			if err != nil {
				return nil, err
			}
			return Remove(out, table, "auto_resolve"), nil
		})
	}
	return Set(table, key, value)
}

// Set sets key in table ("" is the top level, "keys", "projects.<slug>")
// to value: a bool, an int or a string.
func Set(table, key string, value any) error {
	return edit(func(data []byte) ([]byte, error) { return Edit(data, table, key, value) })
}

// edit rewrites the settings file with fn, under its lock and
// atomically, keeping its mode; an unchanged file isn't written.
func edit(fn func(data []byte) ([]byte, error)) error {
	path, err := Path()
	if err != nil {
		return err
	}
	unlock, err := mdfile.Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	perm := fs.FileMode(0o600)
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if fi, err := os.Stat(path); err == nil {
			perm = fi.Mode().Perm()
		}
	}
	data, err := fn(old)
	if err != nil {
		return err
	}
	if bytes.Equal(old, data) {
		return nil
	}
	return fsx.WriteAtomic(path, data, perm)
}

// Edit returns data with key in table set to value, everything else kept.
func Edit(data []byte, table, key string, value any) ([]byte, error) {
	val, err := tomlValue(value)
	if err != nil {
		return nil, err
	}
	if _, err := toml.Decode(string(data), new(map[string]any)); err != nil {
		return nil, fmt.Errorf("the settings can't be read: %w", err)
	}
	bare := keyText(key)
	lines := statements(data)
	want := splitTable(table)
	in := len(want) == 0 // the top level comes first
	found := in
	last := -1 // the region's last setting line, or its header
	var out []string
	done := false
	for i, l := range lines {
		if h, ok := header(l); ok || isArrayHeader(l) {
			if in && !done {
				out = insertAt(out, last, bare+" = "+val+"\n", len(out))
				done = true
			}
			in = ok && equal(h, want)
			if in {
				found, last = true, len(out)
			}
			out = append(out, l)
			continue
		}
		if in && !done {
			if indent, comment, ok := setting(l, key); ok {
				nl := ""
				if strings.HasSuffix(l, "\n") || i < len(lines)-1 {
					nl = "\n"
				}
				out = append(out, indent+bare+" = "+val+comment+nl)
				done = true
				continue
			}
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
				last = len(out)
			}
		}
		out = append(out, l)
	}
	if n := len(out); n > 0 && !strings.HasSuffix(out[n-1], "\n") {
		out[n-1] += "\n"
	}
	switch {
	case done:
	case found:
		out = insertAt(out, last, bare+" = "+val+"\n", len(out))
	default:
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "\n")
		}
		out = append(out, "["+table+"]\n", bare+" = "+val+"\n")
	}
	res := []byte(strings.Join(out, ""))
	// The edit must parse and say what was meant; else the file sets it
	// in another form (dotted keys, inline tables) and stays untouched.
	var got map[string]any
	if _, err := toml.Decode(string(res), &got); err != nil {
		return nil, ErrForm
	}
	v := any(got)
	for _, k := range append(want, key) {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, ErrForm
		}
		v = m[k]
	}
	if n, ok := value.(int); ok {
		value = int64(n)
	}
	if want, ok := value.([]AgentModel); ok {
		arr, _ := v.([]map[string]any)
		if arr == nil {
			for _, x := range asSlice(v) {
				m, _ := x.(map[string]any)
				arr = append(arr, m)
			}
		}
		if len(arr) != len(want) {
			return nil, ErrForm
		}
		for i, x := range arr {
			if len(x) != 2 || x["name"] != any(want[i].Name) || x["about"] != any(want[i].About) {
				return nil, ErrForm
			}
		}
		return res, nil
	}
	if want, ok := value.([]GuardAllow); ok {
		var arr []map[string]any
		for _, x := range asSlice(v) {
			m, _ := x.(map[string]any)
			arr = append(arr, m)
		}
		if a, ok := v.([]map[string]any); ok {
			arr = a
		}
		if len(arr) != len(want) {
			return nil, ErrForm
		}
		for i, x := range arr {
			c, _ := x["command"].(string)
			p, _ := x["path"].(string)
			e, _ := x["exact"].(bool)
			n := 2
			if want[i].Exact {
				n = 3
			}
			if len(x) != n || x["rule"] != any(want[i].Rule) || c != want[i].Command || p != want[i].Path || e != want[i].Exact {
				return nil, ErrForm
			}
		}
		return res, nil
	}
	if want, ok := value.([]ExcludedCommand); ok {
		var got excludedList
		if err := got.UnmarshalTOML(v); err != nil || !slices.Equal(got, excludedList(want)) {
			return nil, ErrForm
		}
		return res, nil
	}
	if want, ok := value.([]Guide); ok {
		var got guideList
		if err := got.UnmarshalTOML(v); err != nil || !slices.Equal(got, guideList(want)) {
			return nil, ErrForm
		}
		return res, nil
	}
	if want, ok := value.(EnvSource); ok {
		if m, _ := v.(map[string]any); len(m) != 1 || m["from"] != any(want.From) {
			return nil, ErrForm
		}
		return res, nil
	}
	if want, ok := value.(Link); ok {
		if got, _, err := parseLink(v); err != nil || got != want {
			return nil, ErrForm
		}
		return res, nil
	}
	if want, ok := value.(remoteLink); ok {
		got, askSet, err := parseLink(v)
		if !askSet {
			got.Ask = true
		}
		if err != nil || got != Link(want) {
			return nil, ErrForm
		}
		return res, nil
	}
	if want, ok := value.([]string); ok {
		// A TOML array decodes as []any.
		arr, _ := v.([]any)
		if len(arr) != len(want) {
			return nil, ErrForm
		}
		for i, x := range arr {
			if x != any(want[i]) {
				return nil, ErrForm
			}
		}
		return res, nil
	}
	if v != value {
		return nil, ErrForm
	}
	return res, nil
}

// asSlice is a decoded TOML array, nil for another value.
func asSlice(v any) []any {
	a, _ := v.([]any)
	return a
}

// Remove returns data without the line setting key in table, everything
// else kept: comments above it and the rest of the table too. When the
// file sets key in a form the line editor doesn't change (a dotted key,
// an inline table), or the result wouldn't parse without it, data comes
// back as it was.
func Remove(data []byte, table, key string) []byte {
	lines := statements(data)
	want := splitTable(table)
	in := len(want) == 0
	var out []string
	for _, l := range lines {
		if h, ok := header(l); ok {
			in = equal(h, want)
		} else if isArrayHeader(l) {
			in = false
		} else if _, _, ok := setting(l, key); ok && in {
			continue
		}
		out = append(out, l)
	}
	res := []byte(strings.Join(out, ""))
	var got map[string]any
	if _, err := toml.Decode(string(res), &got); err != nil {
		return data
	}
	v := any(got)
	for _, k := range want {
		m, ok := v.(map[string]any)
		if !ok {
			return res // the table is gone: so is key
		}
		v = m[k]
	}
	if m, ok := v.(map[string]any); ok {
		if _, still := m[key]; still {
			return data
		}
	}
	return res
}

// insertAt inserts s after index i of lines (at the start for -1); at
// end appends.
func insertAt(lines []string, i int, s string, end int) []string {
	if i+1 >= end {
		return append(lines, s)
	}
	out := append([]string{}, lines[:i+1]...)
	out = append(out, s)
	return append(out, lines[i+1:]...)
}

// keyText is key as a TOML key: bare when it can be, else quoted
// ("mini/acme-api").
func keyText(key string) string {
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return strconv.Quote(key)
		}
	}
	return key
}

func tomlValue(v any) (string, error) {
	switch v := v.(type) {
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case []AgentModel:
		parts := make([]string, len(v))
		for i, x := range v {
			n, _ := tomlValue(x.Name)
			a, _ := tomlValue(x.About)
			parts[i] = "{ name = " + n + ", about = " + a + " }"
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case []GuardAllow:
		parts := make([]string, len(v))
		for i, x := range v {
			r, _ := tomlValue(x.Rule)
			k, t := "command", x.Command
			if x.Path != "" {
				k, t = "path", x.Path
			}
			q, _ := tomlValue(t)
			parts[i] = "{ rule = " + r + ", " + k + " = " + q
			if x.Exact {
				parts[i] += ", exact = true"
			}
			parts[i] += " }"
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case []ExcludedCommand:
		parts := make([]string, len(v))
		for i, x := range v {
			q, err := tomlValue(x.Command)
			if err != nil {
				return "", err
			}
			parts[i] = q
			if !x.Ask {
				parts[i] = "{ command = " + q + ", ask = false }"
			}
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case []Guide:
		// An entry a line, its body a multi-line string; a name of tm
		// 0.38 as it was, and names only on one line.
		if !slices.ContainsFunc(v, func(g Guide) bool { return g.Body != "" }) {
			return tomlValue(guideList(v).titles())
		}
		var b strings.Builder
		b.WriteString("[\n")
		for _, g := range v {
			t, _ := tomlValue(g.Title)
			if g.Body == "" {
				b.WriteString("  " + t + ",\n")
				continue
			}
			b.WriteString("  { title = " + t + ", body = " + multiline(g.Body) + " },\n")
		}
		b.WriteString("]")
		return b.String(), nil
	case EnvSource:
		from, _ := tomlValue(v.From)
		return "{ from = " + from + " }", nil
	case Link:
		allow, _ := tomlValue(v.Allow)
		if !v.Ask {
			return allow, nil
		}
		return "{ allow = " + allow + ", ask = true }", nil
	case remoteLink:
		allow, _ := tomlValue(v.Allow)
		if v.Ask {
			return allow, nil
		}
		return "{ allow = " + allow + ", ask = false }", nil
	case []string:
		parts := make([]string, len(v))
		for i, x := range v {
			q, err := tomlValue(x)
			if err != nil {
				return "", err
			}
			parts[i] = q
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case string:
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range v {
			switch {
			case r == '"' || r == '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&b, `\u%04X`, r)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
		return b.String(), nil
	}
	return "", fmt.Errorf("can't write a %T setting", v)
}

// multiline is s as a TOML multi-line basic string, its lines as they
// are, and a newline after them: a backslash, a quote that would close
// it and a control character but tab and newline escaped.
func multiline(s string) string {
	var b strings.Builder
	b.WriteString(`"""` + "\n")
	for i, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"' && i+1 < len(s) && s[i+1] == '"':
			b.WriteString(`\"`)
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	// Closed on a line of its own; the reader takes that newline off.
	b.WriteString("\n" + `"""`)
	return b.String()
}

// statements splits data into its lines, each with its line end, but
// keeps a statement whose value goes on past its line end (an array or
// inline table whose brackets close on a later line, a multi-line
// string) in one: header and setting then never read a line inside a
// value, and replacing or removing a setting takes its whole value.
// Brackets and '#' inside strings and comments don't count.
func statements(data []byte) []string {
	var out []string
	var cur strings.Builder
	depth, ml := 0, "" // open brackets; the open multi-line string's quotes
	for _, l := range strings.SplitAfter(string(data), "\n") {
		if l == "" {
			continue
		}
		cur.WriteString(l)
		depth, ml = scanLine(l, depth, ml)
		if depth == 0 && ml == "" {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// scanLine reads one line of TOML from the state the lines before left
// (depth open brackets, ml the quotes of an open multi-line string) and
// returns the state after it.
func scanLine(l string, depth int, ml string) (int, string) {
	for i := 0; i < len(l); i++ {
		if ml != "" {
			switch {
			case ml == `"""` && l[i] == '\\':
				i++
			case strings.HasPrefix(l[i:], ml):
				i += 2
				for k := 0; k < 2 && i+1 < len(l) && l[i+1] == ml[0]; k++ {
					i++ // up to two quotes end the string's content
				}
				ml = ""
			}
			continue
		}
		switch c := l[i]; c {
		case '#':
			return depth, ""
		case '"', '\'':
			if q := strings.Repeat(string(c), 3); strings.HasPrefix(l[i:], q) {
				ml, i = q, i+2
				continue
			}
			for i++; i < len(l) && l[i] != c && l[i] != '\n'; i++ {
				if c == '"' && l[i] == '\\' {
					i++
				}
			}
		case '[', '{':
			depth++
		case ']', '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth, ml
}

var headerRE = regexp.MustCompile(`^\s*\[\s*([^\[\]#]+?)\s*\]\s*(#.*)?$`)

// header reads a [table] line (not an [[array]] one).
func header(l string) ([]string, bool) {
	m := headerRE.FindStringSubmatch(strings.TrimRight(l, "\r\n"))
	if m == nil {
		return nil, false
	}
	return splitTable(m[1]), true
}

// isArrayHeader is an [[array of tables]] line: it ends the table before it.
func isArrayHeader(l string) bool {
	return arrayHeaderRE.MatchString(strings.TrimRight(l, "\r\n"))
}

// splitTable splits a table name at its dots, quotes and spaces removed.
// Slugs and our keys need no quoting, so a dot inside quotes isn't read.
func splitTable(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && (p[0] == '"' || p[0] == '\'') && p[len(p)-1] == p[0] {
			p = p[1 : len(p)-1]
		}
		parts[i] = p
	}
	return parts
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// valueRE is a simple value with an optional comment after it.
var valueRE = regexp.MustCompile(`^\s*(true|false|[+-]?[0-9_]+|"(?:[^"\\]|\\.)*"|'[^']*')(\s*#.*)?$`)

// setting reads a "key = value" line for key: its indentation and the
// comment after the value, kept when the value is a simple one.
func setting(l, key string) (indent, comment string, ok bool) {
	t := strings.TrimRight(l, "\r\n")
	rest := strings.TrimLeft(t, " \t")
	indent = t[:len(t)-len(rest)]
	k, v, ok := strings.Cut(rest, "=")
	if !ok {
		return "", "", false
	}
	k = strings.TrimSpace(k)
	if len(k) >= 2 && (k[0] == '"' || k[0] == '\'') && k[len(k)-1] == k[0] {
		k = k[1 : len(k)-1]
	}
	if k != key {
		return "", "", false
	}
	if m := valueRE.FindStringSubmatch(v); m != nil {
		comment = m[2]
	}
	return indent, comment, true
}

// ClearProject removes keys from a project's table, e.g. a deleted
// project's archived, paused and repos, so a new project of the same
// slug starts without them.
func ClearProject(slug string, keys ...string) error {
	return edit(func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, nil
		}
		for _, k := range keys {
			data = Remove(data, "projects."+slug, k)
		}
		return data, nil
	})
}

// SetRepos writes project slug's repo list ([projects.<slug>] repos),
// an empty one too. No socket method writes it: the user's tm project
// repo and project popup do, and the coordinator's tm project repo
// remove, which runs in the server (cli.run).
func SetRepos(slug string, repos []string) error {
	return UpdateRepos(slug, func([]string, bool) ([]string, error) { return repos, nil })
}

// UpdateRepos rewrites project slug's repo list with fn under the
// file's lock: fn gets the list and whether the table sets one, and
// returns the new list, written even when empty.
func UpdateRepos(slug string, fn func(old []string, set bool) ([]string, error)) error {
	return edit(func(data []byte) ([]byte, error) {
		var raw struct {
			Projects map[string]struct {
				Repos *[]string `toml:"repos"`
			} `toml:"projects"`
		}
		if _, err := toml.Decode(string(data), &raw); err != nil {
			return nil, fmt.Errorf("the settings can't be read: %w", err)
		}
		var old []string
		r := raw.Projects[slug].Repos
		if r != nil {
			old = *r
		}
		repos, err := fn(slices.Clone(old), r != nil)
		if err != nil {
			return nil, err
		}
		if repos == nil {
			repos = []string{}
		}
		out, err := Edit(data, "projects."+slug, "repos", repos)
		if err != nil {
			return nil, err
		}
		if !sets(out, "projects."+slug, "repos") {
			return nil, ErrForm
		}
		return out, nil
	})
}

// ErrProjectSet means config.toml already has settings for the new slug.
var ErrProjectSet = errors.New("config.toml already has settings for that project")

// RenameProject moves a project's settings, [projects.<from>] and any
// table below it, to [projects.<to>] (tm project rename, docs/SPEC.md
// §5.1). Only the header lines change: comments, order and the settings
// themselves stay. ErrForm when the file sets them in a form the line
// editor doesn't change (dotted keys, an inline table).
func RenameProject(from, to string) error {
	return edit(func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, nil
		}
		return RenameTable(data, from, to)
	})
}

// RenameTable returns data with the tables of project from renamed to
// project to. A file without settings for from comes back as it was.
func RenameTable(data []byte, from, to string) ([]byte, error) {
	var before map[string]any
	if _, err := toml.Decode(string(data), &before); err != nil {
		return nil, fmt.Errorf("the settings can't be read: %w", err)
	}
	projects, _ := before["projects"].(map[string]any)
	if _, ok := projects[to]; ok {
		return nil, ErrProjectSet
	}
	if _, ok := projects[from]; !ok && !linkedTo(projects, from) {
		return data, nil
	}
	lines := statements(data)
	inLinks := false
	for i, l := range lines {
		h, ok := header(l)
		if !ok {
			// Other projects' links name the project by its slug: the key
			// line follows it (docs/SPEC.md §7.9).
			if _, _, isKey := setting(l, from); inLinks && isKey {
				lines[i] = strings.Replace(l, from, to, 1)
			}
			continue
		}
		inLinks = len(h) == 3 && h[0] == "projects" && h[2] == "links"
		if len(h) < 2 || h[0] != "projects" || h[1] != from {
			continue
		}
		m := headerRE.FindStringSubmatchIndex(strings.TrimRight(l, "\r\n"))
		name := append([]string{"projects", to}, h[2:]...)
		lines[i] = l[:m[2]] + strings.Join(name, ".") + l[m[3]:]
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
	for k, v := range projects {
		if t, ok := v.(map[string]any); ok {
			if links, ok := t["links"].(map[string]any); ok {
				if lv, ok := links[from]; ok {
					nt, nl := maps.Clone(t), maps.Clone(links)
					nl[to] = lv
					delete(nl, from)
					nt["links"] = nl
					v = nt
				}
			}
		}
		moved[k] = v
	}
	if v, ok := moved[from]; ok {
		moved[to] = v
		delete(moved, from)
	}
	want["projects"] = moved
	if !reflect.DeepEqual(after, want) {
		return nil, ErrForm
	}
	return res, nil
}

// linkedTo reports whether a project's links name slug.
func linkedTo(projects map[string]any, slug string) bool {
	for _, v := range projects {
		t, _ := v.(map[string]any)
		links, _ := t["links"].(map[string]any)
		if _, ok := links[slug]; ok {
			return true
		}
	}
	return false
}
