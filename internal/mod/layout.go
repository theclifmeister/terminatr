package mod

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/view"
)

// Layouts (layout.toml; docs/SPEC.md §5.3, the layout v2 design §2).
//
// A layout states only what it changes: every key is optional, an
// absent one meaning the built-in value. v1 (any mod API 1.x) is
// sidebar = ["count", …], the sidebar's column order; v2 (mod API 1.5)
// makes sidebar a table and adds the others:
//
//	[sidebar]    columns, side, width, mode; sections, project_sections (1.7)
//	[details]    place, size
//	[header]     style
//	[footer]     style
//	[status_bar] style
//	[rows]       density
//	[dashboard]  sections

// LayoutAPIMinor is the mod API minor a v2 layout needs.
const LayoutAPIMinor = 5

// SectionsAPIMinor is the mod API minor sidebar.sections and
// sidebar.project_sections need, as the sidebar slots do.
const SectionsAPIMinor = SidebarAPIMinor

// SectionTree is the reserved id in sidebar.sections that marks where the
// tree sits: the band sections listed before it go above the tree.
const SectionTree = "tree"

// MaxLayoutSections bounds each of sidebar.sections and
// sidebar.project_sections.
const MaxLayoutSections = 32

// Layout is a layout mod's file. An empty string, a zero Size or a nil
// list is a key the file doesn't set.
type Layout struct {
	// Columns is sidebar.columns, or the v1 array: the order of the
	// sidebar's columns, the built-in ids (view.DefaultColumns) and mods'
	// ("review-age.age"). A safety column it leaves out is still shown, at
	// the end (view.Columns).
	Columns []string
	Side    string // sidebar.side: left, right
	Width   Size   // sidebar.width
	Mode    string // sidebar.mode: full, slim, hidden
	Details string // details.place: right, bottom, off
	DetSize Size   // details.size
	// Header, Footer and StatusBar are their tables' style: shown or
	// hidden; shown, compact or hidden; shown or compact.
	Header, Footer, StatusBar string
	Density                   string   // rows.density: normal, compact
	Sections                  []string // dashboard.sections: needs, project, other, cards
	// SideSections is sidebar.sections, the band's order: mods' sidebar
	// sections ("ticker.ticker"), those before SectionTree above the
	// tree. ProjectSections is sidebar.project_sections, the order of the
	// sections under each project. Both need mod API 1.7.
	SideSections, ProjectSections []string
	// V2 is set when the file has anything but the v1 array: it needs mod
	// API 1.5.
	V2 bool
	// v2Key is the first v2 table or key, as the API check names it;
	// v17Key the first that needs mod API 1.7.
	v2Key, v17Key string
}

// Size is a size in cells (or rows), or a share of the window ("35%" →
// 0.35): one of them, both zero when unset.
type Size struct {
	Cells int
	Share float64
}

// IsZero reports whether the size is unset.
func (s Size) IsZero() bool { return s.Cells == 0 && s.Share == 0 }

func (s Size) String() string {
	if s.Share != 0 {
		return fmt.Sprintf("%q", strconv.Itoa(int(s.Share*100+0.5))+"%")
	}
	return strconv.Itoa(s.Cells)
}

// Dashboard section ids, dashboard.sections' values.
var LayoutSections = []string{"needs", "project", "other", "cards"}

// layoutTables are the v2 tables and their keys, in the order they are
// checked (and the API check names the first).
var layoutTables = []layoutTable{
	{"sidebar", []string{"columns", "side", "width", "mode", "sections", "project_sections"}},
	{"details", []string{"place", "size"}},
	{"header", []string{"style"}},
	{"footer", []string{"style"}},
	{"status_bar", []string{"style"}},
	{"rows", []string{"density"}},
	{"dashboard", []string{"sections"}},
}

type layoutTable struct {
	name string
	keys []string
}

