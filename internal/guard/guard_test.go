package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// vectors are the guard's test vectors (testdata/vectors.json): what
// each call breaks (rule, null for none) and the summary journaled; and
// the sentence each rule gives each role. The tools (each one's kind and
// input fields, as a manifest's [guard.tools] gives them) go with every
// rule set: Claude's and Codex's, which their manifests must match
// (internal/agent TestGuardToolVectors), and a lowercase vocabulary like
// pi's and opencode's. A case the guard asks about rather than refuses (a
// PowerShell command it can't read) has rule "unknown" and "ask": true.
// The thread-mods and mods-only sets carry rules a mod adds (mods design
// §7, Tighten only): the core's and the mod's, and the mod's alone. The
// yolo-thread set is a yolo thread's guard: the floor only, with tm's
// settings and the project folder no tool writes (noWrite); the
// floor-coordinator set the coordinator's, whose project folder it writes
// but for tm's parts of it; thread-escapes asks about a call that asks to
// leave the sandbox (askEscapes), rule "escape"; thread-ask-first asks
// about a command of an excluded command that asks first (askFirst), rule
// "excluded", and thread-ask-first-only has nothing else; thread-own and
// thread-own-off carry excluded commands of the user's own Claude
// settings (unsandboxed): a call one takes and tm's (tmExcluded) don't is
// an escape, asked, or with escapes off (noEscapes) refused. The
// thread-allow, thread-learn and floor-only sets carry the human's
// allowances (a refusal under an allowance's rule of the command line or
// file it matches is lifted; an "exact" one's * matches only a *), tm's
// own files and the secret variables: a case with "floor" is a refusal no
// allowance lifts; the rules-off sets have the force-push and merge rules
// off (guard_off) and their floor still refusing; "subject" (the command
// line or file an allowance names) and "message", when a case gives them,
// are the refusal's. A case with links judges file writes where they
// land: each link (an absolute path) leads to its target, and a write
// under a link to the same place under the target (Resolve, faked). A
// case with files has those paths (absolute, clean, where they land)
// regular files, the rest not: a shell given input runs a script of the
// worktree only where one is (File, faked). One case a line.
type vectors struct {
	Tools map[string]Tool  `json:"tools"`
	Rules map[string]Rules `json:"rules"`
	Cases []struct {
		Rules   string         `json:"rules"`
		Tool    string         `json:"tool"`
		Input   map[string]any `json:"input"`
		Rule    *string        `json:"rule"`
		Summary string         `json:"summary"`
		Ask     bool           `json:"ask"`
		// Floor: no allowance lifts the refusal. Subject and Message,
		// when given, are the refusal's.
		Floor   bool   `json:"floor"`
		Subject string `json:"subject"`
		Message string `json:"message"`
		// Links are where writes under a link land (Resolve, faked).
		Links map[string]string `json:"links"`
		// Files are the paths (absolute, clean, where they land) that are
		// regular files (File, faked).
		Files []string `json:"files"`
	} `json:"cases"`
	Messages map[string]map[string]string `json:"messages"`
}

// loadVectors reads testdata/vectors.json.
func loadVectors(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("vectors.json: %v", err)
	}
	if len(v.Cases) < 100 {
		t.Fatalf("vectors.json: only %d cases", len(v.Cases))
	}
	// The tools go with every rule set.
	for name, r := range v.Rules {
		r.Tools = v.Tools
		v.Rules[name] = r
	}
	return v
}

// linksTo is a Resolve that follows links: a path under a link (a key)
// to the same place under its target, as guard.test.ts does.
func linksTo(links map[string]string) func(string) string {
	return func(p string) string {
		for link, to := range links {
			if p == link || strings.HasPrefix(p, link+"/") {
				return to + p[len(link):]
			}
		}
		return p
	}
}

