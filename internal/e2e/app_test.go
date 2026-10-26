package e2e

// tm app bridge (docs/SPEC.md §3.8): the Mac app's child, driven here by
// a fake app (apptest) as the app runs it, over its stdin and stdout.

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// runBridge starts tm app bridge in env, as the app does.
func runBridge(t *testing.T, env *Env) *apptest.Bridge {
	t.Helper()
	b, err := apptest.Run(env.Bin, env.Vars)
	if err != nil {
		t.Fatal(err)
	}
	env.track(b.Cmd.Process.Pid, "tm app bridge")
	t.Cleanup(func() { b.Close() })
	return b
}

func appCode(err error) string {
	var e *appapi.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// TestSmokeAppBridgeApprove: the app sees a thread blocked on a
// permission prompt and approves it through session.approve: the
// server reads the dialog off the screen first (check), refuses an
// option that allows always, and journals the answer as "human (app)".
// A prompt from the app takes the thread over, as typing in its pane
// does.
func TestSmokeAppBridgeApprove(t *testing.T) {
	env, projDir, _ := tickerEnv(t)
	th := startThread(t, env, projDir)
	b := runBridge(t, env)
	if b.Hello.Server == nil || b.Hello.Server.Protocol != proto.Protocol || b.Hello.AppAPI != appapi.Version {
		t.Fatalf("hello %+v", b.Hello)
	}

	var ps appapi.Projects
	st, err := b.Stream(proto.MethodProjectsWatch, appapi.ProjectsWatchParams{Detail: true}, &ps)
	if err != nil || len(ps.Projects) != 1 || len(ps.Projects[0].Threads) != 1 || ps.Projects[0].Threads[0].Record.Session != th.ID {
		t.Fatalf("projects.watch: %+v %v", ps, err)
	}
	defer st.Cancel()

	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID}, nil); appCode(err) != proto.ErrNotPermission {
		t.Fatalf("approve while idle: %v", err)
	}
	env.MustCLI("thread", "prompt", "t-0001", "run thread-block", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	waitReason(t, env, th, "permission")

	var list appapi.SessionList
	if err := b.Call(proto.MethodSessionList, nil, &list); err != nil {
		t.Fatal(err)
	}
	blocked := false
	for _, s := range list.Sessions {
		blocked = blocked || s.ID == th.ID && s.State == "blocked" && s.Reason == "permission"
	}
	if !blocked {
		t.Fatalf("session.list: %+v", list.Sessions)
	}

	var check appapi.ApproveResult
	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID, Check: true}, &check); err != nil ||
		check.Sent || check.Option == "" || len(check.Options) < 2 {
		t.Fatalf("check: %+v %v", check, err)
	}
	for i, o := range check.Options {
		if strings.Contains(strings.ToLower(o), "don't ask again") || strings.Contains(strings.ToLower(o), "always") {
			if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID, Choice: i + 1}, nil); appCode(err) != proto.ErrAlwaysAllow {
				t.Errorf("option %d (%s): %v", i+1, o, err)
			}
		}
	}
	var res appapi.ApproveResult
	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID}, &res); err != nil || !res.Sent || res.Option != check.Option {
		t.Fatalf("approve: %+v %v", res, err)
	}
	env.WaitState(th, "idle", agentWait)

	var pr appapi.PromptResult
	if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: th.ID, Text: "from the app"}, &pr); err != nil || pr.Via == "" {
		t.Fatalf("prompt: %+v %v", pr, err)
	}
	waitInbox(t, env, "the user typed into")
	journal, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	for _, want := range []string{"human (app) thread.approve t-0001 ", "human (app) session.prompt t-0001 from the app",
		"human (app) thread.takeover t-0001"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
}

// TestAppBridgeEscape: a prompt to run a command outside the sandbox is
// the user's alone: the app's approve is refused until it says the user
// confirmed that command (escape), then goes through.
func TestAppBridgeEscape(t *testing.T) {
	env, projDir, _ := tickerEnv(t)
	writeConfig(t, env, "[projects.demo]\nguard = false\n")
	th := startThread(t, env, projDir)
	b := runBridge(t, env)
	env.MustCLI("thread", "prompt", "t-0001", "run thread-escape", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	waitReason(t, env, th, "permission")

	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID}, nil); appCode(err) != proto.ErrEscapeUnconfirmed {
		t.Fatalf("approve of an escape unconfirmed: %v", err)
	}
	var check appapi.ApproveResult
	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID, Check: true}, &check); err != nil || !check.Escape || check.Sent {
		t.Fatalf("check: %+v %v", check, err)
	}
	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID, Escape: true}, nil); err != nil {
		t.Fatalf("approve, confirmed: %v", err)
	}
	env.WaitFor(th, "ESCAPED", agentWait)
}

// TestAppBridgePane: the app streams a thread's pane (pane.vt) and its
// conversation (transcript.watch), as the hello's caps say it may; text
// it types into the pane takes the thread over, journaled as the app's.
func TestAppBridgePane(t *testing.T) {
	env, projDir, _ := tickerEnv(t)
	th := startThread(t, env, projDir)
	b := runBridge(t, env)
	if caps := b.Hello.Server.Caps; !slices.Contains(caps, proto.CapPaneVT) || !slices.Contains(caps, proto.CapTranscript) {
		t.Fatalf("caps %v", caps)
	}
	var tr appapi.TranscriptResult
	ts, err := b.Stream(proto.MethodTranscriptWatch, appapi.TranscriptParams{Session: th.ID}, &tr)
	if err != nil || tr.Session.ID != th.ID || tr.Status.State == "" {
		t.Fatalf("transcript.watch: %+v %v", tr, err)
	}
	defer ts.Cancel()

	var pr appapi.PaneResult
	pane, err := b.Stream(proto.MethodPaneVT, appapi.PaneParams{Session: th.ID}, &pr)
	if err != nil || pr.Session.ID != th.ID {
		t.Fatalf("pane.vt: %+v %v", pr, err)
	}
	defer pane.Cancel()
	var ev appapi.PaneEvent
	if name, err := pane.Next(&ev); err != nil || name != proto.EventPaneRepaint || len(ev.Data) == 0 {
		t.Fatalf("first event %q %v", name, err)
	}
	if err := b.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte("typed by the app")}, nil); err != nil {
		t.Fatal(err)
	}
	waitInbox(t, env, "the user typed into")
	journal, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	if !strings.Contains(string(journal), "human (app) thread.takeover t-0001") {
		t.Errorf("journal:\n%s", journal)
	}
}
