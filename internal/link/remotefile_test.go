package link

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// pipe is the data half between the test's two homes: a fetch on mini
// reads what studio's file host serves, and can drop, stall or corrupt
// the stream.
type pipe struct {
	sv      *servers
	cutAt   int64 // the link drops once this many bytes came; 0 never
	corrupt int   // fetches whose bytes are wrong
	abort   string
	served  []string // the codes the host's Close got
}

func (p *pipe) FileReady(string) bool { return true }

func (p *pipe) FetchFile(_ context.Context, _, id string, offset int64, w io.Writer) (peer.FileEnd, error) {
	var src peer.FileSource
	var err error
	p.sv.in(p.sv.studio, func() { src, err = Files().ServeFile(caller.ForPeer("mini"), id, offset) })
	if err != nil {
		return peer.FileEnd{}, err
	}
	code := ""
	defer func() { p.sv.in(p.sv.studio, func() { src.Close(code); p.served = append(p.served, code) }) }()
	if p.abort != "" {
		code = p.abort
		return peer.FileEnd{}, &peer.FileAbort{Code: p.abort}
	}
	bad := p.corrupt > 0
	p.corrupt--
	buf := make([]byte, 4096)
	for off := offset; off < src.Size(); {
		if p.cutAt > 0 && off >= p.cutAt {
			p.cutAt, code = 0, "link"
			return peer.FileEnd{}, errors.New("the link dropped")
		}
		n, _ := src.ReadAt(buf[:min(int64(len(buf)), src.Size()-off)], off)
		if bad && off == offset {
			buf[0] ^= 0xff
		}
		w.Write(buf[:n])
		off += int64(n)
		p.sv.in(p.sv.studio, func() { src.Acked(off) })
	}
	return peer.FileEnd{Size: src.Size(), Sum: src.Sum()}, nil
}

// remoteFile sends a 300 KB library file from acme to mini/api and
// delivers the message and its offer; it says the message, the bytes
// and the two projects.
func remoteFile(t *testing.T, sv *servers) (m *Message, data []byte, acme, api *project.Project, toMini, toStudio *wire) {
	t.Helper()
	toMini = &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}
	toStudio = &wire{sv: sv, to: sv.studio, alias: "mini", id: sv.mid, up: true}
	acme, api = sv.open(sv.studio, "acme"), sv.open(sv.mini, "api")
	sv.in(sv.studio, func() {
		data = libFile(t, acme, "t-0001", "load test.csv", 300<<10)
		var err error
		if m, err = Send(caller.Caller{Kind: caller.Coordinator, Project: "acme"}, acme, "mini/api", Opts{Text: "the load test", File: "t-0001/load test.csv"}); err != nil {
			t.Fatal(err)
		}
		if out := transfer(t, acme, m.Transfer); out.State != FileQueued || !strings.HasPrefix(m.Transfer, "X-"+sv.sid.Short()[:4]+"-") {
			t.Fatalf("out %+v", out)
		}
		Flush(context.Background(), toMini)
	})
	return
}

// TestRemoteFile: the offer follows its message; mini's user accepts,
// the answer goes back; the fetch drops halfway and resumes from the
// part; the file lands checked and the receipt marks studio's copy.
func TestRemoteFile(t *testing.T) {
	sv := setupServers(t)
	m, data, acme, api, _, toStudio := remoteFile(t, sv)
	var in Transfer
	sv.in(sv.mini, func() {
		in = transfer(t, api, m.Transfer)
		got, _ := Read(api, m.ID)
		if in.State != FileOffered || in.Peer != "studio/acme" || got.File != "load test.csv" || got.Transfer != m.Transfer {
			t.Fatalf("in %+v, message %+v", in, got)
		}
		its := items(t, api, KindFile)
		if len(its) != 1 || !its[0].NeedsUser || !strings.Contains(its[0].Summary, "studio/acme offers a file with "+m.ID+" (load test.csv, 300.0 KB)") {
			t.Fatalf("items %+v", its)
		}
		if _, err := AcceptFile(caller.Caller{Kind: caller.Coordinator, Project: "api"}, api, in.ID); code(err) != "human-only" {
			t.Fatalf("coordinator accepted: %v", err)
		}
		if _, err := AcceptFile(human, api, in.ID); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		if out := transfer(t, acme, m.Transfer); out.State != FileAccepted {
			t.Fatalf("out after the answer %+v", out)
		}
	})
	p := &pipe{sv: sv, cutAt: 128 << 10}
	sv.in(sv.mini, func() {
		fetch(context.Background(), p, transfer(t, api, m.Transfer))
		in = transfer(t, api, m.Transfer)
		part, _ := partPath(in.ID)
		fi, err := os.Stat(part)
		if in.State != FileWaiting || in.Wait != "link" || in.Offset != 128<<10 || err != nil || fi.Size() != 128<<10 {
			t.Fatalf("after the drop %+v (part %v)", in, err)
		}
		fetch(context.Background(), p, in)
		in = transfer(t, api, m.Transfer)
		if in.State != FileReceived {
			t.Fatalf("after the resume %+v", in)
		}
		landed := filepath.Join(fileDir(api, "studio/acme", m.ID), "load test.csv")
		if got, _ := os.ReadFile(landed); !bytes.Equal(got, data) {
			t.Fatal("the landed file isn't the one sent")
		}
		if _, err := os.Stat(part); err == nil {
			t.Error("the part is left")
		}
		Flush(context.Background(), toStudio)
	})
	if len(p.served) != 2 || p.served[0] != "link" || p.served[1] != "" {
		t.Errorf("served %q", p.served)
	}
	sv.in(sv.studio, func() {
		if out := transfer(t, acme, m.Transfer); out.State != FileReceived || out.Offset != out.Size {
			t.Fatalf("out after the receipt %+v", out)
		}
		if !strings.Contains(journal(acme), "link.file-received mini/api "+m.Transfer) {
			t.Errorf("journal:\n%s", journal(acme))
		}
	})
}

