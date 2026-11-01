package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/skill"
	"github.com/theclifmeister/terminatr/internal/version"
)

// The system prompt (docs/SPEC.md §7.8): what is large and stable goes
// there, once per launch, for an agent whose manifest takes one
// ([launch] system_prompt): the role's rules, after a thread's brief or
// before a coordinator's PROJECT.md. The agent keeps it across a clear
// and a compaction, so the SessionStart block holds only state and stays
// under the agent's cap on hook output (Claude: 10,000 characters).

// PromptStamp is what a session's system prompt was launched with, so
// the SessionStart block can say when it went stale: the rules' hash
// and tm's version, and for a coordinator PROJECT.md's hash. Nil: the
// session has no rules in its system prompt (an adopted session, an
// agent without one), and the block carries them.
type PromptStamp struct {
	Version string `json:"version"`
	Rules   string `json:"rules"`
	Project string `json:"project,omitempty"`
}

// maxProjectPrompt bounds the PROJECT.md a coordinator's system prompt
// carries; past it the file is to be read.
const maxProjectPrompt = 32 << 10

// promptFile is the system prompt's name in the session's runtime dir.
const promptFile = "system-prompt.md"

// textHash is a short hash of s, for a stamp.
func textHash(s []byte) string {
	h := sha256.Sum256(s)
	return hex.EncodeToString(h[:8])
}

// roleRules is the role's rules for project slug, as `tm skill <role>`
// prints them for it.
var roleRules = func(role, slug string) (string, bool) {
	return skill.ForProject(role, version.Version, slug)
}

// writeSystemPrompt writes the system prompt of a session of role in
// project slug into rt and returns its path and stamp. brief is the
// thread's brief file (the sandbox guidance already in it). A role
// other than coordinator and thread has none: "", nil.
func writeSystemPrompt(rt, role, slug, brief string) (string, *PromptStamp, error) {
	if slug == "" || (role != proto.RoleCoordinator && role != proto.RoleThread) {
		return "", nil, nil
	}
	rules, ok := roleRules(role, slug)
	if !ok {
		return "", nil, fmt.Errorf("no rules for role %s", role)
	}
	stamp := &PromptStamp{Version: version.Version, Rules: textHash([]byte(rules))}
	var b strings.Builder
	switch role {
	case proto.RoleThread:
		text, err := readBrief(brief)
		if err != nil {
			return "", nil, err
		}
		b.WriteString(strings.TrimRight(text, "\n"))
		b.WriteString("\n\n# Your rules\n\n" + rules)
	case proto.RoleCoordinator:
		p, err := project.Open(slug)
		if err != nil {
			return "", nil, err
		}
		b.WriteString(rules)
		path := p.Path("PROJECT.md")
		text, err := os.ReadFile(path)
		if err != nil {
			return "", nil, err
		}
		stamp.Project = textHash(text)
		fmt.Fprintf(&b, "\n# PROJECT.md\n\n%s as this session started (tm says when it changes: read it then).\n\n", path)
		if len(text) > maxProjectPrompt {
			text = append(text[:maxProjectPrompt:maxProjectPrompt], fmt.Sprintf("\n[… cut at %d bytes: read the rest in %s]", maxProjectPrompt, path)...)
		}
		b.WriteString(strings.TrimRight(string(text), "\n") + "\n")
	}
	out := filepath.Join(rt, promptFile)
	if err := os.WriteFile(out, []byte(b.String()), 0o600); err != nil {
		return "", nil, err
	}
	return out, stamp, nil
}

// readBrief is a brief's text, at most maxBrief bytes; "" for none.
func readBrief(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxBrief+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxBrief {
		return "", fmt.Errorf("brief %s is longer than %d bytes", path, maxBrief)
	}
	return string(b), nil
}

// promptHeader is the SessionStart block's first lines for a session
// whose rules are in its system prompt (stamp not nil): where they are,
// and a line to re-read what changed since it started, its rules (a tm
// upgrade, a mod's rules) or a coordinator's PROJECT.md. rules is the
// role's rules now; projectFile PROJECT.md's path ("" for a thread).
func promptHeader(role string, stamp *PromptStamp, rules, projectFile string) string {
	var b strings.Builder
	switch role {
	case proto.RoleCoordinator:
		b.WriteString("Your rules (`tm skill coordinator`) and PROJECT.md are in your system prompt; this block is the project's state.\n")
	default:
		b.WriteString("Your brief and your rules (`tm skill thread`) are in your system prompt; this block is your thread's state.\n")
	}
	if rules != "" && textHash([]byte(rules)) != stamp.Rules {
		cmd := "`tm skill " + role + "`"
		if stamp.Version != version.Version {
			fmt.Fprintf(&b, "tm was upgraded since this session started (%s → %s): run %s once and follow it over the rules in your system prompt.\n",
				stamp.Version, version.Version, cmd)
		} else {
			fmt.Fprintf(&b, "Your rules changed since this session started (a mod's rules): run %s once and follow it over the rules in your system prompt.\n", cmd)
		}
	}
	if projectFile != "" && stamp.Project != "" {
		if text, err := os.ReadFile(projectFile); err == nil && textHash(text) != stamp.Project {
			fmt.Fprintf(&b, "PROJECT.md changed since this session started: read %s once; it replaces the copy in your system prompt.\n", projectFile)
		}
	}
	return b.String()
}

// winCmdLine is the longest command line Windows starts a process with
// (CreateProcess: 32,767 characters), less room for quoting.
const winCmdLine = 32000

// argvTooLong says argv makes a command line longer than goos starts a
// process with. Only Windows's limit is near a system prompt's size;
// Linux takes 128 KiB per argument, macOS 1 MiB in all.
func argvTooLong(goos string, argv []string) bool {
	if goos != "windows" {
		return false
	}
	n := 0
	for _, a := range argv {
		n += len(a) + 3 // a space, and quotes
	}
	return n > winCmdLine
}
