package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/qr"
)

// Pairing a phone from the Network popup (docs/SPEC.md §16.15): D makes
// a code through the relay the paired devices and servers use (asking
// for one when none is known), as tm device pair does, and shows it as
// a QR code drawn with tm's own encoder in half blocks, black on white
// whatever the terminal's theme, with its quiet zone, and as text under
// it to paste in the app. Once the phone joins it asks what tm device
// pair asks, Y adds it (E gives it another name first), and the popup
// closes with the outcome. esc cancels at any stage.
//
//	"█▀▀▀▀▀█ ▄▀ ▄ █▀▀▀▀▀█   Scan this with the terminatr app on the"
//	"█ ███ █ ▀█▄▀ █ ███ █   iPhone. It works once; you confirm the"
//	"…                      phone here before it is added."
//	""
//	"Or paste this code in the app: terminatr://pair?v=1&relay=…"

// phoneStage is where a phone's pairing is.
type phoneStage int

const (
	phoneDialing phoneStage = iota // making a code
	phoneWaiting                   // the code works: its QR code
	phoneOffer                     // the phone joined: Y or N
	phoneAdding                    // the user said yes
	phoneFailed                    // not paired: why
)

// phonePairView stands over the Network popup while a phone's pairing
// runs.
type phonePairView struct {
	// relay is the relay as typed, "" for the known one.
	relay  string
	stage  phoneStage
	invite DeviceInvite
	// code is the invite's QR code's lines, as drawn.
	code []string
	// offer is the phone that joined; name what it is added as, nameErr
	// why that name can't be.
	offer   DeviceOffer
	name    string
	nameErr string
	err     error
	cancel  context.CancelFunc
	answer  chan string
	events  chan phonePairMsg
}

// phonePairMsg is one step of a phone's pairing: the invite (it works
// now), the phone that joined, or the end.
type phonePairMsg struct {
	pv     *phonePairView
	invite *DeviceInvite
	offer  *DeviceOffer
	done   bool
	dev    DeviceEntry
	err    error
}

// pairPhone opens the phone's pairing popup and runs the pairing behind
// it, through relay ("" for the one the paired devices and servers
// use).
func (m *dash) pairPhone(relay string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	pv := &phonePairView{relay: relay, cancel: cancel, answer: make(chan string, 1), events: make(chan phonePairMsg, 3)}
	m.push(pv)
	ev := DevicePairEvents{
		Waiting: func(in DeviceInvite) { pv.events <- phonePairMsg{pv: pv, invite: &in} },
		Confirm: func(o DeviceOffer) string {
			pv.events <- phonePairMsg{pv: pv, offer: &o}
			select {
			case a := <-pv.answer:
				return a
			case <-ctx.Done():
				return ""
			}
		},
	}
	src := m.src
	go func() {
		defer cancel()
		d, err := src.PairDevice(ctx, relay, ev)
		pv.events <- phonePairMsg{pv: pv, done: true, dev: d, err: err}
	}()
	return pv.next()
}

// relayPrompt asks for the relay a phone's pairing goes through, when
// none is known yet.
func (m *dash) relayPrompt(relay string) {
	m.push(&inputView{title: "Pair a phone", lineEdit: newLine(relay), cancel: "not paired",
		label:       "The phone and this server meet on a relay, and none is set up yet. Its address (a host name, or wss://…; see Running a relay in docs/OPERATIONS.md):",
		placeholder: "relay.example.net",
		submit:      func(s string) tea.Cmd { return m.pairPhone(s) }})
}

// next waits for the pairing's next step.
func (pv *phonePairView) next() tea.Cmd {
	ch := pv.events
	return func() tea.Msg { return <-ch }
}

