package server

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestDeviceConn: a device's connection, the server's own pipe, is the
// human's on the human socket with the device as its caller: its acts
// are journaled "human (iphone: <name>)", and it never answers a prompt
// that runs a command outside the sandbox.
func TestDeviceConn(t *testing.T) {
	p := testPaths(t)
	got := make(chan *Server, 1)
	onRun = func(s *Server) { got <- s }
	t.Cleanup(func() { onRun = nil })
	startServer(t, p)
	s := <-got
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Review me"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().SetStatus(human, 1, tasks.Review, ""); err != nil {
		t.Fatal(err)
	}
	c, err := s.deviceConn("Sam's iPhone")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var who proto.CallerInfo
	if err := c.Call(proto.MethodCallerWho, nil, &who); err != nil || who.Kind != string(caller.Human) {
		t.Fatalf("caller.who: %+v %v", who, err)
	}
	var res proto.TaskAskResult
	if err := c.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "T1", Kind: project.KindAccept}, &res); err != nil || !res.Asked {
		t.Fatalf("task.ask: %+v %v", res, err)
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	if !strings.Contains(string(journal), " human (iphone: Sam's iPhone) task.accept.ask T1") {
		t.Errorf("journal:\n%s", journal)
	}
	if got := (callCtx{device: "Sam's iPhone"}).console(); got != "device:Sam's iPhone" {
		t.Errorf("console %q", got)
	}
}

// A device's caller is the human's, named for the journal, and no
// agent: the human's gates take it; its level is its app API's to keep.
func TestDeviceCaller(t *testing.T) {
	d := caller.ForDevice("iPhone")
	if d.Kind != caller.Human || d.String() != "human (iphone: iPhone)" || d.IsAgent() || d.Device != "iPhone" {
		t.Fatalf("device caller %+v %q", d, d.String())
	}
}