// TestVectors: the guard refuses (or asks about) each case's call with
// its rule, summary and sentence.
func TestVectors(t *testing.T) {
	v := loadVectors(t)
	for _, c := range v.Cases {
		r, ok := v.Rules[c.Rules]
		if !ok {
			t.Fatalf("no rules %q", c.Rules)
		}
		if c.Links != nil {
			r.Resolve = linksTo(c.Links)
		}
		if c.Files != nil {
			if r.Resolve == nil {
				r.Resolve = func(p string) string { return p }
			}
			resolve, files := r.Resolve, c.Files
			r.File = func(p string) bool { return slices.Contains(files, path.Clean(resolve(p))) }
		}
		d := r.Judge(c.Tool, c.Input)
		var got, want string
		if d != nil {
			got = fmt.Sprintf("%s / %s / ask %t / floor %t", d.Rule, d.Summary, d.Ask, d.Floor)
			if c.Subject != "" {
				got += " / " + d.Subject
			}
			if c.Message != "" {
				got += " / " + d.Message
			}
		}
		if c.Rule != nil {
			want = fmt.Sprintf("%s / %s / ask %t / floor %t", *c.Rule, c.Summary, c.Ask, c.Floor)
			if c.Subject != "" {
				want += " / " + c.Subject
			}
			if c.Message != "" {
				want += " / " + c.Message
			}
		}
		if got != want {
			t.Errorf("%s %s %v: got %q, want %q", c.Rules, c.Tool, c.Input, got, want)
		}
	}
	probe := map[string][2]any{
		"force-push":    {"Bash", map[string]any{"command": "git push -f"}},
		"push-default":  {"Bash", map[string]any{"command": "git push origin master"}},
		"worktree-only": {"Write", map[string]any{"file_path": "/etc/x"}},
		"delete-branch": {"Bash", map[string]any{"command": "git branch -D x"}},
		"merge":         {"Bash", map[string]any{"command": "gh pr merge 1"}},
		"credentials":   {"Bash", map[string]any{"command": "gh auth token"}},
	}
	for role, msgs := range v.Messages {
		r := v.Rules["thread"]
		r.Role = role
		for rule, p := range probe {
			d := r.Judge(p[0].(string), p[1].(map[string]any))
			if d == nil || d.Message != msgs[rule] {
				t.Errorf("%s %s: got %+v, want %q", role, rule, d, msgs[rule])
			}
		}
	}
}

// TestModMessage: a mod rule's refusal is labelled with the mod and
// gives the mod's sentence; with every core rule off it still refuses.
func TestModMessage(t *testing.T) {
	r := loadVectors(t).Rules["mods-only"]
	d := r.Judge("Bash", map[string]any{"command": "terraform destroy"})
	if d == nil || d.Message != "terminatr guard (mod:infra.no-destroy): infrastructure is the human's to change." {
		t.Fatalf("got %+v", d)
	}
	if (ModRule{Mod: "a-b", ID: "c"}).Name() != "mod:a-b.c" {
		t.Fatal("name")
	}
}

