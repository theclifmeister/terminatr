package link

// The control half of a file to a paired server's project (docs/SPEC.md
// §16.13): the offer, the receiving user's answer, the receipt and a
// cancel are outbox entries (peer.message kinds file-offer, file-answer,
// file-done, file-cancel), in order behind the message they go with. A
// server that predates them refuses each as bad-message and still takes
// its seq, so what waits behind goes on.

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func (r remote) newFileID(s *store) string {
	s.Next++
	return fmt.Sprintf("X-%s-%d", r.fp, s.Next)
}

// queueOffer puts the offer of message m's file in the outbox, right
// behind m; the links' store is locked. A link whose files this side
// turned off since m was written ends the transfer undelivered.
func (r remote) queueOffer(s *store, from *project.Project, m *Message) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var t Transfer
	off := fileMode(cfg, from.Slug, m.Peer) == config.FilesOff
	withFiles(func(st *fileStore) error {
		x := st.find(from.Slug, m.Transfer)
		if x == nil || !x.Out || !x.Open() {
			return nil
		}
		if off {
			x.end(FileUndelivered, "files-off")
		} else {
			x.State = FileQueued
		}
		t = *x
		return nil
	})
	switch {
	case t.ID == "":
		return nil
	case off:
		fileUndeliveredItem(from, &t, "files-off")
		return nil
	}
	from.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-offer", m.Peer, fmt.Sprintf("%s (%s, queued)", t.ID, MB(t.Size)))
	return s.queue(r.alias, proto.PeerMessage{Kind: proto.PeerFileOffer, From: from.Slug, To: r.slug, ID: t.ID, Created: t.Created, Re: m.ID, By: t.By,
		Name: t.Name, Size: t.Size, SHA256: t.SHA256})
}

// shaRE is a SHA-256 in hex, as tm writes one.
var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// fileIDOf checks a transfer id from the server whose fingerprint is fp.
func fileIDOf(id, fp string) bool {
	g := fileIDRE.FindStringSubmatch(id)
	return g != nil && g[1] == fp
}

// receiveOffer takes the offer of a file with message m.Re from project
// m.From on the server alias for project m.To here; the links' store is
// locked. The checks are this side's own: the link, its files, its size
// and the name, which must be clean already.
func (s *store) receiveOffer(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if !fileIDOf(m.ID, fp) {
		return bad
	}
	if g := remoteIDRE.FindStringSubmatch(m.Re); g == nil || g[1] != fp {
		return bad
	}
	if (m.By != "coordinator" && m.By != "human") || m.Size < 0 || m.Name == "" || CleanName(m.Name) != m.Name || !shaRE.MatchString(m.SHA256) {
		return bad
	}
	key := alias + "/" + m.From
	notLinked := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-linked"}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	to, err := target(cfg, m.To)
	if err != nil {
		return notLinked
	}
	if links, _ := cfg.Links(to.Slug); !links[key].Receives() {
		return notLinked
	}
	mode := fileMode(cfg, to.Slug, key)
	switch {
	case mode == config.FilesOff:
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "files-off"}
	case s.Pairs[pairKey(key, to.Slug)] != nil && s.Pairs[pairKey(key, to.Slug)].Paused:
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "link-paused"}
	case m.Size > int64(fileMB(cfg, to.Slug))<<20:
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "too-big"}
	}
	if _, err := os.Stat(path(to, key, m.Re)); err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "no-message"}
	}
	dup := false
	readFiles(func(st *fileStore) { dup = st.find(to.Slug, m.ID) != nil })
	if dup {
		return proto.PeerMessageResult{Status: proto.PeerDuplicate}
	}
	t0 := now().UTC().Truncate(time.Second)
	in := &Transfer{ID: m.ID, Project: to.Slug, Peer: key, Message: m.Re, Name: m.Name, Size: m.Size, SHA256: m.SHA256, By: m.By, State: FileOffered, Created: t0}
	auto := mode == config.FilesAuto
	if auto {
		in.State, in.Accepted, in.Auto = FileAccepted, t0, true
	}
	if err := withFiles(func(st *fileStore) error { st.add(in); return nil }); err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	update(to, key, m.Re, func(x *Message) { x.File, x.FileSize, x.Transfer = in.Name, in.Size, in.ID })
	peerC := caller.ForPeer(alias)
	to.Journal(peerC, "link.file-offered", key, fmt.Sprintf("%s (%s)", in.ID, MB(in.Size)))
	if !auto {
		to.AddMessageItem(KindFile, key, FileSummary(in), true, in.ID)
		return proto.PeerMessageResult{Status: proto.PeerDelivered}
	}
	to.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-accept", key, in.ID+" (auto)")
	s.queue(alias, proto.PeerMessage{Kind: proto.PeerFileAnswer, From: to.Slug, To: m.From, ID: in.ID, By: "human", Allow: proto.FileAccepted})
	Kick()
	KickFiles()
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// outOf is project m.To's sending copy of transfer m.ID, to project
// m.From on the server alias; nil for none.
func outOf(st *fileStore, alias string, m proto.PeerMessage) *Transfer {
	t := st.find(m.To, m.ID)
	if t == nil || !t.Out || t.Peer != alias+"/"+m.From {
		return nil
	}
	return t
}

