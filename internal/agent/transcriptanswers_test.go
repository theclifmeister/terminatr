package agent

import (
	"bufio"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestTranscriptAnswersRecorded reads an answered AskUserQuestion as
// Claude 2.1.316 writes it (testdata/transcripts/answered-*: a real call and result,
// its strings scrubbed, only the fields around them kept): the call
// shows as a tool, its result carries the question and the option
// picked.
func TestTranscriptAnswersRecorded(t *testing.T) {
	tr := claudeTranscript(t)
	f, err := os.Open("testdata/transcripts/answered-claude-2.1.316.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var items []proto.TranscriptItem
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		l := tr.Map(sc.Bytes())
		if l.Unknown > 0 || !tr.TranscriptVersion(l.Version) {
			t.Errorf("%d blocks unknown, version %q", l.Unknown, l.Version)
		}
		items = append(items, l.Items...)
	}
	if len(items) != 2 || items[0].Kind != proto.ItemTool || items[0].Tool != "AskUserQuestion" || items[1].Kind != proto.ItemResult {
		t.Fatalf("items = %+v", items)
	}
	want := []proto.TranscriptAnswer{{
		Header:   "Status bar",
		Question: "The status bar sits above the tab bar while anything works ('4 working · T1'). How should it go?",
		Picked:   []string{"Remove it entirely (Recommended)"},
	}}
	if got := items[1].Answers; !reflect.DeepEqual(got, want) || items[1].Call != items[0].Call {
		t.Errorf("answers = %+v, want %+v", got, want)
	}
}

// TestTranscriptAnswers: a multi-select's labels, the user's own words,
// notes, the text alone (no toolUseResult), and results that are no
// answered menu.
func TestTranscriptAnswers(t *testing.T) {
	tr := claudeTranscript(t)
	result := func(text, extra string) string {
		return `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"tool_result","content":` + text + `,"tool_use_id":"toolu_9"` + extra + `}]}`
	}
	qs := `"questions":[` +
		`{"question":"Which parts?","header":"Parts","multiSelect":true,"options":[{"label":"Server"},{"label":"Mac app"},{"label":"Mac app, iPhone"}]},` +
		`{"question":"Name?","header":"Name","multiSelect":false,"options":[{"label":"tm"},{"label":"terminatr"}]},` +
		`{"question":"Skipped?","header":"Skip","options":[{"label":"a"}]}]`
	cases := []struct {
		name, line string
		want       []proto.TranscriptAnswer
	}{
		{"multi and typed",
			result(`"Your questions have been answered: \"Which parts?\"=\"Mac app, iPhone, Server, docs too\", \"Name?\"=\"Termy\". You can now continue with these answers in mind."`, "") +
				`,"toolUseResult":{` + qs + `,"answers":{"Which parts?":"Mac app, iPhone, Server, docs too","Name?":"Termy"},"annotations":{"Name?":{"notes":"short \u001b[1mplease\u001b[0m"}}}}`,
			[]proto.TranscriptAnswer{
				{Header: "Parts", Question: "Which parts?", Picked: []string{"Mac app, iPhone", "Server"}, Typed: "docs too"},
				{Header: "Name", Question: "Name?", Typed: "Termy", Notes: "short [1mplease[0m"},
			}},
		{"text only",
			result(`"User has answered your questions: \"Pick one, \"x\"?\"=\"A, B\", \"Why?\"=\"Because \"so\".\". You can now continue."`, "") + `}`,
			[]proto.TranscriptAnswer{
				{Question: `Pick one, "x"?`, Picked: []string{"A, B"}},
				{Question: "Why?", Picked: []string{`Because "so".`}},
			}},
		{"declined", result(`"The user doesn't want to proceed with this tool use."`, `,"is_error":true`) + `}`, nil},
		{"error with the prefix", result(`"Your questions have been answered: \"Q\"=\"A\"."`, `,"is_error":true`) + `}`, nil},
		{"no pairs", result(`"Your questions have been answered: but this is Bash output"`, "") + `}`, nil},
		{"other tool", result(`"ok"`, "") + `,"toolUseResult":{` + qs + `,"answers":{"Name?":"tm"}}}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := tr.Map([]byte(c.line))
			if len(l.Items) != 1 || l.Items[0].Kind != proto.ItemResult {
				t.Fatalf("items = %+v", l.Items)
			}
			if got := l.Items[0].Answers; !reflect.DeepEqual(got, c.want) {
				t.Errorf("answers = %#v\nwant %#v", got, c.want)
			}
		})
	}
}

func TestSplitAnswer(t *testing.T) {
	labels := []string{"A", "A, B", "C"}
	for _, c := range []struct {
		ans    string
		multi  bool
		picked []string
		typed  string
	}{
		{"A", false, []string{"A"}, ""},
		{"A, B", false, []string{"A, B"}, ""},
		{"mine", false, nil, "mine"},
		{"A, B, C", true, []string{"A, B", "C"}, ""},
		{"C, A", true, []string{"C", "A"}, ""},
		{"C, my own, words", true, []string{"C"}, "my own, words"},
		{" ", true, nil, ""},
	} {
		picked, typed := splitAnswer(c.ans, labels, c.multi)
		if !reflect.DeepEqual(picked, c.picked) || typed != c.typed {
			t.Errorf("splitAnswer(%q, %v) = %q, %q; want %q, %q", c.ans, c.multi, picked, typed, c.picked, c.typed)
		}
	}
	if picked, _ := splitAnswer("x", nil, false); !reflect.DeepEqual(picked, []string{"x"}) {
		t.Errorf("no labels: %q", picked)
	}
	long := strings.Repeat("q", proto.TranscriptAnswerText+50)
	if got := answersOfText(`"` + long + `"="a".`); len(got) != 1 || len([]rune(got[0].Question)) > proto.TranscriptAnswerText+1 {
		t.Errorf("long question not bounded: %+v", got)
	}
}