// step takes a pairing's step; one whose popup was closed is dropped,
// and the pairing cancelled.
func (pv *phonePairView) step(m *dash, msg phonePairMsg) tea.Cmd {
	if !slices.Contains(m.stack, overlay(pv)) {
		pv.cancel()
		return nil
	}
	switch {
	case msg.done && errors.Is(msg.err, errRelayNeeded):
		m.close(pv)
		m.relayPrompt("")
		return nil
	case msg.done && msg.err == nil:
		m.close(pv)
		d := msg.dev
		m.msg = fmt.Sprintf("paired %s (device id %s), %s; V on it sets its level", d.Name, d.Short, levelWord(d.Level))
		if !d.WriteKey {
			m.msg = fmt.Sprintf("paired %s (device id %s); its app has no write key, so it can only view", d.Name, d.Short)
		}
		if nv := m.network(); nv != nil {
			nv.load(m)
			nv.folded["d:"] = false
			nv.sel, nv.panel = "d:"+d.ID, false
		}
		return nil
	case msg.done:
		pv.stage, pv.err = phoneFailed, msg.err
		return nil
	case msg.offer != nil:
		pv.stage, pv.offer, pv.name, pv.nameErr = phoneOffer, *msg.offer, msg.offer.Name, ""
	default:
		pv.stage, pv.invite = phoneWaiting, *msg.invite
		pv.code = qrLines(msg.invite.Invite)
	}
	return pv.next()
}

func (pv *phonePairView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); {
	case s == "esc" || pv.stage == phoneOffer && s == "N":
		pv.cancel()
		m.close(pv)
		if pv.stage != phoneFailed {
			m.msg = "not paired"
		}
	case pv.stage == phoneOffer && s == "Y":
		if err := pv.offer.Check(pv.name); err != nil {
			pv.nameErr = err.Error()
			return nil
		}
		pv.answer <- pv.name
		pv.stage = phoneAdding
	case pv.stage == phoneOffer && s == "E":
		m.push(&inputView{title: "Add it as", lineEdit: newLine(pv.name), max: device.MaxName, cancel: "name unchanged",
			label: "The name this server knows the phone by; the journal names its acts so:",
			submit: func(s string) tea.Cmd {
				if err := device.CheckName(s); err != nil {
					pv.nameErr = err.Error()
					return nil
				}
				pv.name, pv.nameErr = s, ""
				return nil
			}})
	case pv.stage == phoneFailed && s == "enter":
		m.close(pv)
		return m.pairPhone(pv.relay)
	}
	return nil
}

// qrLines are text's QR code as the popup draws it: two rows of modules
// to a line in half blocks with the quiet zone, black on white whatever
// the terminal's theme; under NO_COLOR, the light modules in the text's
// colour, for light text on a dark background. nil when it can't be
// drawn.
func qrLines(text string) []string {
	c, err := qr.Encode([]byte(text))
	if err != nil {
		return nil
	}
	plain := os.Getenv("NO_COLOR") != ""
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(c.Terminal(!plain)), "\n"), "\n")
	if plain {
		return lines
	}
	for i, l := range lines {
		lines[i] = qrLook.Render(l)
	}
	return lines
}

// countdown is what is left until end, as m:ss; 0:00 once it passed.
func countdown(end time.Time) string {
	left := max(time.Until(end), 0).Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(left.Minutes()), int(left.Seconds())%60)
}

