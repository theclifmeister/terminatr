package link

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// The program channel (the programs design §8, §10): messages between a
// program's director and its member projects' coordinators. It has no
// link in config.toml: membership is the link, which the caller checks
// (program.Send). The files are a link's, in messages/<other>/ of both
// folders (a program's slug is never a project's, §3.4), so a child
// coordinator reads one with tm link read; the breaker and the rate of
// links apply, but a message over the rate is refused, not queued.

// KindProgramMessage is the inbox item of a program channel message:
// fixed words, never the text.
const KindProgramMessage = "program-message"

// SendProgram sends o's text from folder from to folder to, one a
// program and the other a member project of it, as c (the director, a
// child coordinator, or the human for either). The caller has checked
// that c may: membership and the switch are the program package's.
func SendProgram(c caller.Caller, from, to *project.Folder, o Opts) (*Message, error) {
	if c.Kind == caller.Thread {
		return nil, refuse("coordinator-only", "the program channel is the coordinators' and the director's")
	}
	if from.IsProgram() == to.IsProgram() {
		return nil, refuse("not-member", "the program channel joins a program and its member projects")
	}
	if len(o.Attach) > 0 || o.File != "" {
		return nil, refuse("bad-attachment", "program messages carry text only: cite the file instead")
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	text := Clean(o.Text)
	if text == "" {
		return nil, refuse("empty", "the message has no text")
	}
	child := to
	if !from.IsProgram() {
		child = from
	}
	l := limitsOf(cfg, child.Slug)
	if len(text) > l.maxText {
		return nil, refuse("too-long", "message is %s, at most %s: shorten it", KB(len(text)), KB(l.maxText))
	}
	if o.Re != "" {
		if _, err := os.Stat(path(from, to.Slug, o.Re)); !ValidID(o.Re) || err != nil {
			return nil, refuse("unknown-message", "no message %s between %s and %s", o.Re, from.Slug, to.Slug)
		}
	}
	m := &Message{Out: true, Created: now().UTC().Truncate(time.Second), Re: o.Re, Bytes: len(text), By: programBy(c), Peer: to.Slug, Text: text}
	err = withStore(func(s *store) error {
		if err := s.allow(from.Slug, to.Slug); err != nil {
			return err
		}
		t := now()
		if s.over(from.Slug, to.Slug, l, t) {
			return refuse("rate-limited", "%s has sent its %d messages an hour to %s: wait, or say it in one message", from.Slug, l.perHour, to.Slug)
		}
		m.ID = s.nextID()
		m.State = StateSent
		if err := write(from, m); err != nil {
			return err
		}
		in := *m
		in.Out, in.Peer = false, from.Slug
		if err := write(to, &in); err != nil {
			return err
		}
		if _, err := to.AddMessageItem(KindProgramMessage, from.Slug, ProgramSummary(from, m), false, m.ID); err != nil {
			return err
		}
		detail := fmt.Sprintf("%s (%s)", m.ID, KB(m.Bytes))
		from.Journal(c, "program.send", to.Slug, detail)
		to.Journal(c, "program.receive", from.Slug, detail)
		s.account(c.IsAgent(), from.Slug, to.Slug, l, t, side{from, to.Slug}, side{to, from.Slug})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// programBy is who sent a program channel message: the director, a
// coordinator, or the human.
func programBy(c caller.Caller) string {
	switch {
	case c.Kind == caller.Director:
		return "director"
	case c.IsAgent():
		return "coordinator"
	}
	return "human"
}

// ProgramSummary is a program-message item's summary, as folder from
// sent m: fixed words, never the text.
func ProgramSummary(from *project.Folder, m *Message) string {
	who := from.Slug + "'s coordinator"
	if from.IsProgram() {
		who = "the director of " + from.Slug
	}
	if m.By == "human" {
		who = "the user, for " + from.Slug
	}
	extra := KB(m.Bytes)
	if m.Re != "" {
		extra += ", re " + m.Re
	}
	how := "tm link read " + m.ID
	if !from.IsProgram() {
		how = "tm program read " + m.ID
	}
	return fmt.Sprintf("%s wrote %s (%s); read it with %s", who, m.ID, extra, how)
}

// programHandled marks a program channel message read on both sides,
// once p handled its item.
func programHandled(p Box, it project.Item) {
	if !ValidID(it.Message) || !project.ValidSlug(it.Subject) {
		return
	}
	read := func(x *Message) {
		if x.State == StateSent {
			x.State = StateRead
		}
	}
	update(p, it.Subject, it.Message, read)
	var other Box
	if p.IsProgram() {
		if o, err := project.Open(it.Subject); err == nil {
			other = o
		}
	} else if g := programFolder(it.Subject); g != nil {
		other = g
	}
	if other != nil {
		update(other, p.Name(), it.Message, read)
	}
}

// programFolder is program slug's folder here, nil when it has none.
func programFolder(slug string) *project.Folder {
	if !project.ValidSlug(slug) {
		return nil
	}
	root, err := home.ProgramsDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(root, slug)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	return &project.Folder{Slug: slug, Dir: dir, Kind: project.KindProgram}
}

// senderKind is what the link to message m's sender, in p's copy,
// points at: a program's note when a program sent it, else a project's.
func senderKind(p Box, m *Message) vaultlink.Kind {
	if m.Out && p.IsProgram() || !m.Out && !p.IsProgram() && programFolder(m.Peer) != nil {
		return vaultlink.Program
	}
	return vaultlink.Project
}