// TestRemoteFileBadContent: bytes that don't match start once more from
// the first; the second time it fails, and studio hears why.
func TestRemoteFileBadContent(t *testing.T) {
	sv := setupServers(t)
	m, _, acme, api, _, toStudio := remoteFile(t, sv)
	p := &pipe{sv: sv, corrupt: 2}
	sv.in(sv.mini, func() {
		AcceptFile(human, api, m.Transfer)
		fetch(context.Background(), p, transfer(t, api, m.Transfer))
		if in := transfer(t, api, m.Transfer); in.State != FileWaiting || !in.Retried || in.Offset != 0 {
			t.Fatalf("after one bad try %+v", in)
		}
		fetch(context.Background(), p, transfer(t, api, m.Transfer))
		if in := transfer(t, api, m.Transfer); in.State != FileFailed || in.Code != "bad-content" {
			t.Fatalf("after two %+v", in)
		}
		if _, err := os.Stat(filepath.Join(fileDir(api, "studio/acme", m.ID), "load test.csv")); err == nil {
			t.Error("a bad file landed")
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		out := transfer(t, acme, m.Transfer)
		its := items(t, acme, KindUndelivered)
		if out.State != FileFailed || len(its) != 1 || !strings.Contains(its[0].Summary, "your file load test.csv with "+m.ID+" to mini/api wasn't received: the bytes that came didn't match") {
			t.Fatalf("out %+v items %+v", out, its)
		}
	})
}

// TestRemoteFileStalls: a stall waits for the next try; MaxStalls in a
// row fail it.
func TestRemoteFileStalls(t *testing.T) {
	sv := setupServers(t)
	m, _, _, api, _, _ := remoteFile(t, sv)
	p := &pipe{sv: sv, abort: proto.FileStalled}
	sv.in(sv.mini, func() {
		AcceptFile(human, api, m.Transfer)
		for i := 1; i < MaxStalls; i++ {
			fetch(context.Background(), p, transfer(t, api, m.Transfer))
			if in := transfer(t, api, m.Transfer); in.State != FileWaiting || in.Stalls != i {
				t.Fatalf("stall %d: %+v", i, in)
			}
		}
		fetch(context.Background(), p, transfer(t, api, m.Transfer))
		if in := transfer(t, api, m.Transfer); in.State != FileFailed || in.Code != "stalled" {
			t.Fatalf("after %d stalls %+v", MaxStalls, in)
		}
	})
}

// TestRemoteFileCancel: the sender cancels an accepted file: the other
// side stops, drops its part and closes its ask; the fetch is refused.
func TestRemoteFileCancel(t *testing.T) {
	sv := setupServers(t)
	m, _, acme, api, toMini, _ := remoteFile(t, sv)
	p := &pipe{sv: sv, cutAt: 64 << 10}
	sv.in(sv.mini, func() {
		AcceptFile(human, api, m.Transfer)
		fetch(context.Background(), p, transfer(t, api, m.Transfer))
	})
	sv.in(sv.studio, func() {
		if _, err := CancelFile(caller.Caller{Kind: caller.Coordinator, Project: "acme"}, acme, m.Transfer); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		in := transfer(t, api, m.Transfer)
		part, _ := partPath(in.ID)
		if _, err := os.Stat(part); in.State != FileCancelled || err == nil {
			t.Fatalf("in %+v, part %v", in, err)
		}
		var pe *proto.Error
		if _, err := p.FetchFile(context.Background(), "studio", in.ID, 0, io.Discard); !errors.As(err, &pe) || pe.Code != proto.ErrUnknownFile {
			t.Fatalf("a cancelled file was served: %v", err)
		}
	})
}

// TestRemoteFileRefused: mini's side takes no files, or is a tm without
// files at all: studio's copy isn't delivered, in fixed words, and the
// message itself went.
func TestRemoteFileRefused(t *testing.T) {
	sv := setupServers(t)
	os.WriteFile(filepath.Join(sv.mini, "config.toml"), []byte("[projects.api.links]\n\"studio/acme\" = { allow = \"both\", files = \"off\" }\n"), 0o600)
	m, _, acme, _, _, _ := remoteFile(t, sv)
	sv.in(sv.studio, func() {
		got, _ := Read(acme, m.ID)
		its := items(t, acme, KindUndelivered)
		if got.State != StateSent || transfer(t, acme, m.Transfer).Code != "files-off" || len(its) != 1 || !strings.Contains(its[0].Summary, "mini/api takes no files from acme") {
			t.Fatalf("message %s, items %+v", got.State, its)
		}
		m2, err := Send(caller.Caller{Kind: caller.Coordinator, Project: "acme"}, acme, "mini/api", Opts{Text: "again", File: "t-0001/load test.csv"})
		if err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), oldServer{})
		its = items(t, acme, KindUndelivered)
		if out := transfer(t, acme, m2.Transfer); out.State != FileUndelivered || len(its) != 2 || !strings.Contains(its[1].Summary+its[0].Summary, "a tm before 0.47.0 doesn't take files") {
			t.Fatalf("an older server: %+v %+v", out, its)
		}
	})
}

