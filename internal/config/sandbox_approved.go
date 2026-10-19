package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Approved commands (T306, T574 phase 4): commands the human lets a
// thread ask tm to run outside the sandbox (tm run -- make test), in
// the thread's worktree, with tm's environment rather than the
// thread's, its output streamed back and each run journaled. The list
// is the human's, like every sandbox list: a thread runs only what it
// names. A command is words, matched word for word with what the thread
// asks for; a last word * takes any further arguments (none too):
//
//	approved_commands = ["make test", "make e2e", "npm run e2e *", "docker compose up *"]
//
// tm runs the words as they are, without a shell: no pipe, redirect,
// substitution, variable or quote means anything, so the list may hold
// none. A pattern that leaves open which program or code runs (sh *,
// python3 -c *, npx *, make *) is the human's to set (T619): tm says
// what it lets through where it is added, and takes it.
//
// A Codex thread has no excluded commands of its own (T574 §4.6, case
// 3): each excluded command whose words fit this form is an approved
// command for it, asking you first unless it runs without asking.

// ApprovedWords is approved command e's words, and whether its last
// word * takes further arguments (it isn't among the words then).
func ApprovedWords(e string) (words []string, more bool) {
	words = strings.Fields(e)
	if n := len(words); n > 0 && words[n-1] == "*" {
		return words[:n-1], true
	}
	return words, false
}

// ApprovedMatch reports whether argv is a run of approved command e:
// its words exactly, then any arguments when its last word is *.
func ApprovedMatch(e string, argv []string) bool {
	words, more := ApprovedWords(e)
	if len(words) == 0 || len(argv) < len(words) || !more && len(argv) != len(words) {
		return false
	}
	for i, w := range words {
		if argv[i] != w {
			return false
		}
	}
	return true
}

// approvedForm is why e's words don't fit an approved command's form,
// or "": words of plain text, a * only as a last word of its own.
func approvedForm(e string) string {
	words, _ := ApprovedWords(e)
	if len(words) == 0 {
		return fmt.Sprintf("%q names no program", e)
	}
	if strings.Contains(e, "  ") || strings.ContainsAny(e, "\t") {
		return fmt.Sprintf("%q has words apart by more than one space: write them with one space between", e)
	}
	for _, w := range words {
		switch {
		case strings.ContainsAny(w, "*?[]{}"):
			return fmt.Sprintf("%q has a pattern inside it: only a last word * takes more arguments", e)
		case strings.ContainsAny(w, "'\"\\`"):
			return fmt.Sprintf("%q has a quote or backslash: tm runs the words as they are, without a shell", e)
		case strings.Contains(w, "$"):
			return fmt.Sprintf("%q has a $: tm runs the words as they are, without a shell or variables", e)
		}
	}
	for _, op := range shellOps {
		if strings.Contains(e, op) {
			return fmt.Sprintf("%q has %q: tm runs one command, without a shell, so pipes, redirects and ; mean nothing", e, strings.TrimSpace(op))
		}
	}
	return ""
}

// CheckApprovedCommand checks one approved command: its form
// (approvedForm), and never tm itself, which would run as you outside
// the sandbox. A wide pattern passes, with its warning
// (approvedWarning).
func CheckApprovedCommand(e string) error {
	if err := checkSandboxText(e); err != nil {
		return err
	}
	if why := approvedForm(e); why != "" {
		return errors.New(why)
	}
	// tm itself, also behind a wrapper (env tm, sudo tm).
	words, _ := ApprovedWords(e)
	for wrapped := false; len(words) > 0; words = words[1:] {
		switch w := words[0]; {
		case program(w) == "tm":
			return fmt.Errorf("%q is tm itself: a thread runs tm in its sandbox already", e)
		case slices.Contains(wrappers, program(w)):
			wrapped = true
			continue
		case wrapped && (strings.HasPrefix(w, "-") || strings.Contains(w, "=") || strings.Trim(w, "0123456789.smhd") == ""):
			continue
		}
		break
	}
	return nil
}

// ApprovedOf is excluded command e as an approved command for a Codex
// thread (T574 case 3): e itself when it fits an approved command's
// form and checks, else "" and why not.
func ApprovedOf(e string) (string, string) {
	if err := CheckApprovedCommand(e); err != nil {
		return "", "not an approved command for Codex threads: " + err.Error()
	}
	return e, ""
}

// approvedWarning is what an approved command opens, said where it is
// added: tm runs it outside the sandbox as you, and the thread writes
// the files most commands read.
func approvedWarning(e string) string {
	w := "tm runs it outside the sandbox, as you, in the thread's worktree, without asking: the thread writes the files it reads (a Makefile, package.json, the tests), so it can make it run any code as you"
	words, more := ApprovedWords(e)
	switch why := codeWarning(e); {
	case more && why != "":
		w += "; " + why
	case more && len(words) == 1:
		w += "; it leaves every argument of " + words[0] + " open, which is everything it can do"
	}
	if more {
		w += "; its last * passes any argument the program takes, its options too (git log --output writes any file, go test -exec runs any program, docker compose run mounts any folder)"
	}
	return w
}
