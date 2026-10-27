package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

var updateTranscripts = flag.Bool("update-transcripts", false, "rewrite the transcripts' golden items in testdata/transcripts/")

// claudeTranscript is the built-in Claude manifest's [transcript].
func claudeTranscript(t *testing.T) *Transcript {
	t.Helper()
	m := builtinManifest(t, "claude")
	if m.Transcript == nil {
		t.Fatal("the claude manifest has no [transcript]")
	}
	return m.Transcript
}

func builtinManifest(t *testing.T, name string) *Manifest {
	t.Helper()
	b, ok := Builtin(name)
	if !ok {
		t.Fatalf("no built-in manifest %q", name)
	}
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestTranscriptDrift reads the recorded Claude transcripts (real lines,
// their strings scrubbed: testdata/transcripts) through the manifest's
// table: every conversation block is known, every version tested, and
// the items are the golden ones. A Claude release that moves the format
// on gets a fixture recorded from it; a block or line the table doesn't
// know fails here, as it shows unmapped to the app.
func TestTranscriptDrift(t *testing.T) {
	tr := claudeTranscript(t)
	files, _ := filepath.Glob("testdata/transcripts/claude-*.jsonl")
	if len(files) == 0 {
		t.Fatal("no recorded transcripts")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			kinds := map[string]int{}
			sc := bufio.NewScanner(bytes.NewReader(data))
			sc.Buffer(nil, 16<<20)
			for n := 1; sc.Scan(); n++ {
				l := tr.Map(sc.Bytes())
				if l.Unknown > 0 {
					t.Errorf("line %d: %d of %d blocks unknown: %.200s", n, l.Unknown, l.Units, sc.Text())
				}
				if !tr.TranscriptVersion(l.Version) {
					t.Errorf("line %d: version %q isn't tested", n, l.Version)
				}
				for _, it := range l.Items {
					kinds[it.Kind]++
					b, _ := json.Marshal(it)
					got.Write(append(b, '\n'))
				}
			}
			for _, k := range []string{proto.ItemPrompt, proto.ItemText, proto.ItemTool, proto.ItemResult} {
				if kinds[k] == 0 {
					t.Errorf("no %s items: %v", k, kinds)
				}
			}
			golden := strings.TrimSuffix(f, ".jsonl") + ".items"
			if *updateTranscripts {
				if err := os.WriteFile(golden, got.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (go test -run TestTranscriptDrift -update-transcripts writes it)", err)
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Errorf("items differ from %s (-update-transcripts rewrites it):\n%s", golden, got.String())
			}
		})
	}
}

// TestTranscriptMap: what each kind of line becomes, clean and bounded.
func TestTranscriptMap(t *testing.T) {
	tr := claudeTranscript(t)
	long := strings.Repeat("x", proto.TranscriptItemText+10)
	cases := []struct {
		name, line string
		want       []proto.TranscriptItem
	}{
		{"typed prompt", `{"type":"user","isSidechain":false,"message":{"role":"user","content":"fix the \u001b[31mbug\u001b[0m\u202e"},"version":"2.1.310"}`,
			[]proto.TranscriptItem{{Kind: "prompt", Text: "fix the [31mbug[0m"}}},
		{"timed prompt", `{"type":"user","timestamp":"2026-10-26T20:43:05.123Z","message":{"content":"go"}}`,
			[]proto.TranscriptItem{{Kind: "prompt", Text: "go", At: time.Date(2026, 10, 26, 20, 43, 5, 123e6, time.UTC)}}},
		{"long prompt", `{"type":"user","message":{"content":"` + long + `"}}`,
			[]proto.TranscriptItem{{Kind: "prompt", Text: long[:proto.TranscriptItemText] + "…", Truncated: true}}},
		{"prompt as blocks", `{"type":"user","message":{"content":[{"type":"text","text":"look"},{"type":"image","source":{}}]}}`,
			[]proto.TranscriptItem{{Kind: "prompt", Text: "look"}}},
		{"pasted image", `{"type":"user","message":{"content":[{"type":"text","text":"see"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0K"}}]}}`,
			[]proto.TranscriptItem{{Kind: "prompt", Text: "see", Files: []proto.TranscriptFile{{Ref: "1.0", Name: "Image #1", Type: "image/png"}}}}},
		{"image alone", `{"type":"user","message":{"content":[{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"/9j/"}},{"type":"image","source":{"type":"base64","media_type":"image/x-icon","data":"AAAB"}}]}}`,
			[]proto.TranscriptItem{{Kind: "prompt", Files: []proto.TranscriptFile{{Ref: "0.0", Name: "Image #1", Type: "image/jpeg"}}}}},
		{"sent files", `{"type":"assistant","message":{"content":[{"type":"text","text":"Here."},{"type":"tool_use","id":"toolu_3","name":"SendUserFile","input":{"files":["/tmp/s/shot.png","","/tmp/s/report.pdf","/home/u/.ssh/id_rsa","/tmp/s/notes.KEY"],"caption":"The \u001b[1mshot\u001b[0m","display":"attach"}}]}}`,
			[]proto.TranscriptItem{{Kind: "text", Text: "Here."}, {Kind: "tool", Tool: "SendUserFile", Call: "toolu_3", Text: "The [1mshot[0m",
				Files: []proto.TranscriptFile{{Ref: "1.0", Name: "shot.png", Path: "/tmp/s/shot.png", Type: "image/png"},
					{Ref: "1.2", Name: "report.pdf", Path: "/tmp/s/report.pdf", Type: "application/pdf"},
					{Ref: "1.3", Name: "id_rsa", Path: "/home/u/.ssh/id_rsa"}, {Ref: "1.4", Name: "notes.KEY", Path: "/tmp/s/notes.KEY"}},
				Display: "attach"}}},
		{"one sent file", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_4","name":"SendUserFile","input":{"files":"/tmp/a.gif"}}]}}`,
			[]proto.TranscriptItem{{Kind: "tool", Tool: "SendUserFile", Call: "toolu_4", Files: []proto.TranscriptFile{{Ref: "0.0", Name: "a.gif", Path: "/tmp/a.gif", Type: "image/gif"}}}}},
		{"interrupted", `{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`,
			[]proto.TranscriptItem{{Kind: "note", Text: "Interrupted"}}},
		{"command", `{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`, nil},
		{"task notice", `{"type":"user","promptSource":"system","message":{"content":"<task-notification>done</task-notification>"}}`, nil},
		{"meta", `{"type":"user","isMeta":true,"message":{"content":[{"type":"text","text":"Base directory for this skill"}]}}`, nil},
		{"sidechain", `{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent"}]}}`, nil},
		{"text", `{"type":"assistant","message":{"content":[{"type":"text","text":"Done.\n\nAll tests pass."}]}}`,
			[]proto.TranscriptItem{{Kind: "text", Text: "Done.\n\nAll tests pass."}}},
		{"thinking", `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm"}]}}`, nil},
		{"bash call", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./...\nmake vet","description":"x"}}]}}`,
			[]proto.TranscriptItem{{Kind: "tool", Tool: "Bash", Call: "toolu_1", Text: "go test ./... make vet"}}},
		{"mcp call", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"mcp__x__y","input":{"a":"b"}}]}}`,
			[]proto.TranscriptItem{{Kind: "tool", Tool: "mcp__x__y", Call: "toolu_2"}}},
		{"result", `{"type":"user","message":{"content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"\nok  \tpkg\t1.2s\nok  \tother\n","is_error":false}]}}`,
			[]proto.TranscriptItem{{Kind: "result", Call: "toolu_1", Text: "ok pkg 1.2s", Lines: 3}}},
		{"error result as blocks", `{"type":"user","message":{"content":[{"tool_use_id":"toolu_2","type":"tool_result","content":[{"type":"text","text":"Exit code 1"},{"type":"text","text":"FAIL"}],"is_error":true}]}}`,
			[]proto.TranscriptItem{{Kind: "result", Call: "toolu_2", Text: "Exit code 1", Lines: 2, Error: true}}},
		{"not conversation", `{"type":"system","subtype":"turn_duration","durationMs":5}`, nil},
	}
	for _, c := range cases {
		l := tr.Map([]byte(c.line))
		if l.Unknown != 0 {
			t.Errorf("%s: %d unknown", c.name, l.Unknown)
		}
		got, _ := json.Marshal(l.Items)
		want, _ := json.Marshal(c.want)
		if string(got) != string(want) {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want)
		}
	}
}