// TestRemoteFileAuto: mini's side takes files without asking: no item
// asks, the answer goes back at once, and the received file gets an
// item that needs no one.
func TestRemoteFileAuto(t *testing.T) {
	sv := setupServers(t)
	os.WriteFile(filepath.Join(sv.mini, "config.toml"), []byte("[projects.api.links]\n\"studio/acme\" = { allow = \"both\", files = \"auto\" }\n"), 0o600)
	m, _, acme, api, _, toStudio := remoteFile(t, sv)
	sv.in(sv.mini, func() {
		if in := transfer(t, api, m.Transfer); in.State != FileAccepted || !in.Auto || len(items(t, api, KindFile)) != 0 {
			t.Fatalf("in %+v", in)
		}
		fetch(context.Background(), &pipe{sv: sv}, transfer(t, api, m.Transfer))
		its := items(t, api, KindFile)
		if len(its) != 1 || its[0].NeedsUser || !strings.Contains(its[0].Summary, "studio/acme's file load test.csv (300.0 KB) with "+m.ID+" was received") {
			t.Fatalf("items %+v", its)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		if out := transfer(t, acme, m.Transfer); out.State != FileReceived {
			t.Fatalf("out %+v", out)
		}
	})
}

// TestReceiveOfferChecks: what a paired server's offer must be.
func TestReceiveOfferChecks(t *testing.T) {
	sv := setupServers(t)
	m, _, _, api, _, _ := remoteFile(t, sv)
	fp := sv.sid.Short()[:4]
	good := proto.PeerMessage{Kind: proto.PeerFileOffer, From: "acme", To: "api", ID: "X-" + fp + "-90", Re: m.ID, By: "coordinator", Name: "a.txt", Size: 10, SHA256: strings.Repeat("ab", 32)}
	seq := 10
	sv.in(sv.mini, func() {
		for name, tc := range map[string]struct {
			edit func(*proto.PeerMessage)
			code string
		}{
			"dirty name":   {func(x *proto.PeerMessage) { x.Name = "../a.txt" }, "bad-message"},
			"hidden name":  {func(x *proto.PeerMessage) { x.Name = ".a" }, "bad-message"},
			"bad hash":     {func(x *proto.PeerMessage) { x.SHA256 = "zz" }, "bad-message"},
			"other server": {func(x *proto.PeerMessage) { x.ID = "X-0000-91" }, "bad-message"},
			"no message":   {func(x *proto.PeerMessage) { x.Re = "M-" + fp + "-77" }, "no-message"},
			"too big":      {func(x *proto.PeerMessage) { x.Size = 26 << 20 }, "too-big"},
			"not linked":   {func(x *proto.PeerMessage) { x.To = "nothere" }, "not-linked"},
		} {
			x := good
			tc.edit(&x)
			seq++
			x.Epoch, x.Seq = "e", seq
			if r := Receive("studio", sv.sid, x); r.Status != proto.PeerRefused || r.Code != tc.code {
				t.Errorf("%s: %+v", name, r)
			}
		}
		if n := len(Transfers(api, "")); n != 1 {
			t.Errorf("%d transfers after refusals", n)
		}
	})
}