func (pv *phonePairView) render(m *dash) string {
	w := m.inner(viewWidth)
	b := box{title: "Pair a phone", sel: -1, keys: "esc cancel"}
	var body []string
	switch pv.stage {
	case phoneDialing:
		body = wrapLines("Making a code…", w)
	case phoneWaiting:
		b.keys = "esc cancel"
		body = pv.waiting(m, w, m.boxRows(b))
	case phoneOffer, phoneAdding:
		o := pv.offer
		body = []string{th().Head.Render("A phone joined")}
		version := o.Version
		if version == "" {
			version = "unknown"
		}
		body = append(body, wrapLines(fmt.Sprintf("%q, device id %s, app %s", oneLine(o.Phone), o.Short, oneLine(version)), w)...)
		if o.Again != "" {
			body = append(body, wrapLines(fmt.Sprintf("This phone is paired already, as %q: pairing again keeps its level.", o.Again), w)...)
		}
		if !o.WriteKey {
			body = append(body, wrapLines("Its app has no write key: it can only view, whatever its level.", w)...)
		}
		body = append(body, "")
		a := "a view-only"
		if o.Level != device.LevelView {
			a = "a" + map[bool]string{true: "n", false: ""}[o.Level == device.LevelAnswer] + " " + o.Level
		}
		body = append(body, wrapLines(fmt.Sprintf("Add it as %q, %s device?", pv.name, a), w)...)
		if pv.nameErr != "" {
			for _, l := range wrapLines(pv.nameErr, w) {
				body = append(body, th().Bad.Render(l))
			}
		}
		if pv.stage == phoneOffer {
			body = append(body, faintLines("Only a phone you just scanned the code with. Answer within "+countdown(o.AnswerBy)+".", w)...)
			b.keys = "Y add · N don't add · E another name · esc cancel"
		} else {
			body = append(body, "", "Adding it…")
		}
	case phoneFailed:
		for _, l := range wrapLines("Not paired: "+strings.TrimPrefix(oneLine(pv.err.Error()), "not paired: "), w) {
			body = append(body, th().Bad.Render(l))
		}
		b.keys = "enter new code · esc close"
	}
	b.body = body
	return m.popup(b)
}

// waiting is the waiting stage's n lines w cells wide: the QR code with
// the words beside it when there is room, else under it, and the code
// as text under both; without the rows for the QR code, the words and
// the text, and how tall the window needs to be.
func (pv *phonePairView) waiting(m *dash, w, n int) []string {
	in := pv.invite
	words := []string{"Scan this with the terminatr app on the iPhone. It works once; you confirm the phone here before it is added."}
	if time.Now().Before(in.Expires) {
		words = append(words, "Works for "+countdown(in.Expires)+" · waiting for the phone…")
	} else {
		words = append(words, "The code's time ran out.")
	}
	words = append(words, fmt.Sprintf("This server: %s, id %s.", in.Name, in.Server), "Through "+in.Relay+".")
	if in.Note != "" {
		words = append(words, "Note: "+in.Note+".")
	}
	code := []string{th().Faint.Render("Or paste this code in the app:")}
	for _, l := range wrapInput("", in.Invite, max(w, 8)) {
		code = append(code, th().Accent.Render(l))
	}
	qw := 0
	if len(pv.code) > 0 {
		qw = ansi.StringWidth(pv.code[0])
	}
	text := func(width int) []string {
		// A cell spare: a wrap after a short id's hyphen keeps it.
		width--
		var out []string
		for i, p := range words {
			if i > 0 {
				out = append(out, "")
			}
			if i == 0 {
				out = append(out, wrapLines(p, width)...)
			} else {
				out = append(out, faintLines(p, width)...)
			}
		}
		return out
	}
	side := w - qw - 3
	var out []string
	switch {
	case qw > 0 && side >= 28 && len(pv.code)+1+len(code) <= n:
		t := text(side)
		for i := range max(len(pv.code), len(t)) {
			l := strings.Repeat(" ", qw)
			if i < len(pv.code) {
				l = pv.code[i]
			}
			if i < len(t) {
				l += "   " + t[i]
			}
			out = append(out, l)
		}
	case qw > 0 && qw <= w && len(pv.code)+1+len(text(w))+1+len(code) <= n:
		out = append(append(append(out, pv.code...), ""), text(w)...)
	default:
		out = text(w)
		need := m.h - n + len(pv.code) + 1 + len(code)
		if qw > 0 {
			out = append(out, "", th().Warn.Render(fmt.Sprintf("The QR code needs a window at least %d wide and %d tall: make it bigger, or paste the code.", qw+4+2, need)))
		}
	}
	return append(append(out, ""), code...)
}