func TestCommands(t *testing.T) {
	got := Commands(`cd x && git push -f origin 'my branch'; echo "a $(gh pr merge 3) b" | cat`)
	want := [][]string{{"cd", "x"}, {"git", "push", "-f", "origin", "my branch"}, {"gh", "pr", "merge", "3"}, {"echo", "a $(gh pr merge 3) b"}, {"cat"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	if got := Commands("cat < ~/.ssh/id_rsa"); !reflect.DeepEqual(got, [][]string{{"cat", "<", "~/.ssh/id_rsa"}}) {
		t.Errorf("got %q", got)
	}
	// ${...} is part of its word; a $( ) in it is read too.
	got = Commands(`echo ${A}x ${B:-"}"} ${C:-$(gh pr merge 3)}; { ls; }`)
	want = [][]string{{"gh", "pr", "merge", "3"}, {"echo", "${A}x", `${B:-"}"}`, "${C:-$(gh pr merge 3)}"}, {"ls"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

// TestCommandsArguments: a substitution is a word of its command, read
// after it; a heredoc's body is commands only with a shell on the line.
func TestCommandsArguments(t *testing.T) {
	got := Commands("git push $(echo -f) `x` origin main")
	want := [][]string{{"git", "push", "$(echo -f)", "`x`", "origin", "main"}, {"echo", "-f"}, {"x"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	got = Commands("cat > a <<-'EOF' # c\n\tgit push -f\n\tEOF\nls")
	want = [][]string{{"cat", ">", "a", "<", "<", "EOF"}, {"ls"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	got = Commands("cat <<EOF | bash\ngh pr view $(x)\nEOF")
	want = [][]string{{"cat", "<", "<", "EOF"}, {"bash"}, {"x"}, {"gh", "pr", "view", "$(x)"}, {"x"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

// TestPSCommandsExpression: an expression given as an argument is a
// marked word of its command, its commands read after it.
func TestPSCommandsExpression(t *testing.T) {
	got := PSCommands("git push ('-f') origin; if ($x) { ls }")
	want := [][]string{{"git", "push", psExprMark + "('-f')", "origin"}, {"-f"}, {"if"}, {"$x"}, {"ls"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

// TestResolve: with Resolve, a write is judged where it lands and named
// as given.
func TestResolve(t *testing.T) {
	r := loadVectors(t).Rules["thread"]
	r.Resolve = func(p string) string { return "/etc/" + p[strings.LastIndex(p, "/link/")+len("/link/"):] }
	d := r.Judge("Write", map[string]any{"file_path": "link/hosts"})
	if d == nil || d.Rule != "worktree-only" || d.Summary != "Write of /h/.terminatr/worktrees/demo/t-0001-fix/link/hosts" {
		t.Errorf("got %+v", d)
	}
	r.Resolve = func(p string) string {
		if p != "/h/.terminatr/worktrees/demo/t-0001-fix/a/../b" {
			t.Errorf("resolved %q", p)
		}
		return p
	}
	if d := r.Judge("Edit", map[string]any{"file_path": "a/../b"}); d != nil {
		t.Errorf("got %+v", d)
	}
}

func TestPSCommandsBracedVariable(t *testing.T) {
	got := PSCommands("Write-Host ${env:A`}B}x; [string] ${env:C}")
	want := [][]string{{"Write-Host", "${env:A`}B}x"}, {"[string]", "${env:C}"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestPatchPaths(t *testing.T) {
	p := "*** Begin Patch\n*** Add File: a.txt\n+hi\n*** Update File: /w/b.go\n*** Move to: c/d.go\n@@\n-x\n+y\n*** Delete File: e \n*** End Patch"
	if got := PatchPaths(p); !reflect.DeepEqual(got, []string{"a.txt", "/w/b.go", "c/d.go", "e"}) {
		t.Errorf("got %q", got)
	}
}

// FuzzJudge: no command line panics the matcher.
// TestLimits: a command line over MaxCommand, or nested past
// maxNesting, is refused whole (unreadable, floor) whatever the rules
// allow; one just inside is judged as usual, and nesting that would
// double the parser's work at each level (heredocs in heredocs) stays
// within maxParse and returns at once.
func TestLimits(t *testing.T) {
	r := Rules{On: true, Role: "thread", Rules: []string{"merge"}, Home: "/h", Cwd: "/w", Writable: []string{"/w"},
		Tools: map[string]Tool{"Bash": {Kind: KindShell, Fields: []string{"command"}}}}
	judge := func(c string) *Denial { return r.Judge("Bash", map[string]any{"command": c}) }
	nest := func(n int) string { return strings.Repeat("echo $(", n) + "ls" + strings.Repeat(")", n) }
	if d := judge(nest(maxNesting)); d != nil {
		t.Fatalf("nested %d deep: %+v", maxNesting, d)
	}
	if d := judge("echo $(" + nest(maxNesting-2) + " && gh pr merge 1)"); d == nil || d.Rule != "merge" {
		t.Fatalf("a merge inside deep nesting: %+v", d)
	}
	for name, c := range map[string]string{
		"too deep":  nest(maxNesting + 2),
		"too long":  "echo " + strings.Repeat("a", MaxCommand),
		"backticks": strings.Repeat("echo \"$(", maxNesting+2) + "x" + strings.Repeat(")\"", maxNesting+2),
	} {
		if d := judge(c); d == nil || d.Rule != RuleUnreadable || !d.Floor || d.Ask {
			t.Errorf("%s: %+v", name, d)
		}
	}
	// No allowance lifts it, and it is refused in yolo's floor too.
	r.Allow = []Allow{{Rule: RuleUnreadable, Command: "*"}}
	if d := judge(nest(maxNesting + 2)); d == nil || d.Rule != RuleUnreadable {
		t.Errorf("allowed: %+v", d)
	}
	var doc strings.Builder
	for i := range 40 {
		fmt.Fprintf(&doc, "bash <<E%d\n$(cat <<F%d\n", i, i)
	}
	start := time.Now()
	judge(doc.String())
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("nested heredocs took %v", took)
	}
}

// TestJudgeBudget: one judge reads with one budget. Re-reads count:
// eval and sh -c nested past maxNesting, and a line whose parts are
// judged from many folders over, or one that needs many file-system
// checks, stop and refuse (unreadable) rather than run on; a judge past
// its Deadline stops and refuses (unchecked).
func TestJudgeBudget(t *testing.T) {
	r := Rules{On: true, Role: "thread", Rules: []string{"merge"}, Home: "/h", Cwd: "/w", Writable: []string{"/w"},
		Tools: map[string]Tool{"Bash": {Kind: KindShell, Fields: []string{"command"}}, "PowerShell": {Kind: KindShell, Syntax: SyntaxPowerShell, Fields: []string{"command"}}}}
	judge := func(r Rules, tool, c string) *Denial { return r.Judge(tool, map[string]any{"command": c}) }
	if d := judge(r, "Bash", strings.Repeat("eval ", 8)+"gh pr merge 1"); d == nil || d.Rule != "merge" {
		t.Fatalf("eval 8 deep: %+v", d)
	}
	if d := judge(r, "Bash", strings.Repeat("eval ", maxNesting+4)+"ls"); d == nil || d.Rule != RuleUnreadable {
		t.Errorf("eval past maxNesting: %+v", d)
	}
	// sh -c counts with eval: alone each is inside the limit.
	inner := strings.Repeat("eval ", maxNesting-2) + "ls"
	if d := judge(r, "Bash", inner); d != nil {
		t.Fatalf("eval inside the limit: %+v", d)
	}
	sh := inner
	for range 4 {
		sh = "sh -c " + strconv.Quote(sh)
	}
	if d := judge(r, "Bash", sh); d == nil || d.Rule != RuleUnreadable {
		t.Errorf("sh -c and eval past maxNesting: %+v", d)
	}
	// Each "cd || true" adds a folder the rest is judged from: the parts
	// times the folders, past the budget.
	many := strings.Repeat("cd d || true; ", 2000) + "echo " + strings.Repeat("x ", 12000)
	start := time.Now()
	if d := judge(r, "Bash", many); d == nil || d.Rule != RuleUnreadable {
		t.Errorf("many folders: %+v", d)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("many folders took %v", took)
	}
	// Each file-system check counts: a line that needs more than the
	// budget holds is refused before it makes them all, and a path is
	// checked once.
	checks := 0
	slow := r
	slow.Resolve = func(p string) string { checks++; return p }
	var cds strings.Builder
	for i := range 200 {
		fmt.Fprintf(&cds, "cd -P /d%d && cd -P /d%d && ", i, i)
	}
	if d := judge(slow, "Bash", cds.String()+"ls"); d == nil || d.Rule != RuleUnreadable || !strings.Contains(d.Message, "file-system checks") {
		t.Errorf("many checks: %+v", d)
	}
	if checks > judgeParse/fsCheck {
		t.Errorf("made %d checks", checks)
	}
	checks = 0
	if d := judge(slow, "Bash", "cd -P /d && cd -P /d && cd -P /d && ls"); d != nil || checks != 1 {
		t.Errorf("one path: %+v, %d checks", d, checks)
	}
	late := r
	late.Deadline = time.Now().Add(-time.Second)
	for _, tool := range []string{"Bash", "PowerShell"} {
		if d := judge(late, tool, "ls"); d == nil || d.Rule != RuleUnchecked || !strings.Contains(d.Message, "took too long") {
			t.Errorf("%s past its deadline: %+v", tool, d)
		}
	}
	if d := late.JudgeCommand("ls"); d == nil || d.Rule != RuleUnchecked {
		t.Errorf("JudgeCommand past its deadline: %+v", d)
	}
}

// TestCdWalk: a script that walks ten subfolders in turn (cd X || exit
// 1; …; cd ..) is judged as usual, not refused for its folders: a cd ..
// that always runs merges the folders back into their parents (cd X ||
// exit merges nothing). A write the floor holds is still found.
func TestCdWalk(t *testing.T) {
	wt := "/h/.terminatr/worktrees/demo/t-0001-fix"
	r := Rules{On: true, Role: "thread", Rules: []string{"merge", "worktree-only"}, Home: "/h", Cwd: wt, Writable: []string{wt},
		NoWrite: []string{"/h/.terminatr/projects/demo"},
		Tools:   map[string]Tool{"Bash": {Kind: KindShell, Fields: []string{"command"}}}}
	judge := func(c string) *Denial { return r.Judge("Bash", map[string]any{"command": c}) }
	var walk, line, and strings.Builder
	for _, d := range []string{"api", "cli", "web", "worker", "infra", "docs", "tools", "lib", "ops", "site"} {
		fmt.Fprintf(&walk, "cd %s || exit 1\ngo test ./...\ncd ..\n", d)
		fmt.Fprintf(&line, "cd %s || exit 1; go test ./...; cd ..; ", d)
		fmt.Fprintf(&and, "cd %s && go test ./... ; cd .. ; ", d)
	}
	for name, c := range map[string]string{"|| exit": walk.String(), "one line": line.String(), "&&": and.String()} {
		if d := judge(c); d != nil {
			t.Errorf("%s walk: %+v", name, d)
		}
		if d := judge(c + "gh pr merge 1"); d == nil || d.Rule != "merge" {
			t.Errorf("%s walk, then a merge: %+v", name, d)
		}
	}
	if d := judge(walk.String() + "cd ../../../projects/demo || exit $?\nrm TASKS.md"); d == nil || d.Rule != RuleTMFiles {
		t.Errorf("into the project folder: %+v", d)
	}
	if d := judge("cd sub || exit 1; rm ../../../../projects/demo/TASKS.md"); d == nil || d.Rule != RuleTMFiles {
		t.Errorf("from the folder the cd went to: %+v", d)
	}
}

func FuzzJudge(f *testing.F) {
	f.Add("git push -f origin 'x' && echo \"$(gh pr merge 1)\" `x` \\")
	f.Add("rm -rf ~/.terminatr/worktrees/../x; cat <~/.ssh/k")
	f.Add("& { git push -f }; $x = @\"\n$(gh pr merge 1)\n\"@ <# c #> 'a''b' \"`\"$(\" 2>&1 &")
	f.Add("cat <<-'E' <<F | sudo -u x env -S 'bash -c' # c\n\tE\n$((1<<2)) `y\nF\ngit push ('-f') @(1) $[2]")
	f.Add("timeout -s 9 5 xargs -I{} nice -n gh -R o api graphql \"$(x \"(\")\" <<")
	f.Add("|")
	f.Add("f() { cd /x; }; ln -s \"$d\" l; while :; do cp -vt l a; done; trap 'f' EXIT")
	r := Rules{On: true, Role: "thread", Rules: []string{"force-push", "push-default", "worktree-only", "delete-branch", "merge", "credentials"},
		Home: "/h", Cwd: "/w", Writable: []string{"/w"}, Worktrees: "/h/wt", Protected: []string{"main"}, Secrets: []string{"/h/.ssh"},
		Tools: map[string]Tool{"Bash": {Kind: KindShell, Fields: []string{"command"}}, "PowerShell": {Kind: KindShell, Syntax: SyntaxPowerShell, Fields: []string{"command"}},
			"apply_patch": {Kind: KindPatch, Fields: []string{"command"}}, "Glob": {Kind: KindGlob, Fields: []string{"pattern"}}},
		Mods: []ModRule{{Mod: "m", ID: "x", Commands: [][]string{{"terraform", "destroy*"}, {"kubectl"}}, Writes: []string{"/h/p"}, Reads: []string{"/etc"}, Message: "no"}}}
	f.Fuzz(func(t *testing.T, s string) {
		r.Judge("Bash", map[string]any{"command": s})
		r.Judge("PowerShell", map[string]any{"command": s})
		r.Judge("apply_patch", map[string]any{"command": s})
		r.Judge("Glob", map[string]any{"pattern": s})
	})
}
