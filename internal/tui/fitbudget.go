package tui

import "github.com/theclifmeister/terminatr/internal/modview"

// fitBudget is the budget the sidebar's band (fitBand) and a panel
// (fitPanel) share: per section whether its header shows, its body
// lines, whether it gave its body up for room (shrunk), and how many
// headers didn't fit (more). order is the order the sections give way
// in, and keep those whose header gives way last (the cursor's, the
// selected); with keepBody, a kept section's body gives way when one
// after it is dropped.
type fitBudget struct {
	show, shrunk []bool
	body         []int
	more         int
	order        []int
	keep         []bool
	keepBody     bool
}

// newFitBudget is the budget of n sections, none shown yet.
func newFitBudget(n int) fitBudget {
	return fitBudget{show: make([]bool, n), shrunk: make([]bool, n), body: make([]int, n), keep: make([]bool, n)}
}

// ask shows section j with its body of want lines, at most
// modview.MaxSidebarHeight.
func (f *fitBudget) ask(j, want int) {
	f.show[j], f.body[j] = true, min(max(want, 0), modview.MaxSidebarHeight)
}

// giveOrder sets the order the sections give way in: those of each
// group in turn, each group's from its last.
func (f *fitBudget) giveOrder(groups ...func(int) bool) {
	f.order = nil
	for _, in := range groups {
		for j := len(f.show) - 1; j >= 0; j-- {
			if in(j) {
				f.order = append(f.order, j)
			}
		}
	}
}

func (f fitBudget) headers() int {
	n := 0
	for _, on := range f.show {
		if on {
			n++
		}
	}
	return n
}

// shrink folds the body of the first section in in that has one.
func (f *fitBudget) shrink(in func(int) bool) bool {
	for _, j := range f.order {
		if in(j) && f.show[j] && f.body[j] > 0 {
			f.body[j], f.shrunk[j] = 0, true
			return true
		}
	}
	return false
}

// drop hides the first section in in that shows, a kept one only with
// kept.
func (f *fitBudget) drop(kept bool, in func(int) bool) bool {
	for _, j := range f.order {
		switch {
		case !in(j) || !f.show[j]:
		case kept || !f.keep[j]:
			f.show[j], f.body[j] = false, 0
			return true
		case f.keepBody && f.body[j] > 0:
			// The kept header stays; its body gives way first.
			f.body[j], f.shrunk[j] = 0, true
		}
	}
	return false
}

// give takes one step of room from the sections in in: a body, else a
// header, a kept one last.
func (f *fitBudget) give(in func(int) bool) bool {
	return f.shrink(in) || f.drop(false, in) || f.drop(true, in)
}

// tally counts the headers that don't show (more); none of them is
// shrunk.
func (f *fitBudget) tally() {
	for j, on := range f.show {
		if !on {
			f.more++
			f.shrunk[j] = false
		}
	}
}

// anySection is every section.
func anySection(int) bool { return true }
