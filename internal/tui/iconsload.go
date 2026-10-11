package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"
)

// Icon sets from TOML (docs/SPEC.md §4, mods design §2 tier 1): a file
// with the fields of iconSet under the same names, plus name, base (the
// built-in set that fills every field the file leaves out: unicode,
// nerd or ascii, unicode when unset) and wide. The glyphs are checked
// by the rules TestIconWidths holds the built-in sets to.

// glyphs are the set's glyph fields by their TOML names.
func (s *iconSet) glyphs() map[string]*string {
	return map[string]*string{
		"folder": &s.folder, "folderOpen": &s.folderOpen, "mid": &s.mid, "end": &s.end,
		"coord": &s.coord, "thread": &s.thread,
		"hint": &s.hint, "current": &s.current, "remote": &s.remote, "paused": &s.paused, "inactive": &s.inactive,
		"working": &s.working, "blocked": &s.blocked, "idle": &s.idle, "starting": &s.starting, "running": &s.running,
		"review": &s.review, "done": &s.done, "other": &s.other, "none": &s.none,
		"ciPass": &s.ciPass, "ciFail": &s.ciFail, "ciRun": &s.ciRun,
		"fold": &s.fold, "unfold": &s.unfold,
		"todoDone": &s.todoDone, "todoNow": &s.todoNow, "todoOpen": &s.todoOpen,
		"barOn": &s.barOn, "barOff": &s.barOff,
	}
}

// builtinIcons is the built-in set called name, nil if none is.
func builtinIcons(name string) *iconSet {
	switch name {
	case IconsUnicode:
		return &unicodeIcons
	case IconsNerd:
		return &nerdIcons
	case IconsASCII:
		return &asciiIcons
	}
	return nil
}

// validateIcons reports every glyph of s that breaks the rules: one cell
// wide (a connector two), no emoji, and, for the built-in unicode and
// ascii sets and any set whose base says so, no Private Use Area glyph
// in a unicode set nor non-ASCII in an ascii one (pua: the set may use
// the Private Use Area, a Nerd Font's; ascii: it may not leave ASCII).
func validateIcons(s *iconSet, pua, ascii bool) []string {
	var errs []string
	g := s.glyphs()
	names := make([]string, 0, len(g))
	for n := range g {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		v := *g[n]
		if v == "" && (n == "coord" || n == "thread") {
			continue
		}
		want := 1
		if n == "mid" || n == "end" {
			want = 2
		}
		if w := ansi.StringWidth(v); w != want || len([]rune(v)) != want {
			errs = append(errs, fmt.Sprintf("%s %q is %d cells, want %d", n, v, ansi.StringWidth(v), want))
		}
		for _, r := range v {
			switch {
			case r >= 0x1F000 && r < 0xF0000:
				errs = append(errs, fmt.Sprintf("%s %q looks like an emoji", n, v))
			case !pua && unicode.In(r, unicode.Co):
				errs = append(errs, fmt.Sprintf("%s %q is in the Private Use Area", n, v))
			case ascii && r >= 0x80:
				errs = append(errs, fmt.Sprintf("%s %q isn't ASCII", n, v))
			}
		}
	}
	return errs
}

// parseIcons reads an icon set from TOML. Missing fields (and empty
// ones, but for coord and thread) fall back to the base set's; an
// unknown key, a wrong type or a glyph that breaks the width rules is an
// error naming every fault. A set based on nerd may use the Private Use
// Area; the others may not, and a set based on ascii must stay ASCII.
func parseIcons(data []byte) (*iconSet, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	base := IconsUnicode
	if v, ok := raw["base"]; ok {
		b, _ := v.(string)
		if builtinIcons(b) == nil {
			return nil, fmt.Errorf("base %v: want unicode, nerd or ascii", v)
		}
		base = b
	}
	set := *builtinIcons(base)
	set.name = base
	fields := set.glyphs()
	var errs []string
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		switch v := raw[k]; k {
		case "base":
		case "name":
			if s, ok := v.(string); ok && s != "" {
				set.name = s
			} else {
				errs = append(errs, "name: want a non-empty string")
			}
		case "wide":
			if b, ok := v.(bool); ok {
				set.wide = b
			} else {
				errs = append(errs, "wide: want true or false")
			}
		default:
			p, ok := fields[k]
			s, isStr := v.(string)
			switch {
			case !ok:
				errs = append(errs, fmt.Sprintf("unknown key %q", k))
			case !isStr:
				errs = append(errs, fmt.Sprintf("%s: want a string", k))
			case s != "" || k == "coord" || k == "thread":
				*p = s
			}
		}
	}
	errs = append(errs, validateIcons(&set, base == IconsNerd, base == IconsASCII)...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return &set, nil
}
