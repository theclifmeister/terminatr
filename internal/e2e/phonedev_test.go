package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestPhoneDev is a dev server for the iPhone app in the Simulator, and
// the Mac app pointed at it (T792, T796; ios/Makefile's dev-server): a
// real tm server with the fake agent, seeded with what the app's screens
// show (seedPhoneDev), its device links on, and tm relay serve
// --self-signed's code on a loopback port in this process (a thread may
// run neither tm relay serve nor tm device pair). It skips unless
// TM_PHONE_DEV_DIR is set. In that directory it writes invite.txt, a
// fresh pairing code each time one is used (the app's -pair launch
// argument takes it), and home.txt, the server's TERMINATR_HOME and
// TERMINATR_SOCKET as shell lines (for tm commands, or the Mac app,
// against it); it watches two files: asleep, while it exists, stops the
// server (the phone shows it asleep or offline; on its return the server
// resumes the agents, idle); stop ends the test. It runs at most
// TM_PHONE_DEV_MINUTES (default 20). A phone that pairs with a write key
// is set to answer, or to TM_PHONE_DEV_LEVEL (write: the phone's write
// screens and its terminal's keys, T840).
func TestPhoneDev(t *testing.T) {
	dir := os.Getenv("TM_PHONE_DEV_DIR")
	if dir == "" {
		t.Skip("TM_PHONE_DEV_DIR not set")
	}
	minutes := 20
	if m, err := time.ParseDuration(os.Getenv("TM_PHONE_DEV_MINUTES") + "m"); err == nil && m > 0 {
		minutes = int(m.Minutes())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(minutes)*time.Minute)
	defer cancel()

	cert, key, pin, err := relay.SelfSigned(filepath.Join(dir, "relay"), []netip.Addr{netip.MustParseAddr("127.0.0.1")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := relay.DefaultConfig()
	cfg.Log = log.New(os.Stderr, "relay: ", log.Lmicroseconds)
	go relay.Serve(ctx, ln, relay.New(cfg), relay.TLS{CertFile: cert, KeyFile: key})
	url := "wss://" + ln.Addr().String() + "#" + relay.PinPrefix + pin

	start := time.Now()
	env := seedPhoneDev(t)
	t.Logf("seeded in %v; relay %s, home %s", time.Since(start).Round(100*time.Millisecond), url, env.Home)
	home := fmt.Sprintf("export TERMINATR_HOME=%q TERMINATR_SOCKET=%q\n", env.Home, env.Socket)
	if err := os.WriteFile(filepath.Join(dir, "home.txt"), []byte(home), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, _, err := peer.EnsureIdentity(peer.DirIn(env.Home))
	if err != nil {
		t.Fatal(err)
	}
	devDir := device.DirIn(env.Home)

	// Pairing, over and over: each code once, the user's yes given here.
	go func() {
		for ctx.Err() == nil {
			in, err := device.NewInvite(url, srv.ID)
			if err != nil {
				t.Error(err)
				return
			}
			paired, err := device.Pair(ctx, srv, in, device.PairOptions{Name: "studio", Version: "dev", Waiting: func() {
				os.WriteFile(filepath.Join(dir, "invite.txt"), []byte(in.String()), 0o644)
				t.Logf("pair: %s", in)
			}, Confirm: func(o device.Offer) (bool, error) {
				t.Logf("pair: yes to %q %s (%s)", o.Name, o.Version, o.ID)
				return true, nil
			}})
			if err != nil {
				t.Logf("pair: %v", err)
				continue
			}
			d, err := device.Add(devDir, paired.Key, paired.WriteKey, paired.Name, url)
			if err != nil {
				t.Logf("pair: %v", err)
				continue
			}
			// A phone that signs its acts answers here (T791): the
			// dev server is the user's own.
			if d.WriteKey != nil {
				level := device.LevelAnswer
				if os.Getenv("TM_PHONE_DEV_LEVEL") == device.LevelWrite {
					level = device.LevelWrite
				}
				if d, err = device.SetLevel(devDir, d.Name, level); err != nil {
					t.Logf("pair: %v", err)
				}
			}
			t.Logf("paired %s at %s", d.Name, d.Effective())
			if c, err := client.DialAt(client.Paths{Socket: env.Socket}.HumanSocket(), proto.KindControl, proto.Protocol); err == nil {
				c.Call(proto.MethodDeviceReload, nil, nil)
				c.Close()
			}
		}
	}()

	asleep := false
	for ctx.Err() == nil {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		_, err := os.Stat(filepath.Join(dir, "asleep"))
		switch want := err == nil; {
		case want && !asleep:
			t.Log("server: asleep")
			env.MustCLI("server", "stop", "--yes")
			asleep = true
		case !want && asleep:
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			t.Log("server: awake")
			env.MustCLI("server", "start")
			asleep = false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// devTask is a task of a seeded project's board.
type devTask struct {
	title, status, notes string
	steps                []string
	checked              int // the first steps ticked
}

// devThread is a thread the seed starts on a task, and the script it
// then runs: the state the app shows it in. The script gets the
// thread's folder in Claude's temp folder, where the seed writes files
// first (a report and its attachment: Claude attaches from there).
type devThread struct {
	task   int // the task's number, T<task>
	script func(task int, tmp string) string
	files  map[string]string
	status string // what the coordinator moves its task to once it's done
}

// devTurn is one exchange of a coordinator's history: the user's prompt,
// the tools it ran and its reply.
type devTurn struct {
	prompt string
	calls  []devCall
	reply  string
}

type devCall struct{ tool, arg, result string }

// seedPhoneDev starts a server with four projects (docs in ios/Makefile):
//
//   - acme-web, pinned: a coordinator with a long history (more than a
//     transcript page: Earlier messages), tasks across the board with
//     steps and notes, and four threads: one asking a question, one
//     working, one blocked on a permission prompt and one idle with its
//     report (an attachment in the library) and its task in review;
//     CONTEXT.md, MEMORY.md and three memory notes; linked both ways with
//     billing-api, whose message (with a file) is in its inbox.
//   - billing-api: a coordinator, a thread idle with a report and one
//     working through its todo list.
//   - docs-site: a coordinator, a few tasks and an open question for the
//     user.
//   - old-prototype: inactive, its tasks done.
//
// Everything comes through tm commands and the fake agent's scripts, the
// way agents make it: deterministic but for ids and times.
func seedPhoneDev(t *testing.T) *Env {
	env := New(t)
	// The server reads its identity as it starts: the pairing's.
	if _, _, err := peer.EnsureIdentity(peer.DirIn(env.Home)); err != nil {
		t.Fatal(err)
	}
	env.FakeClaude()
	scripts := t.TempDir()
	env.Setenv("FAKEAGENT_SCRIPTS", scripts)
	script := func(name, body string) {
		if err := os.WriteFile(filepath.Join(scripts, name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Kickoffs and nudges run scripts (FAKEAGENT_REPLIES), not the fake's
	// "ok: PROMPT": a kickoff the one its agent's state comes from, a
	// nudge an answer in a coordinator's words; one nudge per project at
	// most while it runs.
	replies := filepath.Join(scripts, "replies.toml")
	var kickoffs []string // [[reply]] tables, before the nudge's
	setReplies := func() {
		text := strings.Join(kickoffs, "") + "\n[[reply]]\nmatch = \"[tm]\"\nscript = \"coordinator nudge\"\n"
		if err := os.WriteFile(replies, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	kickoff := func(match, name, body string) {
		script(name, body)
		kickoffs = append(kickoffs, fmt.Sprintf("\n[[reply]]\nmatch = %q\nscript = %q\n", match, name))
		setReplies()
	}
	setReplies()
	script("coordinator nudge", nudgeScript)
	env.Setenv("FAKEAGENT_REPLIES", replies)
	env.Setenv("TERMINATR_TICK_NUDGE", "24h")

	newProject := func(slug, goal string, repo bool) string {
		args := []string{"project", "new", slug, "--goal", goal, "--json"}
		if repo {
			args = append(args, "--repo", devRepo(t, slug))
		}
		var p struct{ Dir string }
		if err := json.Unmarshal([]byte(env.MustCLI(args...)), &p); err != nil {
			t.Fatal(err)
		}
		env.Trust(p.Dir)
		return p.Dir
	}
	acme := newProject("acme-web", "The storefront: catalogue, search, cart and checkout (Next.js)", true)
	newProject("billing-api", "Invoices, payments and the PDF service behind the storefront (Go)", true)
	newProject("docs-site", "The public docs and API reference", true)
	newProject("old-prototype", "The first storefront prototype, kept for reference", false)
	writeConfig(t, env, "[projects.acme-web]\npinned = true\n")

	addTasks := func(slug string, list []devTask) {
		for i, tk := range list {
			args := []string{"task", "add", tk.title, "--project", slug}
			if tk.notes != "" {
				args = append(args, "--notes", tk.notes)
			}
			for _, s := range tk.steps {
				args = append(args, "--step", s)
			}
			env.MustCLI(args...)
			id := "T" + strconv.Itoa(i+1)
			for n := 1; n <= tk.checked; n++ {
				env.MustCLI("task", "steps", id, "check", strconv.Itoa(n), "--project", slug)
			}
			switch tk.status {
			case "", "open":
			case "done":
				env.MustCLI("task", "status", id, "done", "--approved-by-user", "--project", slug)
			default:
				env.MustCLI("task", "status", id, tk.status, "--project", slug)
			}
		}
	}
	addTasks("acme-web", acmeTasks)
	addTasks("billing-api", billingTasks)
	addTasks("docs-site", docsTasks)
	addTasks("old-prototype", oldTasks)
	env.MustCLI("project", "deactivate", "old-prototype", "--yes")

	for name, text := range map[string]string{"CONTEXT.md": acmeContext, "MEMORY.md": acmeMemory} {
		if err := os.WriteFile(filepath.Join(acme, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range acmeNotes {
		f := filepath.Join(t.TempDir(), name+".md")
		os.WriteFile(f, []byte(text), 0o644)
		env.MustCLI("memory", "new", name, "--file", f, "--project", "acme-web")
	}
	env.MustCLI("ask", "add", "Should the API reference move to its own subdomain?", "--project", "docs-site",
		"--header", "Hosting", "--option", "api.acme.dev: its own deploys and search index",
		"--option", "Keep /api under docs: one site, one build", "--recommend", "Keep /api under docs")

	// The coordinators, one at a time (their greeting kickoffs read
	// alike), each with its history: the earlier turns written into its
	// transcript, then the last turn's reply.
	var coords []*Session
	histories := map[string][]devTurn{"acme-web": acmeHistory(), "billing-api": billingHistory, "docs-site": docsHistory}
	for _, slug := range []string{"acme-web", "billing-api", "docs-site"} {
		kickoffs = nil
		kickoff("You are this project's coordinator", slug+" history", historyScript(histories[slug]))
		s := &Session{ID: strings.TrimSpace(env.MustCLI("project", "open", slug))}
		// Idle after its greeting, not before it.
		Poll(agentWait, func() bool {
			n := 0
			for _, r := range env.FakeRecords("prompt") {
				if strings.Contains(r.Str("text"), "You are this project's coordinator") {
					n++
				}
			}
			return n > len(coords)
		})
		env.WaitState(s, "idle", agentWait)
		coords = append(coords, s)
	}
	kickoffs = nil

	// The threads: each one's kickoff (its brief's path names it) runs
	// its script. Files it reports with are in its folder in Claude's
	// temp folder first.
	threads := map[string][]devThread{
		"acme-web": {{task: 13, script: questionScript}, {task: 14, script: workingScript}, {task: 15, script: permissionScript},
			{task: 16, script: reportScript, status: "review", files: map[string]string{"report.md": applePayReport, "apple-pay-review.md": applePayReview}}},
		"billing-api": {{task: 3, script: billingReportScript, files: map[string]string{"report.md": invoiceReport, "invoice-api-v2.md": invoiceAPI}},
			{task: 4, script: billingWorkingScript}},
	}
	type started struct {
		slug string
		th   devThread
		s    *Session
	}
	var all []started
	for _, slug := range []string{"acme-web", "billing-api"} {
		for i, th := range threads[slug] {
			id, thread := "T"+strconv.Itoa(th.task), fmt.Sprintf("t-%04d", i+1)
			tmp, err := home.ThreadTempDirOf(env.Home, slug, thread)
			if err != nil {
				t.Fatal(err)
			}
			tmp = filepath.Join(tmp, "claude-"+strconv.Itoa(os.Getuid()))
			kickoff(filepath.Join("projects", slug, "threads", thread, "brief.md"), slug+" "+thread, th.script(th.task, tmp))
			env.MustCLI("task", "delegate", id, "--approved-by-user", "--over-cap", "--project", slug)
			os.MkdirAll(tmp, 0o700)
			for name, text := range th.files {
				if err := os.WriteFile(filepath.Join(tmp, name), []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			all = append(all, started{slug, th, taskSession(t, env, slug, id)})
		}
	}
	for _, st := range all {
		want := map[int]string{13: "blocked/question", 15: "blocked/permission"}[st.th.task]
		if want == "" {
			want = "working"
			if st.th.files != nil {
				want = "idle"
			}
		}
		// A report's turn ends idle: wait for its report, not an idle
		// before the kickoff.
		if st.th.files != nil {
			Poll(agentWait, func() bool {
				return strings.Contains(env.MustCLI("inbox", "list", "--project", st.slug), fmt.Sprintf("T%d ", st.th.task))
			})
		}
		env.WaitState(st.s, want, agentWait)
		if st.th.status != "" {
			env.MustCLI("task", "status", "T"+strconv.Itoa(st.th.task), st.th.status, "--project", st.slug)
		}
	}
	info, _ := env.Info(all[4].s)

	env.MustCLI("project", "link", "acme-web", "billing-api", "both", "--no-ask")
	env.MustCLI("link", "send", "acme-web", "Invoices v2 is live: GET /v2/invoices/{id} now returns due_at (RFC 3339) "+
		"and a pdf_url that stays valid for an hour. The old /v1 shape goes away on 1 December; the attached "+
		"note has the field list.", "--attach", info.Thread+"/invoice-api-v2.md", "--project", "billing-api")
	for _, s := range coords {
		env.WaitState(s, "idle", agentWait)
	}
	return env
}

// taskSession waits for the session of slug's thread on task id.
func taskSession(t *testing.T, env *Env, slug, id string) *Session {
	t.Helper()
	var s *Session
	Poll(agentWait, func() bool {
		for _, info := range env.Sessions() {
			if info.Role == "thread" && info.Project == slug && info.Task == id {
				s = &Session{ID: info.ID, PID: info.PID}
			}
		}
		return s != nil
	})
	if s == nil {
		t.Fatalf("no thread session for %s's %s", slug, id)
	}
	return s
}

// devRepo is a clone of a bare origin with a commit on main, and a few
// files named like the project's.
func devRepo(t *testing.T, slug string) string {
	root := t.TempDir()
	origin, repo := filepath.Join(root, "origin.git"), filepath.Join(root, slug)
	gittest.Run(t, root, "init", "--bare", "-q", origin)
	gittest.Run(t, root, "clone", "-q", origin, repo)
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("# "+slug+"\n"), 0o644)
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "first")
	gittest.Run(t, repo, "push", "-q", "origin", "HEAD:main")
	gittest.Run(t, repo, "remote", "set-head", "origin", "main")
	return repo
}

// historyScript is a coordinator's greeting: it says hello, writes turns
// into the transcript as they are (prompts, tool calls with their
// results, replies) and ends with the last turn's reply.
func historyScript(turns []devTurn) string {
	var steps []map[string]any
	entry := func(payload map[string]any) {
		steps = append(steps, map[string]any{"do": "transcript", "payload": payload})
	}
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	entry(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
		text("Morning. I've read the board and the inbox; ask me whenever you want a rundown.")}}})
	n := 0
	for i, tn := range turns {
		entry(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": tn.prompt}})
		for _, c := range tn.calls {
			n++
			id := fmt.Sprintf("toolu_dev%04d", n)
			key := map[string]string{"Bash": "command", "Read": "file_path", "Edit": "file_path", "Grep": "pattern"}[c.tool]
			entry(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": id, "name": c.tool, "input": map[string]any{key: c.arg}}}}})
			entry(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": id, "content": c.result}}}})
		}
		if i < len(turns)-1 {
			entry(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{text(tn.reply)}}})
		}
	}
	steps = append(steps, map[string]any{"do": "stream", "ms": 300, "text": turns[len(turns)-1].reply})
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(map[string]any{"step": steps}); err != nil {
		panic(err)
	}
	return b.String()
}

// The thread scripts: what each thread does once started.

func questionScript(task int, tmp string) string {
	return fmt.Sprintf(`
[[step]]
do = "stream"
ms = 300
text = "Search sorts by sales today, so new products sink to the last page. I'll look at what the ranker has to go on."

[[step]]
do = "tool"
tool = "Grep"
input = { pattern = "rankProducts" }
ms = 300

[[step]]
do = "tool"
tool = "Read"
input = { file_path = "src/search/rank.ts" }
ms = 300

[[step]]
do = "run"
cmd = 'tm task steps T%[1]d check 1'

[[step]]
do = "question"
header = "Ranking"
question = "Which ranking should search use for products with no sales yet?"
options = ["Newest first", "By rating", "Keep current"]
descriptions = ["New products surface until they have sales data.", "Uses review scores; unrated ones go last.", "Alphabetical, as today."]

[[step]]
do = "stream"
ms = 300
text = "Noted, going with that."
`, task)
}

func workingScript(task int, tmp string) string {
	return `
[[step]]
do = "todo_create"
subject = "Reproduce the flake"
description = "Run cart.spec.ts 20 times"
active_form = "Reproducing the flake"

[[step]]
do = "todo_create"
subject = "Fix the race"
description = "Wait for the cart total before checkout"
active_form = "Fixing the race"

[[step]]
do = "todo_create"
subject = "Run the suite"
description = "The whole e2e suite, twice"
active_form = "Running the suite"

[[step]]
do = "todo_update"
id = "1"
status = "in_progress"

[[step]]
do = "stream"
ms = 300
text = "The cart test fails about one run in eight on CI. Running it twenty times locally to catch it."

[[step]]
do = "tool"
tool = "Read"
input = { file_path = "e2e/cart.spec.ts" }
ms = 300

[[step]]
do = "tool"
tool = "Bash"
input = { command = "npx playwright test e2e/cart.spec.ts --repeat-each 20" }
ms = 86400000
`
}

func permissionScript(task int, tmp string) string {
	return `
[[step]]
do = "stream"
ms = 300
text = "Next 16 needs React 19.2. I'll bump both and see what breaks."

[[step]]
do = "tool"
tool = "Read"
input = { file_path = "package.json" }
ms = 300

[[step]]
do = "permission"
tool = "Bash"
subject = "npm install next@16.0.1 react@19.2.0 react-dom@19.2.0"
input = { command = "npm install next@16.0.1 react@19.2.0 react-dom@19.2.0", description = "Upgrade Next.js and React" }
response = { stdout = "added 3 packages, changed 41 packages", stderr = "", interrupted = false }

[[step]]
do = "stream"
ms = 300
text = "Installed. Building next."
`
}

// reportStep is a run step that reports with the report and its
// attachment the seed wrote in tmp.
func reportStep(tmp, attach string) string {
	return fmt.Sprintf(`
[[step]]
do = "run"
cmd = 'tm report --file %s --attach %s'
`, filepath.Join(tmp, "report.md"), filepath.Join(tmp, attach))
}

func reportScript(task int, tmp string) string {
	return fmt.Sprintf(`
[[step]]
do = "stream"
ms = 300
text = "Adding the Apple Pay button to checkout, behind the payment-request check."

[[step]]
do = "tool"
tool = "Edit"
input = { file_path = "src/checkout/PaymentButtons.tsx" }
ms = 300

[[step]]
do = "run"
cmd = 'tm task steps T%[1]d check 1'

[[step]]
do = "tool"
tool = "Bash"
input = { command = "npm test -- checkout" }
ms = 300

[[step]]
do = "run"
cmd = 'tm task steps T%[1]d check 2 && tm task steps T%[1]d check 3'
%[2]s
[[step]]
do = "stream"
ms = 300
text = "Reported: the button shows on Safari with a card in Wallet, and falls back to the card form elsewhere. The review notes are attached."
`, task, reportStep(tmp, "apple-pay-review.md"))
}

func billingReportScript(task int, tmp string) string {
	return fmt.Sprintf(`
[[step]]
do = "tool"
tool = "Bash"
input = { command = "go test ./invoices/..." }
ms = 300

[[step]]
do = "run"
cmd = 'tm task steps T%[1]d check 1 && tm task steps T%[1]d check 2'
%[2]s
[[step]]
do = "stream"
ms = 300
text = "Invoices v2 is done and reported, with the field list attached for the storefront."
`, task, reportStep(tmp, "invoice-api-v2.md"))
}

func billingWorkingScript(task int, tmp string) string {
	return `
[[step]]
do = "todo_create"
subject = "Find the dropped webhooks"
description = "Query Stripe's event log for failures"
active_form = "Finding the dropped webhooks"

[[step]]
do = "todo_create"
subject = "Add retries with backoff"
description = "Exponential, at most 6 tries"
active_form = "Adding retries"

[[step]]
do = "todo_update"
id = "1"
status = "completed"

[[step]]
do = "todo_update"
id = "2"
status = "in_progress"

[[step]]
do = "tool"
tool = "Edit"
input = { file_path = "webhooks/retry.go" }
ms = 300

[[step]]
do = "tool"
tool = "Bash"
input = { command = "go test -run TestRetry -count=50 ./webhooks/" }
ms = 86400000
`
}

// The boards.

var acmeTasks = []devTask{
	{title: "Product page: image zoom", status: "done", steps: []string{"Pinch zoom on touch", "Hover zoom on desktop"}, checked: 2},
	{title: "Search: typo tolerance", status: "done", steps: []string{"Fuzzy match", "Tests"}, checked: 2},
	{title: "Cart: keep items across devices", status: "done", steps: []string{"Store the cart server side", "Merge on sign-in"}, checked: 2},
	{title: "Checkout: address autocomplete", status: "done"},
	{title: "Footer: legal links per country", status: "done"},
	{title: "Category pages: infinite scroll", status: "done", steps: []string{"Paginate the API", "Scroll restore"}, checked: 2},
	{title: "Wishlist", status: "done"},
	{title: "Order history page", status: "done"},
	{title: "Gift cards at checkout", status: "done", steps: []string{"Balance check", "Split payment"}, checked: 2},
	{title: "Lighthouse: LCP under 2s on mobile", status: "done"},
	{title: "Reviews: photo uploads", status: "done"},
	{title: "Sitemap and robots.txt", status: "done"},
	{title: "Search: rank new products", notes: "New products have no sales, so they never reach the first page. Pick a ranking with the user.",
		steps: []string{"Read the ranker", "Pick a ranking", "Implement it", "Tests"}},
	{title: "Cart: fix the flaky checkout e2e", notes: "cart.spec.ts fails about one CI run in eight: the total isn't settled when the test clicks Checkout.",
		steps: []string{"Reproduce", "Fix the race", "Suite green twice"}},
	{title: "Upgrade to Next.js 16", notes: "Needs React 19.2. Watch the image loader config, which moved.",
		steps: []string{"Bump the packages", "Fix the build", "Smoke test checkout"}},
	{title: "Checkout: Apple Pay", notes: "Behind the payment-request check; the card form stays the fallback.",
		steps: []string{"Payment button", "Tests", "Review notes"}},
	{title: "Use the image CDN for product photos", status: "ready", notes: "Cloudinary account is set up; the key is in the vault.",
		steps: []string{"Loader", "Migrate the product page", "Purge the old bucket"}},
	{title: "Show invoice PDFs in order history", status: "ready", notes: "billing-api's /v2/invoices has pdf_url (see its link message)."},
	{title: "Dark mode", notes: "Follow the system setting; no toggle for now."},
	{title: "Accessibility pass on checkout", steps: []string{"Keyboard only", "VoiceOver", "Contrast"}},
	{title: "Recently viewed products"},
	{title: "Ship to stores (click and collect)", status: "blocked", notes: "Waiting on the stores' stock API from the retail team."},
}

var billingTasks = []devTask{
	{title: "Move invoices to Postgres 17", status: "done"},
	{title: "Stripe: tax IDs on invoices", status: "done"},
	{title: "Invoices v2 API", notes: "due_at as RFC 3339, pdf_url signed for an hour.", steps: []string{"Endpoint", "Signed PDF links", "Tell the storefront"}},
	{title: "Retry failed Stripe webhooks", notes: "About 0.3% of events are dropped when the API restarts.",
		steps: []string{"Find the dropped ones", "Retries with backoff", "Alert on a dead letter"}},
	{title: "Dunning emails", status: "ready"},
	{title: "Credit notes"},
}

var docsTasks = []devTask{
	{title: "Docs search with Algolia", status: "done"},
	{title: "Versioned API reference", status: "ready", steps: []string{"Pick a generator", "Publish v1 and v2"}},
	{title: "Getting started: rewrite for Next 16"},
	{title: "Broken links checker in CI"},
}

var oldTasks = []devTask{
	{title: "Static catalogue", status: "done"},
	{title: "Fake checkout", status: "done"},
	{title: "Demo for the investors", status: "done"},
}

// The coordinators' histories.

// acmeHistory is twelve tasks' worth of the usual: what's its state,
// delegate it, read the report, merge. More than a transcript page.
func acmeHistory() []devTurn {
	var turns []devTurn
	for i, tk := range acmeTasks[:12] {
		n, th, pr := i+1, fmt.Sprintf("the T%d thread", i+1), 400+3*i
		turns = append(turns,
			devTurn{prompt: fmt.Sprintf("What's the state of T%d?", n),
				calls: []devCall{{"Bash", fmt.Sprintf("tm task show T%d", n), fmt.Sprintf("T%d %s\nstatus: ready\nsteps: 0/2", n, tk.title)}},
				reply: fmt.Sprintf("T%d (%s) is on deck with two steps and nothing in its way. Want me to start a thread on it?", n, tk.title)},
			devTurn{prompt: "Yes, start it.",
				calls: []devCall{{"Bash", fmt.Sprintf("tm task delegate T%d --approved-by-user", n), fmt.Sprintf("started a thread on T%d", n)}},
				reply: fmt.Sprintf("Started a thread on T%d. I'll read its report when it lands.", n)},
			devTurn{prompt: "Anything from " + th + "?",
				calls: []devCall{
					{"Bash", fmt.Sprintf("tm thread show T%d", n), fmt.Sprintf("PR: #%d\n\n## Report\n%s: done, tests added.\n\n## Next\nMerge the PR", pr, tk.title)},
					{"Bash", fmt.Sprintf("gh pr checks %d", pr), "build  pass  2m10s\ntest   pass  4m02s\nlint   pass  38s"}},
				reply: fmt.Sprintf("It opened #%d for T%d and the checks are green: build, test and lint. It added tests for both steps. Merge it?", pr, n)},
			devTurn{prompt: "Merge it.",
				calls: []devCall{{"Bash", fmt.Sprintf("tm thread merge T%d", n), fmt.Sprintf("merged #%d (squash)", pr)}},
				reply: fmt.Sprintf("Merged #%d. T%d is done and its thread will close itself.", pr, n)},
		)
	}
	return append(turns,
		devTurn{prompt: "Add the next batch: new-product ranking, the flaky cart test, Next 16 and Apple Pay.",
			calls: []devCall{{"Bash", "tm task add --json < plan.json", "T13 Search: rank new products\nT14 Cart: fix the flaky checkout e2e\nT15 Upgrade to Next.js 16\nT16 Checkout: Apple Pay"}},
			reply: "Added T13-T16 with their steps and notes. They're independent, so all four can run at once."},
		devTurn{prompt: "Start all four.",
			calls: []devCall{
				{"Bash", "tm task delegate T13 --approved-by-user", "started t-0001 on T13"},
				{"Bash", "tm task delegate T14 --approved-by-user", "started t-0002 on T14"},
				{"Bash", "tm task delegate T15 --approved-by-user --over-cap", "started t-0003 on T15"},
				{"Bash", "tm task delegate T16 --approved-by-user --over-cap", "started t-0004 on T16"}},
			reply: "All four are running. T13 will need your pick on the ranking, and T15 may ask before it installs packages."},
		devTurn{prompt: "Where are we?",
			calls: []devCall{{"Bash", "tm thread list", "t-0001 T13 blocked  question: Which ranking should search use…\nt-0002 T14 working  Bash: npx playwright test\nt-0003 T15 blocked  permission: Bash npm install next@16.0.1…\nt-0004 T16 idle     reported"}},
			reply: "T16 (Apple Pay) reported and is in review: its notes are in the library. T14 is still chasing the flaky cart test. T13 waits on your pick for the ranking, and T15 asks to run npm install for Next 16. Billing says invoices v2 is live, so T18 can start once you say."},
	)
}

var billingHistory = []devTurn{
	{prompt: "What's left before invoices v2 ships?",
		calls: []devCall{{"Bash", "tm task show T3", "T3 Invoices v2 API\nstatus: ready\nsteps: 0/3"}},
		reply: "T3 has three steps: the endpoint, signed PDF links and telling the storefront. Nothing blocks it."},
	{prompt: "Start it, and the webhook retries too.",
		calls: []devCall{
			{"Bash", "tm task delegate T3 --approved-by-user", "started t-0001 on T3"},
			{"Bash", "tm task delegate T4 --approved-by-user", "started t-0002 on T4"}},
		reply: "Started t-0001 on invoices v2 and t-0002 on the webhook retries."},
	{prompt: "Did invoices v2 land?",
		calls: []devCall{{"Read", "threads/t-0001/report.md", "## Report\nGET /v2/invoices/{id} returns due_at and pdf_url.\n\n## Next\nTell acme-web"}},
		reply: "Yes: t-0001 reported, the field list is attached. I've sent acme-web a link message with it, and t-0002 is still on the retries."},
}

var docsHistory = []devTurn{
	{prompt: "What's on deck?",
		calls: []devCall{{"Bash", "tm task list --status ready", "T2 Versioned API reference"}},
		reply: "Only T2, the versioned API reference."},
	{prompt: "Before we start T2: where should the reference live?",
		reply: "That depends on hosting, so I've put it to you as a question: its own subdomain, or kept under /api in the docs. I'd keep it under docs: one site, one build."},
}

// The files: acme-web's context and memory, and the reports' attachments.

const acmeContext = `# Context

Living context for this project: the current plan, conventions and
decisions. The coordinator keeps it current; threads read it.

## Where things stand

- Storefront on Next.js 15, moving to 16 (T15). Deploys from main on
  Vercel; previews per PR.
- Checkout: card form and gift cards; Apple Pay in review (T16).
- Search ranks by sales; new products need a ranking (T13, the user picks).
- billing-api serves invoices; v2 is live (link message from billing-api).

## Conventions

- One PR per task, squash merge when green.
- e2e with Playwright: ` + "`npm run test:e2e`" + `; unit with Vitest.
- No new dependencies without asking.

## Needs you

- T13: pick the ranking for products with no sales.
- T15: allow npm install for the Next 16 upgrade.
`

const acmeMemory = `# Memory

Lessons for threads, one per line; the notes in memory/ hold the
details.

- Checkout never ships without the card form as the fallback ([[checkout]]).
- Search results are cached for 60s: test ranking changes with ?nocache=1 ([[search]]).
- CI's e2e job runs on 2 cores: wait for network idle, never fixed sleeps ([[e2e-tests]]).
`

var acmeNotes = map[string]string{
	"checkout": `---
title: "Checkout keeps the card form as the fallback"
touches:
  - "src/checkout/"
standing: true
---
## Decided
- Every new payment method shows only when the browser supports it; the card form stays below it.
- Payment buttons are in PaymentButtons.tsx, one component each.

## Ruled out
- Hiding the card form when Apple Pay is available: users with no card in Wallet got stuck.

## Watch out
- The payment-request check is async: render nothing until it settles, never a flash of the button.
`,
	"search": `---
title: "Search results are cached for 60 seconds"
touches:
  - "src/search/"
---
## Decided
- The ranker runs on the server; results are cached per query for 60s.

## Watch out
- Add ?nocache=1 when checking a ranking change, or you see the old order for a minute.
`,
	"e2e-tests": `---
title: "e2e tests wait for network idle"
touches:
  - "e2e/"
---
## Decided
- Wait for the network to go idle or for a specific element, never a fixed sleep.

## Ruled out
- Retrying flaky tests in CI: it hid a real race in the cart total.
`,
}

const applePayReport = `PR: https://github.com/acme/web/pull/452

## Report
Checkout: Apple Pay (#452). The button shows on Safari when Wallet has a
card, behind the payment-request check; elsewhere the card form shows as
before. Vitest and Playwright pass. Review notes attached
(apple-pay-review.md).

## Next
Try it on an iPhone with a card in Wallet
Merge #452 when you're happy

## Check
Open a preview on Safari with a card in Wallet: the black Apple Pay
button sits above the card form.
`

const applePayReview = `# Apple Pay: review notes

## What changed
- PaymentButtons.tsx: ApplePayButton, shown when canMakePayments() says yes.
- The merchant session comes from /api/apple-pay/session (new route).

## Tested
- Safari 26 on macOS with a card in Wallet: pays, order confirmed.
- Chrome: no button, the card form as before.
- Vitest: 14 new tests. Playwright: checkout suite green twice.

## Open
- The domain verification file must be on the production domain before launch.
`

const invoiceReport = `## Report
Invoices v2 API: GET /v2/invoices/{id} returns due_at (RFC 3339) and a
signed pdf_url valid for an hour. v1 stays until 1 December. The field
list for the storefront is attached (invoice-api-v2.md).

## Next
Send acme-web the field list
`

const invoiceAPI = `# Invoices v2

GET /v2/invoices/{id}

| field    | type           | notes                    |
|----------|----------------|--------------------------|
| id       | string         | inv_…                    |
| total    | integer        | minor units              |
| currency | string         | ISO 4217                 |
| due_at   | string         | RFC 3339                 |
| pdf_url  | string         | signed, valid for 1 hour |

v1 goes away on 1 December.
`

// nudgeScript is a coordinator's answer to the ticker's nudge.
const nudgeScript = `
[[step]]
do = "tool"
tool = "Bash"
input = { command = "tm inbox list" }
ms = 200

[[step]]
do = "stream"
ms = 200
text = "Read the new inbox items. Nothing in them changes the plan; what needs you is on the board."
`