// layoutEnums are the values each word key takes.
var layoutEnums = map[string][]string{
	"sidebar.side":     {"left", "right"},
	"sidebar.mode":     {"full", "slim", "hidden"},
	"details.place":    {"right", "bottom", "off"},
	"header.style":     {"shown", "hidden"},
	"footer.style":     {"shown", "compact", "hidden"},
	"status_bar.style": {"shown", "compact"},
	"rows.density":     {"normal", "compact"},
}

// shareRE is a share of the window, "25%".
var shareRE = regexp.MustCompile(`^([0-9]{1,3})%$`)

// ParseLayout reads and checks a layout, v1 or v2, joining every error
// with its key. It refuses what can't be used (an unknown key, a wrong
// type, a value or size it doesn't take); what is only a fallback (a
// hidden sidebar, sections without needs) is the console's to fix up as
// it loads.
func ParseLayout(data []byte) (Layout, error) {
	var l Layout
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return l, err
	}
	p := layoutParser{l: &l}
	if arr, ok := raw["sidebar"].([]any); ok {
		l.Columns = p.columns("sidebar", arr)
	}
	for _, t := range layoutTables {
		v, ok := raw[t.name]
		if !ok {
			continue
		}
		if _, v1 := v.([]any); v1 && t.name == "sidebar" {
			continue
		}
		tab, ok := v.(map[string]any)
		if !ok {
			if t.name == "sidebar" {
				p.fail("sidebar: an array of columns, or a table, [sidebar]")
			} else {
				p.fail("%s: a table, [%s]", t.name, t.name)
			}
			continue
		}
		first := "[" + t.name + "]"
		for _, k := range t.keys {
			if v, ok := tab[k]; ok {
				if !strings.Contains(first, " ") {
					first += " " + k
				}
				p.key(t.name+"."+k, v)
				if t.name == "sidebar" && (k == "sections" || k == "project_sections") && l.v17Key == "" {
					l.v17Key = "[sidebar] " + k
				}
			}
		}
		if !l.V2 {
			l.V2, l.v2Key = true, first
		}
		for _, k := range slices.Sorted(maps.Keys(tab)) {
			if !slices.Contains(t.keys, k) {
				p.fail("unknown key %s.%s (%s takes %s)", t.name, k, t.name, strings.Join(t.keys, ", "))
			}
		}
	}
	for _, k := range slices.Sorted(maps.Keys(raw)) {
		if !slices.ContainsFunc(layoutTables, func(t layoutTable) bool { return t.name == k }) {
			p.fail("unknown key %s", k)
		}
	}
	p.sizes()
	return l, errors.Join(p.errs...)
}

// CheckAPI says why a mod written for mod API api can't carry the
// layout: a v2 one needs 1.5, and one with the sidebar's section keys
// 1.7, so that an older tm refuses the mod by its api (CheckAPI) rather
// than by a parse error. A v1 one goes with any 1.x.
func (l Layout) CheckAPI(api string) error {
	if !l.V2 {
		return nil
	}
	maj, min, ok := splitAPI(api)
	if !ok || maj != 1 {
		return nil
	}
	switch {
	case l.v17Key != "" && min < SectionsAPIMinor:
		return fmt.Errorf(`%s needs api = "1.%d" in mod.toml`, l.v17Key, SectionsAPIMinor)
	case min < LayoutAPIMinor:
		return fmt.Errorf(`%s needs api = "1.%d" in mod.toml`, l.v2Key, LayoutAPIMinor)
	}
	return nil
}

// checkLayoutAPI is CheckAPI for the mod's layout, which tm mod install
// asks with the manifest's: nil when it has none, or one that doesn't
// parse (CheckData says why).
func (m Mod) checkLayoutAPI() error {
	if !m.Has(KindLayout) {
		return nil
	}
	data, err := m.Data(KindLayout)
	if err != nil {
		return nil
	}
	l, err := ParseLayout(data)
	if err != nil {
		return nil
	}
	if err := l.CheckAPI(m.Manifest.API); err != nil {
		return fmt.Errorf("%s: %w", KindLayout, err)
	}
	return nil
}