// TestTranscriptUnmapped: what the core reports as the format moving on
// (the app then shows the terminal): a block kind the table doesn't
// know, content of another shape, a line that isn't JSON, a version
// outside tested.
func TestTranscriptUnmapped(t *testing.T) {
	tr := claudeTranscript(t)
	for _, line := range []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_call","name":"Bash"}]}}`,
		`{"type":"assistant","message":{"parts":[{"type":"text","text":"moved"}]}}`,
		`{"type":"user","message":{"content":{"text":"an object"}}}`,
		`{"type":"assistant","message":{"content":[`,
	} {
		if l := tr.Map([]byte(line)); l.Unknown == 0 {
			t.Errorf("%s: read as %+v, want unknown", line, l)
		}
	}
	if tr.TranscriptVersion("3.0.0") || !tr.TranscriptVersion("2.1.999") || !tr.TranscriptVersion("") {
		t.Error("tested versions")
	}
}

// TestTranscriptValidate: a table the core can't run is refused.
func TestTranscriptValidate(t *testing.T) {
	bad := []Transcript{
		{},
		{Content: "c", BlockKind: "type", Items: []TranscriptRule{{Kind: "bogus", Match: map[string]string{"a": "b"}, Block: "text", Text: "t"}}},
		{Content: "c", BlockKind: "type", Items: []TranscriptRule{{Kind: "tool", Match: map[string]string{"a": "b"}, Block: "x"}}},
		{Content: "c", BlockKind: "type", Items: []TranscriptRule{{Kind: "text", Match: map[string]string{"a": "b"}, Block: "x"}}},
		{Content: "c", BlockKind: "type", Tested: []string{"2."}, Items: []TranscriptRule{{Kind: "skip", Match: map[string]string{"a": "b"}, Block: "x"}}},
	}
	for i, tr := range bad {
		if tr.validate() == nil {
			t.Errorf("%d: %+v validated", i, tr)
		}
	}
	if m := builtinManifest(t, "codex"); m.Transcript != nil {
		t.Error("codex has a [transcript]: the conversation view is Claude's only for now")
	}
}

