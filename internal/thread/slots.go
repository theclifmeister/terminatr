package thread

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// Slot waiters (docs/SPEC.md §9): a coordinator that tm thread start or
// tm task delegate refused at a cap (over-cap, over-global-cap) waits
// for a slot. The ticker tells the waiters, oldest first and one per
// free slot, with one slot-free item each (ticker/slots.go), and holds
// that slot for it SlotHold; its next start that passes the caps
// forgets it, as does the end of the hold, or a day.

// Waiter is one project's coordinator refused at a cap.
type Waiter struct {
	Project string `json:"project"`
	// Code is the refusal: over-cap or over-global-cap.
	Code string `json:"code"`
	// At is the first refusal, its place in line; Last the latest,
	// What what it wanted to start then (a task ref and title, or a
	// thread title).
	At   time.Time `json:"at"`
	Last time.Time `json:"last"`
	What string    `json:"what,omitempty"`
	// Told is when the ticker told it a slot freed: the slot is held for
	// it for SlotHold, so the next waiter isn't told about the same one.
	Told time.Time `json:"told,omitzero"`
}

// SlotHold is how long a slot a waiter was told of stays its own.
const SlotHold = 10 * time.Minute

// WaiterTTL is how long a waiter waits before it is forgotten.
const WaiterTTL = 24 * time.Hour

func slotsPath() (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "state", "slots.json"), nil
}

// Waiters reads the waiters, oldest first.
func Waiters() []Waiter {
	file, err := slotsPath()
	if err != nil {
		return nil
	}
	var ws []Waiter
	if b, err := os.ReadFile(file); err == nil {
		json.Unmarshal(b, &ws)
	}
	return ws
}

// UpdateWaiters rewrites the waiters with fn under the file's lock.
func UpdateWaiters(fn func([]Waiter) []Waiter) error {
	file, err := slotsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	unlock, err := mdfile.Lock(file)
	if err != nil {
		return err
	}
	defer unlock()
	var ws []Waiter
	if b, err := os.ReadFile(file); err == nil {
		json.Unmarshal(b, &ws)
	}
	ws = fn(ws)
	slices.SortStableFunc(ws, func(a, b Waiter) int { return a.At.Compare(b.At) })
	b, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(file, append(b, '\n'), 0o600)
}

// Refused records that slug's coordinator was refused at a cap (code)
// wanting to start what: one waiter per project, which keeps its place.
func Refused(slug, code, what string, now time.Time) error {
	return UpdateWaiters(func(ws []Waiter) []Waiter {
		for i := range ws {
			if ws[i].Project == slug {
				ws[i].Code, ws[i].Last, ws[i].What, ws[i].Told = code, now, what, time.Time{}
				return ws
			}
		}
		return append(ws, Waiter{Project: slug, Code: code, At: now, Last: now, What: what})
	})
}

// Started forgets slug's waiter: its coordinator got a slot.
func Started(slug string) error {
	if !slices.ContainsFunc(Waiters(), func(w Waiter) bool { return w.Project == slug }) {
		return nil
	}
	return UpdateWaiters(func(ws []Waiter) []Waiter {
		return slices.DeleteFunc(ws, func(w Waiter) bool { return w.Project == slug })
	})
}