// layoutParser collects a layout's values and errors.
type layoutParser struct {
	l    *Layout
	errs []error
	// width, detSize: how the sizes were written (sizeCells,
	// sizeShare), checked once the place is known.
	width, detSize int
}

// How a size is written.
const (
	sizeUnset = iota
	sizeCells
	sizeShare
)

func (p *layoutParser) fail(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

// key reads one v2 key, its name as table.key.
func (p *layoutParser) key(name string, v any) {
	l := p.l
	switch name {
	case "sidebar.columns":
		arr, ok := v.([]any)
		if !ok {
			p.fail("sidebar.columns: a list of columns, [\"count\", …]")
			return
		}
		l.Columns = p.columns(name, arr)
	case "sidebar.width":
		l.Width, p.width = p.size(name, v)
	case "details.size":
		l.DetSize, p.detSize = p.size(name, v)
	case "dashboard.sections":
		l.Sections = p.sections(v)
	case "sidebar.sections":
		l.SideSections = p.sideSections(name, v, true)
	case "sidebar.project_sections":
		l.ProjectSections = p.sideSections(name, v, false)
	default:
		s := p.enum(name, v)
		switch name {
		case "sidebar.side":
			l.Side = s
		case "sidebar.mode":
			l.Mode = s
		case "details.place":
			l.Details = s
		case "header.style":
			l.Header = s
		case "footer.style":
			l.Footer = s
		case "status_bar.style":
			l.StatusBar = s
		case "rows.density":
			l.Density = s
		}
	}
}

// columns checks the sidebar's columns, key naming them: built-in ids
// or a mod's, each once, at most view.MaxColumns.
func (p *layoutParser) columns(key string, arr []any) []string {
	cols := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			p.fail("%s: columns are strings", key)
			return nil
		}
		cols = append(cols, s)
	}
	if len(cols) > view.MaxColumns {
		p.fail("%s: %d columns, at most %d", key, len(cols), view.MaxColumns)
	}
	seen := map[string]bool{}
	for _, c := range cols {
		switch {
		case !slices.Contains(view.DefaultColumns, c) && !view.IsModColumn(c):
			p.fail("%s: no column %q (built in: %s; a mod's: <mod>.<column>)", key, c, strings.Join(view.DefaultColumns, ", "))
		case seen[c]:
			p.fail("%s: %s is listed twice", key, c)
		}
		seen[c] = true
	}
	return cols
}

// enum reads a word key, one of layoutEnums[key].
func (p *layoutParser) enum(key string, v any) string {
	allowed := layoutEnums[key]
	want := strings.Join(allowed[:len(allowed)-1], ", ") + " or " + allowed[len(allowed)-1]
	s, ok := v.(string)
	switch {
	case !ok:
		p.fail("%s: %s; a string, %s", key, show(v), want)
	case key == "header.style" && s == "compact":
		p.fail("%s: %q: the header is one row, %s", key, s, want)
	case !slices.Contains(allowed, s):
		p.fail("%s: %q; %s", key, s, want)
	default:
		return s
	}
	return ""
}

// size reads a size: an integer, or a share "N%"; how it was written,
// sizeUnset when it wasn't one.
func (p *layoutParser) size(key string, v any) (Size, int) {
	switch v := v.(type) {
	case int64:
		return Size{Cells: int(min(max(v, -1), 1<<20))}, sizeCells
	case string:
		if m := shareRE.FindStringSubmatch(v); m != nil {
			n, _ := strconv.Atoi(m[1])
			return Size{Share: float64(n) / 100}, sizeShare
		}
	}
	p.fail(`%s: %s; an integer or a share like "25%%"`, key, show(v))
	return Size{}, sizeUnset
}