// TestTranscriptFileAt: a line names a file only as Map's items give
// it: a SendUserFile call's path, a prompt's pasted image; nothing else
// of the line, nor of a line that isn't the conversation's.
func TestTranscriptFileAt(t *testing.T) {
	tr := claudeTranscript(t)
	sent := `{"type":"assistant","message":{"content":[{"type":"text","text":"/etc/passwd"},{"type":"tool_use","id":"t","name":"SendUserFile","input":{"files":["/tmp/a.png","","/tmp/b.pdf"],"other":"/etc/hosts"}},{"type":"tool_use","id":"u","name":"Read","input":{"file_path":"/tmp/c.png"}}]}}`
	for _, c := range []struct {
		block, n int
		want     string
	}{{1, 0, "/tmp/a.png"}, {1, 2, "/tmp/b.pdf"}, {1, 1, ""}, {1, 3, ""}, {0, 0, ""}, {2, 0, ""}, {3, 0, ""}, {-1, 0, ""}, {1, -1, ""}} {
		f, err := tr.FileAt([]byte(sent), c.block, c.n)
		if f.Path != c.want || (err == nil) != (c.want != "") || f.Data != nil {
			t.Errorf("%d.%d: %+v %v, want %q", c.block, c.n, f, err, c.want)
		}
	}
	side := strings.Replace(sent, `"type":"assistant"`, `"type":"assistant","isSidechain":true`, 1)
	if f, err := tr.FileAt([]byte(side), 1, 0); err == nil {
		t.Errorf("a subagent's call: %+v", f)
	}
	img := `{"type":"user","message":{"content":[{"type":"text","text":"see"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}},{"type":"image","source":{"type":"base64","media_type":"image/x-icon","data":"aGk="}}]}}`
	if f, err := tr.FileAt([]byte(img), 1, 0); err != nil || string(f.Data) != "hi" || f.Type != "image/png" || f.Path != "" {
		t.Errorf("image: %+v %v", f, err)
	}
	for _, at := range [][2]int{{1, 1}, {2, 0}, {0, 0}} {
		if f, err := tr.FileAt([]byte(img), at[0], at[1]); err == nil {
			t.Errorf("image %v: %+v", at, f)
		}
	}
	// An image in an assistant's line isn't a pasted one.
	asst := strings.Replace(img, `"type":"user"`, `"type":"assistant"`, 1)
	if f, err := tr.FileAt([]byte(asst), 1, 0); err == nil {
		t.Errorf("assistant image: %+v", f)
	}
	if _, err := tr.FileAt([]byte("not json"), 0, 0); err == nil {
		t.Error("not json")
	}
}

// TestSentFileType: the types session.file sends, by name; never a dot
// file, a key or a binary.
func TestSentFileType(t *testing.T) {
	for name, want := range map[string]string{
		"/a/shot.PNG": "image/png", "x.jpeg": "image/jpeg", "r.pdf": "application/pdf", "n.md": "text/markdown",
		"/a/.env": "", "/a/.secret.png": "", "id_rsa": "", "k.pem": "", "k.key": "", "a.out": "", "x.sh": "", "x": "",
	} {
		if got := SentFileType(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}
