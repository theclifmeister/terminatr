package e2e

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// pairing is one tm peer pair or join running, its output read line by
// line.
type pairing struct {
	t     *testing.T
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan string
	out   strings.Builder
}

func startPairing(t *testing.T, env *Env, args ...string) *pairing {
	t.Helper()
	p := &pairing{t: t, cmd: exec.Command(env.Bin, append([]string{"peer"}, args...)...), lines: make(chan string, 64)}
	p.cmd.Env = env.Vars
	var err error
	if p.in, err = p.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	out, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.cmd.Stderr = p.cmd.Stdout
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	env.track(p.cmd.Process.Pid, "tm peer "+args[0])
	go func() {
		defer close(p.lines)
		sc := bufio.NewScanner(out)
		sc.Split(scanPrompt)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
	}()
	return p
}

// scanPrompt splits at newlines, and after a prompt's "] " that waits
// for an answer without one.
func scanPrompt(data []byte, atEOF bool) (int, []byte, error) {
	if i := strings.IndexByte(string(data), '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if i := strings.Index(string(data), "[y/N] "); i >= 0 {
		return i + 6, data[:i+6], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// waitLine waits for a line matching re and returns its submatches.
func (p *pairing) waitLine(re string) []string {
	p.t.Helper()
	rx := regexp.MustCompile(re)
	timeout := time.After(30 * time.Second)
	for {
		select {
		case l, ok := <-p.lines:
			if !ok {
				p.t.Fatalf("tm peer ended before %q:\n%s", re, p.out.String())
			}
			p.out.WriteString(l + "\n")
			if m := rx.FindStringSubmatch(l); m != nil {
				return m
			}
		case <-timeout:
			p.t.Fatalf("no %q within 30s:\n%s", re, p.out.String())
		}
	}
}

func (p *pairing) answer(s string) {
	p.t.Helper()
	if _, err := io.WriteString(p.in, s+"\n"); err != nil {
		p.t.Fatal(err)
	}
}

// wait waits for the command to end and returns its exit code and the
// rest of its output.
func (p *pairing) wait() (int, string) {
	p.t.Helper()
	for l := range p.lines {
		p.out.WriteString(l + "\n")
	}
	err := p.cmd.Wait()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), p.out.String()
	} else if err != nil {
		p.t.Fatal(err)
	}
	return 0, p.out.String()
}

// waitPeerState waits until env's server shows its link to alias in
// state (and reason).
func waitPeerState(t *testing.T, env *Env, alias, state, reason string) {
	t.Helper()
	var last string
	ok := Poll(20*time.Second, func() bool {
		var st proto.ServerStatus
		r := env.CLI("server", "status", "--json")
		if json.Unmarshal([]byte(r.Stdout), &st) != nil || st.Link == nil {
			last = r.Stdout + r.Stderr
			return false
		}
		for _, p := range st.Link.Peers {
			if p.Alias == alias {
				last = p.State + " " + p.Reason + " " + p.Error
				return p.State == state && p.Reason == reason
			}
		}
		last = "no link to " + alias
		return false
	})
	if !ok {
		t.Fatalf("link to %s never %s %s; last: %s", alias, state, reason, last)
	}
}

// TestSmokePeerPairing (T394, relay phase 1): two installations pair
// through an in-process relay with tm peer pair and join, both people
// confirming the same six digits, and their servers' link comes up. A
// join with the used code finds nobody; a removed peer's link goes.
func TestSmokePeerPairing(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	relayURL := "ws" + strings.TrimPrefix(hs.URL, "http")

	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")

	pair := startPairing(t, studio, "pair", "--relay", relayURL, "--as", "mini")
	code := pair.waitLine(`^On the other server: tm peer join (\S+)$`)[1]
	if !strings.HasPrefix(code, relayURL+"/") {
		t.Fatalf("code %q", code)
	}
	join := startPairing(t, mini, "join", code, "--as", "studio")
	checkA := pair.waitLine(`^Check: (\d{3} \d{3})$`)[1]
	checkB := join.waitLine(`^Check: (\d{3} \d{3})$`)[1]
	if checkA != checkB {
		t.Fatalf("the two sides show %s and %s", checkA, checkB)
	}
	pair.waitLine(`Do they match\? \[y/N\] $`)
	join.waitLine(`Do they match\? \[y/N\] $`)
	pair.answer("y")
	join.answer("y")
	if code, out := pair.wait(); code != 0 || !strings.Contains(out, "Paired with mini") {
		t.Fatalf("pair: exit %d\n%s", code, out)
	}
	if code, out := join.wait(); code != 0 || !strings.Contains(out, "Paired with studio") {
		t.Fatalf("join: exit %d\n%s", code, out)
	}

	waitPeerState(t, studio, "mini", proto.LinkUp, "")
	waitPeerState(t, mini, "studio", proto.LinkUp, "")
	if out := studio.MustCLI("peer", "list"); !regexp.MustCompile(`(?m)^mini\s+[0-9A-F]{4}-[0-9A-F]{4}\s+\S+\s+up `).MatchString(out) {
		t.Fatalf("peer list:\n%s", out)
	}

	// The code worked once.
	again := mini.CLI("peer", "join", code)
	if again.Code != 1 || !strings.Contains(again.Stderr, "nobody is pairing with this code") {
		t.Fatalf("join again: exit %d\n%s%s", again.Code, again.Stdout, again.Stderr)
	}

	// Removed on one side: its link goes, the other's waits for it.
	studio.MustCLI("peer", "remove", "mini")
	if out := studio.MustCLI("peer", "list"); !strings.Contains(out, "no peers") {
		t.Fatalf("peer list after remove:\n%s", out)
	}
	waitPeerState(t, mini, "studio", proto.LinkOffline, proto.LinkReasonPeer)
}
