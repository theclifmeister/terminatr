package view

import (
	"fmt"
	"math"
)

// Sidebar is the projects sidebar's layout (docs/SPEC.md §4): part of
// the view, so every console shows it alike. internal/tui draws it.
type Sidebar struct {
	// Width is the full sidebar's width in columns, its border included.
	Width int `json:"width"`
	// Slim keeps the slim strip (glyphs and short names) in any window.
	Slim bool `json:"slim"`
	// Side is the window's edge it is on: "" (left) or right.
	Side string `json:"side,omitempty"`
	// Share is the full sidebar's width as a share of the window, 0 for
	// Width: each window makes it cells of its own.
	Share float64 `json:"share,omitempty"`
	// Hidden is no sidebar at all (0 columns): b brings it back.
	Hidden bool `json:"hidden,omitempty"`
}

const (
	SideDefault = 36 // a full sidebar's width: room for a thread's id and a few words of its title beside the 17-cell grid
	SideMin     = 14 // no maximum: only the window bounds it (SideRoom)
	SideSlim    = 11 // the slim strip: the gutter, the state column (2), 5 letters (or 4 and …), the needs-you column (2), the border
	SideRoom    = 60 // a full sidebar leaves the panes at least this many columns
	SideStep    = 2  // { and } change the width this much

	ShareMin = 0.10 // a width given as a share of the window: layout.toml's "10%" to "60%"
	ShareMax = 0.60

	SideLeft  = "left"
	SideRight = "right"
)

// Valid checks a sidebar from the wire: its side and its share.
func (s Sidebar) Valid() error {
	if s.Side != "" && s.Side != SideLeft && s.Side != SideRight {
		return fmt.Errorf("sidebar side %q: left or right", s.Side)
	}
	if s.Share != 0 && (math.IsNaN(s.Share) || s.Share < ShareMin || s.Share > ShareMax) {
		return fmt.Errorf("sidebar share %v: %v to %v", s.Share, ShareMin, ShareMax)
	}
	return nil
}

// Clamp keeps the width in range; 0 is the default. There is no upper
// bound: a width too wide for the window is shown narrower (Cols) and
// kept, so it comes back in a wider window. The side is "" or right, a
// share in range or 0, and a hidden sidebar isn't slim too.
func (s Sidebar) Clamp() Sidebar {
	if s.Width == 0 {
		s.Width = SideDefault
	}
	s.Width = max(s.Width, SideMin)
	if s.Side != SideRight {
		s.Side = ""
	}
	if s.Share != 0 {
		if math.IsNaN(s.Share) {
			s.Share = 0
		} else {
			s.Share = math.Round(min(max(s.Share, ShareMin), ShareMax)*100) / 100
		}
	}
	if s.Hidden {
		s.Slim = false
	}
	return s
}

// Right says whether the sidebar is on the window's right edge.
func (s Sidebar) Right() bool { return s.Side == SideRight }

// width is the full sidebar's width in a window w wide: Width, or the
// share of w, at least SideMin.
func (s Sidebar) width(w int) int {
	if s.Share > 0 {
		return max(int(s.Share*float64(w)+0.5), SideMin)
	}
	return s.Clamp().Width
}

// Cols is the sidebar's width in a window w columns wide: 0 when hidden,
// else the full width, cut to leave the panes SideRoom, or the slim strip
// when asked for or when the window is narrow. A width wider than the
// default shrinks down to the default before the strip takes over.
func (s Sidebar) Cols(w int) int {
	if s.Hidden {
		return 0
	}
	if s.Slim || w-SideRoom < s.least(w) {
		return min(SideSlim, max(w-1, 1))
	}
	return min(s.width(w), w-SideRoom)
}

// least is the narrowest the full sidebar shrinks to in a narrow window.
func (s Sidebar) least(w int) int { return min(s.width(w), SideDefault) }

// Full says whether the sidebar is at its full width in a window w wide.
func (s Sidebar) Full(w int) bool { return s.Cols(w) > SideSlim }

// Key applies a sidebar key ({ narrower, } wider, b the next of hidden,
// slim strip, full) to s in a window w wide; msg says why nothing
// changed.
func (s Sidebar) Key(key string, w int) (out Sidebar, msg string) {
	s = s.Clamp()
	narrow := fmt.Sprintf("the window is too narrow for the full sidebar (it needs %d columns)", s.least(w)+SideRoom)
	switch key {
	case "b":
		switch {
		case s.Hidden:
			s.Hidden, s.Slim = false, true
		case s.Slim:
			s.Slim = false
			if !s.Full(w) {
				msg = narrow
			}
		default:
			s.Hidden = true
		}
		return s, msg
	case "{", "}":
		if s.Hidden || s.Slim || !s.Full(w) {
			s.Hidden, s.Slim = false, false
			if !s.Full(w) {
				return s, narrow
			}
			return s, ""
		}
		d := SideStep
		if key == "{" {
			d = -d
		}
		// Step from the width shown, never wider than the window allows
		// (the panes keep SideRoom); at that limit } keeps a wider saved
		// width as it is. The width is cells from then on.
		shown, most := s.Cols(w), max(w-SideRoom, SideMin)
		if d > 0 && shown >= most {
			return s, ""
		}
		s.Width, s.Share = min(max(shown+d, SideMin), most), 0
	}
	return s, ""
}

// DragTo is s with its border dragged to column x of a window w wide:
// the border is the sidebar's last column on the left, its first on the
// right.
func (s Sidebar) DragTo(x, w int) Sidebar {
	if s.Right() {
		x = w - 1 - x
	}
	s.Hidden = false
	s.Slim = x+1 <= SideSlim
	if !s.Slim {
		s.Width, s.Share = min(max(x+1, SideMin), max(w-SideRoom, SideMin)), 0
	}
	return s.Clamp()
}