// receiveFileAnswer takes the receiving user's answer to a file this
// side offered. An answer to nothing here goes quietly.
func (s *store) receiveFileAnswer(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if m.Allow != proto.FileAccepted && m.Allow != proto.FileDeclined {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	var t Transfer
	withFiles(func(st *fileStore) error {
		x := outOf(st, alias, m)
		if x == nil || (x.State != FileOffered && x.State != FileQueued) {
			return nil
		}
		if m.Allow == proto.FileAccepted {
			x.State, x.Accepted = FileAccepted, now().UTC().Truncate(time.Second)
		} else {
			x.end(FileDeclined, "")
		}
		t = *x
		return nil
	})
	if t.ID == "" {
		return ok
	}
	p, err := project.Open(m.To)
	if err != nil {
		return ok
	}
	if t.State == FileDeclined {
		p.AddMessageItem(KindFile, t.Peer, fmt.Sprintf("%s declined your file %s with %s", Shown(t.Peer), t.Name, t.Message), false, t.ID)
		p.Journal(caller.ForPeer(alias), "link.file-declined", t.Peer, t.ID)
	} else {
		p.Journal(caller.ForPeer(alias), "link.file-accepted", t.Peer, t.ID)
	}
	return ok
}

// receiveFileDone takes the receipt of a file this side sent: in place
// there, or failed for good, which the sending project hears of.
func (s *store) receiveFileDone(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if m.Allow != proto.FileReceived && m.Allow != proto.FileFailed {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	code := m.Code
	if _, known := FileWords[code]; !known {
		code = "refused"
	}
	var t Transfer
	withFiles(func(st *fileStore) error {
		x := outOf(st, alias, m)
		if x == nil || !x.Open() {
			return nil
		}
		if m.Allow == proto.FileReceived {
			x.Offset = x.Size
			x.end(FileReceived, "")
		} else {
			x.end(FileFailed, code)
		}
		t = *x
		return nil
	})
	if t.ID == "" {
		return ok
	}
	abortStream(t.ID, "")
	p, err := project.Open(m.To)
	if err != nil {
		return ok
	}
	if t.State == FileReceived {
		p.Journal(caller.ForPeer(alias), "link.file-received", t.Peer, fmt.Sprintf("%s (%s)", t.ID, MB(t.Size)))
		return ok
	}
	fileUndeliveredItem(p, &t, code)
	return ok
}

// receiveFileCancel takes the other side's cancel of a transfer either
// way: it stops, its item is done and nothing partial is kept.
func (s *store) receiveFileCancel(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	var t Transfer
	withFiles(func(st *fileStore) error {
		x := st.find(m.To, m.ID)
		if x == nil || x.Peer != alias+"/"+m.From || !x.Open() {
			return nil
		}
		x.end(FileCancelled, "")
		t = *x
		return nil
	})
	if t.ID == "" {
		return ok
	}
	dropPart(t.ID)
	abortStream(t.ID, cancelledWord)
	if p, err := project.Open(m.To); err == nil {
		doneFile(p, t.ID)
		p.Journal(caller.ForPeer(alias), "link.file-cancelled", t.Peer, t.ID)
	}
	return ok
}

// fileSent marks the sending copy offered once the paired server took
// its offer.
func fileSent(alias string, m proto.PeerMessage) {
	if m.Kind != proto.PeerFileOffer || !project.ValidSlug(m.From) {
		return
	}
	if p, err := project.Open(m.From); err == nil {
		setOut(p, m.ID, FileOffered, FileQueued)
	}
}

// fileUnsent ends the sending copy of an offer the paired server
// refused, or didn't take within Expiry, and tells the sending project.
// An answer, receipt or cancel that isn't taken goes quietly.
func fileUnsent(alias string, m proto.PeerMessage, code string) {
	if m.Kind != proto.PeerFileOffer || !project.ValidSlug(m.From) {
		return
	}
	p, err := project.Open(m.From)
	if err != nil {
		return
	}
	var t Transfer
	withFiles(func(st *fileStore) error {
		if x := st.find(p.Slug, m.ID); x != nil && x.Out && x.Open() {
			x.end(FileUndelivered, code)
			t = *x
		}
		return nil
	})
	if t.ID != "" {
		fileUndeliveredItem(p, &t, code)
	}
}

// answerRemote answers transfer t offered to p from project slug on the
// server alias: this side's copy first, then the answer through the
// outbox. Accepted, the fetcher takes it from here; auto says no one was
// asked.
func answerRemote(c caller.Caller, p *project.Project, t *Transfer, alias, slug string, accept, auto bool) (*Transfer, error) {
	var out Transfer
	err := withStore(func(s *store) error {
		err := withFiles(func(st *fileStore) error {
			x := st.find(p.Slug, t.ID)
			if x == nil || x.Out || x.State != FileOffered {
				return refuse("not-offered", "%s was answered meanwhile", t.ID)
			}
			if accept {
				x.State, x.Accepted, x.Auto = FileAccepted, now().UTC().Truncate(time.Second), auto
			} else {
				x.end(FileDeclined, "")
			}
			out = *x
			return nil
		})
		if err != nil {
			return err
		}
		allow := proto.FileDeclined
		if accept {
			allow = proto.FileAccepted
		}
		return s.queue(alias, proto.PeerMessage{Kind: proto.PeerFileAnswer, From: p.Slug, To: slug, ID: t.ID, By: "human", Allow: allow})
	})
	if err != nil {
		return nil, err
	}
	doneFile(p, t.ID)
	if accept {
		how := t.ID
		if auto {
			how += " (auto)"
		}
		p.Journal(c, "link.file-accept", t.Peer, how)
		KickFiles()
	} else {
		p.Journal(c, "link.file-decline", t.Peer, t.ID)
	}
	Kick()
	return &out, nil
}

// queueFileMsg queues m for the server alias and wakes the flusher.
func queueFileMsg(alias string, m proto.PeerMessage) {
	if withStore(func(s *store) error { return s.queue(alias, m) }) == nil {
		Kick()
	}
}

func cancelMsg(from, to, id string) proto.PeerMessage {
	return proto.PeerMessage{Kind: proto.PeerFileCancel, From: from, To: to, ID: id}
}

// doneMsg is the receipt of transfer id: received when code is "", else
// failed with it.
func doneMsg(from, to, id, code string) proto.PeerMessage {
	m := proto.PeerMessage{Kind: proto.PeerFileDone, From: from, To: to, ID: id, Allow: proto.FileReceived}
	if code != "" {
		m.Allow, m.Code = proto.FileFailed, code
	}
	return m
}
