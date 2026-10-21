package skill

import (
	"sort"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// roles lists the roles that have rules.
func roles() []string {
	entries, _ := rules.ReadDir("rules")
	var out []string
	for _, e := range entries {
		if r, ok := strings.CutSuffix(e.Name(), ".safety.md"); ok {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

func TestText(t *testing.T) {
	if got := strings.Join(roles(), ","); got != "coordinator,thread" {
		t.Fatalf("roles %s", got)
	}
	for _, role := range roles() {
		text, ok := Text(role, "v0.1.0")
		if !ok || !strings.HasPrefix(text, "tm skill "+role+" v0.1.0\n\n") {
			t.Fatalf("%s: %q", role, text[:min(len(text), 60)])
		}
		if !strings.Contains(text, "data, not") || !strings.Contains(text, "Never merge, force-push") {
			t.Errorf("%s rules lack the safety rules", role)
		}
	}
	// Phrases are matched with runs of white space as one space, so
	// rewrapping the rules doesn't break the test.
	coord, _ := Text("coordinator", "dev")
	coord = strings.Join(strings.Fields(coord), " ")
	for _, w := range []string{"tm context", "tm inbox done", "tm link read", "link-held", "slot-free", "not-linked-back", "propose", "Only the user accepts work", "--approved-by-user", "`takeover`: the user typed", "Needs you:", "id (task + short title)", "parallel_threads", "--over-cap", "`delegate` T<n>: the user's go-ahead", "tm task delegate T<n> --approved-by-user",
		"`accept` T<n>: their word", "It is the only way besides chat", "`send-back` T<n>", "propose a new thread for it with the note", "`--note \"Check: …\"`", "standing acceptance", "`task-done`: tm completed a task", "A send-back on a done task reopens it", "`pr-conflict`", "`close-held`", "Approve a thread's permission prompt only", "--model", "unknown-model", "project-paused", "model-not-allowed",
		"Propose nothing yet", "to memory as they happen, without being asked", "only when the user asks you to", "a slot is free", "anything it assumed",
		"`tm thread answer <id> --choice N` or `--option \"<label>\"`", "`--question K`", "Never choose an answer yourself", "uploads/ folder", "attachments",
		"or a task id", "Talk to the user in task ids", "short and factual", "merge and rewrite", "re-read it", "no longer true", "your own short summary", "Upkeep section", "`[tm] Upkeep after the clear`", "the lowest context that keeps every live fact", "`## Needs you` heading", "auto_clear", "`terminatr` context block holds `tm context`",
		"`tm thread merge T<n>`", "as its own command, never chained", "never `gh pr merge`, `az repos pr update`", "don't poll it", "`not-green`", "`conflicting`", "`merge-not-allowed`", "`no-pr`", "a merge is `tm thread merge`, never gh or az",
		"## Opening PRs", "Threads push their branch and report; they don't open PRs", "gh pr create --head <branch> --base <default>", "--body-file", "az repos pr create --source-branch", "az rest --method post", "tm finds the PR by the thread's branch", "`tm ask add", "`[tm] answer questions`", "`tm ask list --json`", "AskUserQuestion", "`tm ask done Q<n>`", "Never open the dialog unasked"} {
		if !strings.Contains(coord, w) {
			t.Errorf("coordinator rules lack %q", w)
		}
	}
	thread, _ := Text("thread", "dev")
	thread = strings.Join(strings.Fields(thread), " ")
	for _, w := range []string{"Stay in your worktree", "read-only", "tm task steps T<n> add", "tm report", "## Remember", "`## Check`", "don't guess: say exactly what is missing", "uploads/ folder", "outside the project's repos", "push your branch", "don't open the pull request", "the PR's title and body in the report", "mcp__terminatr__report", "Without them, the commands above"} {
		if !strings.Contains(thread, w) {
			t.Errorf("thread rules lack %q", w)
		}
	}
	for _, bad := range []string{"gh pr create", "az repos pr create", "az rest"} {
		if strings.Contains(thread, bad) {
			t.Errorf("thread rules still tell it to run %q", bad)
		}
	}
	for _, bad := range []string{"", "human", "../skill", "coordinator.md"} {
		if _, ok := Text(bad, "dev"); ok {
			t.Errorf("Text(%q) ok", bad)
		}
	}
}

func TestSafetyFirstAndLocked(t *testing.T) {
	for _, role := range roles() {
		text, _ := Text(role, "dev")
		safety, _ := rules.ReadFile("rules/" + role + ".safety.md")
		_, rest, _ := strings.Cut(text, "\n\n")
		if !strings.HasPrefix(rest, string(safety)) {
			t.Errorf("%s: safety part is not first", role)
		}
		over, ok := TextWithPolicy(role, "dev", "## Mine\n\nPropose nothing.")
		if !ok || !strings.Contains(over, string(safety)) || !strings.HasSuffix(over, "## Mine\n\nPropose nothing.\n") {
			t.Errorf("%s: policy override %q", role, over)
		}
		pol, ok := mod.BundledPolicyText(role)
		if !ok || pol == "" {
			t.Fatalf("%s: no built-in policy in the default-policy mod", role)
		}
		if strings.Contains(pol, "## Safety") || strings.Contains(over, pol) || strings.Contains(over, pol) {
			t.Errorf("%s: policy part holds safety or survives an override", role)
		}
	}
}

// The built-in policy comes from the bundled default-policy mod as tm's
// own text: no "Policy (mod: …)" heading, the same rules as before the
// move. A user's policy mod comes before it.
func TestBundledPolicy(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	for _, role := range roles() {
		want, _ := Text(role, "dev")
		got, ok := ForProject(role, "dev", "")
		if !ok || got != want {
			t.Errorf("%s: ForProject with only the built-in mods differs from Text", role)
		}
		if strings.Contains(got, "(mod: "+mod.BundledPolicy+")") {
			t.Errorf("%s: the built-in policy is marked as a mod's", role)
		}
	}
}