// sizes checks the sizes' ranges: sidebar.width's, and details.size's
// for its place.
func (p *layoutParser) sizes() {
	l := p.l
	if p.width != sizeUnset {
		p.inRange("sidebar.width", l.Width, p.width, 14, 200, 10, 60, "")
	}
	if p.detSize == sizeUnset {
		return
	}
	switch l.Details {
	case "off":
		p.fail("details.size has no effect when place is off")
	case "bottom":
		p.inRange("details.size", l.DetSize, p.detSize, 6, 100, 20, 70, " rows")
	default:
		p.inRange("details.size", l.DetSize, p.detSize, 24, 200, 20, 70, "")
	}
}

// inRange checks a size written as how: cells (or rows, unit) lo to
// hi, or a share plo% to phi%.
func (p *layoutParser) inRange(key string, s Size, how, lo, hi, plo, phi int, unit string) {
	if how == sizeShare {
		if pc := int(s.Share*100 + 0.5); pc < plo || pc > phi {
			p.fail("%s: \"%d%%\"; %d%% to %d%%", key, pc, plo, phi)
		}
		return
	}
	switch {
	case s.Cells < lo:
		p.fail("%s: %d; at least %d%s", key, s.Cells, lo, unit)
	case s.Cells > hi:
		p.fail("%s: %d; at most %d%s", key, s.Cells, hi, unit)
	}
}

// sections checks dashboard.sections: known ids, each once.
func (p *layoutParser) sections(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		p.fail(`dashboard.sections: a list of sections, ["needs", …]`)
		return nil
	}
	out := make([]string, 0, len(arr))
	seen := map[string]bool{}
	for _, e := range arr {
		s, ok := e.(string)
		switch {
		case !ok:
			p.fail("dashboard.sections: sections are strings")
			return nil
		case !slices.Contains(LayoutSections, s):
			p.fail("dashboard.sections: no section %q (%s)", s, strings.Join(LayoutSections, ", "))
		case seen[s]:
			p.fail("dashboard.sections: %s is listed twice", s)
		default:
			out = append(out, s)
		}
		seen[s] = true
	}
	return out
}

// sideSections checks sidebar.sections (band, which may hold the tree
// marker once) or sidebar.project_sections: mods' section ids,
// "<mod>.<view>", each once, at most MaxLayoutSections. Whether the mod
// is there is the console's to see: an id it doesn't know is skipped.
func (p *layoutParser) sideSections(key string, v any, band bool) []string {
	want := `["<mod>.<view>", …]`
	if band {
		want = `["<mod>.<view>", "tree", …]`
	}
	arr, ok := v.([]any)
	if !ok {
		p.fail("%s: a list of sections, %s", key, want)
		return nil
	}
	if len(arr) > MaxLayoutSections {
		p.fail("%s: %d sections, at most %d", key, len(arr), MaxLayoutSections)
	}
	out := make([]string, 0, len(arr))
	seen := map[string]bool{}
	for _, e := range arr {
		s, ok := e.(string)
		switch {
		case !ok:
			p.fail("%s: sections are strings", key)
			return nil
		case s == SectionTree && !band:
			p.fail(`%s: "tree" marks the tree in sidebar.sections; the sections under each project are always below its threads`, key)
		case seen[s] && s == SectionTree:
			p.fail("%s: %q may appear once", key, s)
		case seen[s]:
			p.fail("%s: %s is listed twice", key, s)
		case s != SectionTree && !view.IsModColumn(s):
			p.fail("%s: no section %q (a mod's: <mod>.<view>)", key, s)
		default:
			out = append(out, s)
		}
		seen[s] = true
	}
	return out
}

// SplitSections is a band order (sidebar.sections) as the ids above the
// tree and those below it: all below without the tree marker.
func SplitSections(order []string) (above, below []string) {
	i := slices.Index(order, SectionTree)
	if i < 0 {
		return nil, order
	}
	return order[:i], order[i+1:]
}

// show is a value as a layout file writes it, for an error.
func show(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(v)
}
