package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sandbox is project slug's sandbox lists for its threads on platform
// goos (docs/SPEC.md §5.2), the guides they get there, and a
// note for each rule that doesn't apply or is refused, which the launch
// skips: config.toml keeps it. A config.toml that doesn't load opens
// nothing (fails closed) and gives one note saying why, for the log and
// the launch's warnings. It is launchSandbox of the rules (access.go).
func (l launchConfig) sandbox(slug, goos string) (lists agent.SandboxLists, guides []config.Guide, skipped []string) {
	rules, why := sandboxRules(l, slug, goos, tmDirs{})
	return launchSandbox(proto.AccessSet{Rules: rules, SandboxError: why})
}

// maxBrief is the most of a thread's brief.md that guideBrief copies:
// the brief is the coordinator's text, not unbounded.
const maxBrief = 1 << 20

// approvedGuide is the section of the brief of a thread with approved
// commands (approved.go); %s lists them.
const approvedGuide = `## Approved commands: tm runs them outside your sandbox

The human approved these commands for this project's threads. Your sandbox
can't run them (they need what it closes), so ask tm to run one for you,
outside the sandbox, in your worktree, with its output streamed back:

    tm run -- <command> [args…]      (tm run --list lists them)

%s
A command matches word for word; a last * takes any further arguments. tm
runs the words as they are, without a shell: no pipes, redirects or
variables. Each run is journaled. Run nothing else outside the sandbox this
way: only the human adds a command to this list.
`

// approvedText is the list of approvedGuide.
func approvedText(cmds []string) string {
	var b strings.Builder
	for _, c := range cmds {
		b.WriteString("- " + c + "\n")
	}
	return b.String()
}

// guideBrief is the brief a thread with guides or approved commands
// gets: brief (its brief.md, "" for none) with each guide after it
// (config.GuideText), then the approved commands of lists, written in
// its runtime dir rt at every launch, so a resume sees today's rules;
// brief itself without any.
func guideBrief(rt, brief string, guides []config.Guide, lists agent.SandboxLists) (string, error) {
	if len(guides) == 0 && len(lists.Approved) == 0 {
		return brief, nil
	}
	var text []byte
	if brief != "" {
		f, err := os.Open(brief)
		if err != nil {
			return brief, err
		}
		b, err := io.ReadAll(io.LimitReader(f, maxBrief+1))
		f.Close()
		if err != nil {
			return brief, err
		}
		if len(b) > maxBrief {
			return brief, fmt.Errorf("brief %s is longer than %d bytes", brief, maxBrief)
		}
		text = append(b, '\n')
	}
	guide := config.GuideText(guides)
	if len(lists.Approved) > 0 {
		if guide != "" {
			guide += "\n"
		}
		guide += fmt.Sprintf(approvedGuide, approvedText(lists.Approved))
	}
	path := filepath.Join(rt, "brief.md")
	if err := os.WriteFile(path, append(text, guide...), 0o600); err != nil {
		return brief, err
	}
	return path, nil
}
