package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Pairing from the Peers tab (docs/SPEC.md §16.8): n asks for the relay
// (the last one used to start with), shows the one-time code once it
// works and waits for the other server; j asks for a code. Both then
// show the other server and the six digits, and pin it on y, as tm peer
// pair and tm peer join do (Source.PairPeer, JoinPeer). The code shows
// in the popup only: never in the footer, an error or a log.
//
//	"Pairing code (works once, for 10 minutes; 9:41 left):"
//	""
//	"    relay.example.net/7Q4M-K2XD-9HVA-PT3C"
//	""
//	"On the other server: , then the Peers tab, j, and type it in;"
//	"or tm peer join CODE. Pass it on yourself (a chat, a call)."
//	"Waiting for the other server…"

// pairStage is where a pairing is.
type pairStage int

const (
	pairDialing  pairStage = iota // reaching the relay
	pairWaiting                   // the code works: waiting for the other side
	pairCheck                     // the six digits: y or n
	pairAnswered                  // waiting for the other side's answer
	pairFailed                    // not paired: why
)

// codeLife is how long a pairing code works (the relay's pair room).
const codeLife = 10 * time.Minute

// pairView stands over the Peers tab while a pairing runs. esc cancels
// it at any stage.
type pairView struct {
	join bool
	// input is the relay or code as typed: enter after a failure asks
	// for it again, with this to start from.
	input  string
	stage  pairStage
	code   string
	since  time.Time
	offer  PairOffer
	err    error
	cancel context.CancelFunc
	answer chan bool
	events chan pairMsg
}

// pairMsg is one step of a pairing: the code (it works now), the other
// server to check, or the end.
type pairMsg struct {
	pv    *pairView
	code  string
	offer *PairOffer
	done  bool
	alias string
	err   error
}

// pairPrompt asks for the relay to pair through, starting from relay.
func (m *dash) pairPrompt(relay string) {
	m.push(&inputView{title: "Pair a server", text: relay, cancel: "not paired",
		label:  "The relay both servers dial (a host name, or wss://…; see Running a relay in docs/OPERATIONS.md):",
		submit: func(s string) tea.Cmd { return m.startPairing(false, s) }})
}

// joinPrompt asks for the code the other server shows.
func (m *dash) joinPrompt(code string) {
	m.push(&inputView{title: "Join a server", text: code, cancel: "not paired",
		label:  "The pairing code the other server shows (relay.example.net/7Q4M-K2XD-9HVA-PT3C):",
		submit: func(s string) tea.Cmd { return m.startPairing(true, s) }})
}

// startPairing opens the pairing popup and runs the pairing behind it.
func (m *dash) startPairing(join bool, input string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	pv := &pairView{join: join, input: input, cancel: cancel,
		answer: make(chan bool, 1), events: make(chan pairMsg, 3)}
	m.push(pv)
	ev := PairEvents{
		Waiting: func(code string) { pv.events <- pairMsg{pv: pv, code: code} },
		Confirm: func(o PairOffer) bool {
			pv.events <- pairMsg{pv: pv, offer: &o}
			select {
			case a := <-pv.answer:
				return a
			case <-ctx.Done():
				return false
			}
		},
	}
	src := m.src
	go func() {
		defer cancel()
		var alias string
		var err error
		if join {
			alias, err = src.JoinPeer(ctx, input, ev)
		} else {
			alias, err = src.PairPeer(ctx, input, ev)
		}
		pv.events <- pairMsg{pv: pv, done: true, alias: alias, err: err}
	}()
	return pv.next()
}

// next waits for the pairing's next step.
func (pv *pairView) next() tea.Cmd {
	ch := pv.events
	return func() tea.Msg { return <-ch }
}

// step takes a pairing's step; one whose popup was closed is dropped,
// and the pairing cancelled.
func (pv *pairView) step(m *dash, msg pairMsg) tea.Cmd {
	if !slices.Contains(m.stack, overlay(pv)) {
		pv.cancel()
		return nil
	}
	switch {
	case msg.done && msg.err == nil:
		m.close(pv)
		m.msg = "paired with " + msg.alias + "; its link comes up in a moment"
		for _, o := range m.stack {
			if sv, ok := o.(*settingsView); ok {
				sv.peers.load(m)
				sv.peers.selectAlias(msg.alias)
			}
		}
		return m.load()
	case msg.done:
		pv.stage, pv.err = pairFailed, msg.err
		return nil
	case msg.offer != nil:
		pv.stage, pv.offer = pairCheck, *msg.offer
	default:
		pv.stage, pv.code, pv.since = pairWaiting, msg.code, time.Now()
	}
	return pv.next()
}

func (pv *pairView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); {
	case s == "esc":
		pv.cancel()
		m.close(pv)
		if pv.stage != pairFailed {
			m.msg = "not paired"
		}
	case pv.stage == pairCheck && (s == "y" || s == "n"):
		pv.answer <- s == "y"
		pv.stage = pairAnswered
	case pv.stage == pairFailed && s == "enter":
		m.close(pv)
		if pv.join {
			m.joinPrompt(pv.input)
		} else {
			m.pairPrompt(pv.input)
		}
	}
	return nil
}

func (pv *pairView) render(m *dash) string {
	w := m.inner(dialogWidth)
	title := "Pair a server"
	if pv.join {
		title = "Join a server"
	}
	var body []string
	keys := "esc cancel"
	switch pv.stage {
	case pairDialing:
		if pv.join {
			body = wrapLines("Joining through the relay…", w)
		} else {
			body = wrapLines("Opening a pairing on "+relayWords(pv.input)+"…", w)
		}
	case pairWaiting:
		left := max(codeLife-time.Since(pv.since), 0).Round(time.Second)
		body = wrapLines(fmt.Sprintf("Pairing code (works once, for 10 minutes; %d:%02d left):", int(left.Minutes()), int(left.Seconds())%60), w)
		body = append(body, "")
		for _, l := range wrapInput("", pv.code, max(w-4, 8)) {
			body = append(body, "    "+th().Accent.Render(l))
		}
		body = append(body, "")
		body = append(body, faintLines("On the other server: , then the Peers tab, j, and type it in; or tm peer join CODE. Pass it on yourself (a chat, a call), never through the relay.", w)...)
		body = append(body, wrapLines("Waiting for the other server…", w)...)
	case pairCheck, pairAnswered:
		name := pv.offer.Name
		if name == "" {
			name = "(no name)"
		}
		body = wrapLines("The other server: "+name, w)
		body = append(body, fmt.Sprintf("server id %s · tm %s", pv.offer.Short, strings.TrimPrefix(pv.offer.Version, "v")))
		body = append(body, "", "Check: "+th().Accent.Render(pv.offer.Check), "")
		if pv.stage == pairCheck {
			body = append(body, wrapLines("Pair only if the other side shows the same six digits. Do they match?", w)...)
			keys = "y they match · n they don't · esc cancel"
		} else {
			body = append(body, wrapLines("Waiting for the other side's answer…", w)...)
		}
	case pairFailed:
		for _, l := range wrapLines("Not paired: "+strings.TrimPrefix(oneLine(pv.err.Error()), "not paired: "), w) {
			body = append(body, th().Bad.Render(l))
		}
		keys = "enter try again · esc close"
	}
	return m.popup(box{title: title, body: body, sel: -1, keys: keys, dialog: true})
}
