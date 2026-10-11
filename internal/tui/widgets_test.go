package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/widget"
)

// TestWidgetLook: every widget icon has a glyph in every set (the tree's
// coordinator and thread marks may be none), and a tree drawn in each set
// fits the width at every width, Nerd Font icons with their blank.
func TestWidgetLook(t *testing.T) {
	defer icons.Store(ic())
	tree, err := widget.Decode([]byte(`{"kind":"stack","children":[
		{"kind":"list","id":"l","title":"States","rows":[` + iconRows() + `]},
		{"kind":"progress","label":"done","value":3,"max":7},
		{"kind":"steps","steps":[{"text":"a","state":"done"},{"text":"b","state":"now"},{"text":"c"}]},
		{"kind":"bars","values":[3,1,4,1,5,9,2,6],"spark":true},
		{"kind":"keys","keys":[{"key":"o","label":"open"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range []*iconSet{&unicodeIcons, &nerdIcons, &asciiIcons} {
		icons.Store(set)
		for _, name := range append(widget.Icons, "bar.on", "bar.off") {
			if widgetGlyph(set, name) == "" && name != "coordinator" && name != "thread" {
				t.Errorf("%s: no glyph for %s", set.name, name)
			}
		}
		if got := (widgetLook{}).ASCII(); got != (set == &asciiIcons) {
			t.Errorf("%s: ASCII() is %v", set.name, got)
		}
		for w := 1; w <= 60; w++ {
			v := widget.NewView()
			v.Set(tree)
			for _, s := range v.Render(w, 100, widgetLook{}) {
				if n := ansi.StringWidth(s); n > w {
					t.Fatalf("%s at %d: %q is %d wide", set.name, w, s, n)
				}
			}
		}
	}
	icons.Store(&nerdIcons)
	if g := (widgetLook{}).Icon("review"); g != nerdIcons.review+" " {
		t.Errorf("a nerd icon without its blank: %q", g)
	}
}

func iconRows() string {
	s := ""
	for i, name := range widget.Icons {
		if i > 0 {
			s += ","
		}
		s += `{"id":"` + name + `","spans":[{"icon":"` + name + `","role":"warn"},{"text":" ` + name + `"}]}`
	}
	return s
}
