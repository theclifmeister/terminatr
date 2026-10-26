package server

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestAppHelloJournal: a control hello that names the Mac app's bridge
// (proto.ClientApp) keeps the human's rights on the human socket, and
// what it does is journaled as "human (app)"; the same call without it,
// as "human". task.ask checks the live board.
func TestAppHelloJournal(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Ship it"}, {Title: "Review me"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().SetStatus(human, 2, tasks.Review, ""); err != nil {
		t.Fatal(err)
	}

	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	plain, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()

	var who proto.CallerInfo
	if err := app.Call(proto.MethodCallerWho, nil, &who); err != nil || who.Kind != string(caller.Human) {
		t.Fatalf("caller.who through the app: %+v %v", who, err)
	}
	var res proto.TaskAskResult
	if err := app.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "T2", Kind: project.KindAccept}, &res); err != nil || !res.Asked {
		t.Fatalf("accept T2: %+v %v", res, err)
	}
	if err := app.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "T2", Kind: project.KindAccept}, &res); err != nil || res.Asked {
		t.Fatalf("accept T2 again: %+v %v", res, err)
	}
	if err := plain.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "1", Kind: project.KindDelegate}, &res); err != nil || !res.Asked {
		t.Fatalf("delegate T1: %+v %v", res, err)
	}
	var perr *proto.Error
	for _, bad := range []proto.TaskAskParams{
		{Project: "web", Task: "T1", Kind: project.KindAccept},   // not in review
		{Project: "web", Task: "T9", Kind: project.KindDelegate}, // no such task
		{Project: "web", Task: "T2", Kind: "delete"},
		{Project: "web", Task: "x", Kind: project.KindDelegate},
		{Project: "nope", Task: "T1", Kind: project.KindDelegate},
	} {
		if err := app.Call(proto.MethodTaskAsk, bad, nil); !errors.As(err, &perr) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	for _, want := range []string{" human (app) task.accept.ask T2", " human task.delegate.ask T1"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	if items, _ := pr.Inbox(); len(items) != 2 {
		t.Errorf("inbox %+v", items)
	}

	// The server socket vouches for no human: the app's hello there
	// gains nothing.
	srv, err := client.DialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if err := srv.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "T1", Kind: project.KindDelegate}, nil); !onHumanSocketOnly(err) {
		t.Errorf("task.ask on the server socket: %v", err)
	}
}
