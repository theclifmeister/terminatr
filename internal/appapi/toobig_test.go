package appapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestWriteTooBig (T892): a device's bridge writes no line over MaxOut.
// An answer over it answers too-big, naming its method and size; an
// event over it ends its stream so; each is logged; a line under it
// goes as it is.
func TestWriteTooBig(t *testing.T) {
	var out bytes.Buffer
	var logs []string
	b := &bridge{o: Options{MaxOut: 4 << 10, Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }},
		w: &out, open: map[int64]string{7: proto.MethodSessionFile, 8: proto.MethodTranscriptWatch}}
	big, _ := json.Marshal(strings.Repeat("x", 8<<10))
	lines := func() []Out {
		var got []Out
		for l := range bytes.SplitSeq(bytes.TrimSuffix(out.Bytes(), []byte("\n")), []byte("\n")) {
			if len(l)+1 > b.o.MaxOut {
				t.Errorf("a line of %d bytes went", len(l)+1)
			}
			var o Out
			if err := json.Unmarshal(l, &o); err != nil {
				t.Fatal(err)
			}
			got = append(got, o)
		}
		out.Reset()
		return got
	}

	if err := b.write(Out{ID: 7, Result: big}); !errors.Is(err, errTooBig) {
		t.Fatalf("write: %v", err)
	}
	got := lines()
	if len(got) != 1 || got[0].ID != 7 || got[0].Error == nil || got[0].Error.Code != ErrTooBig || got[0].End || len(got[0].Result) != 0 ||
		!strings.Contains(got[0].Error.Message, "session.file: its answer is 8213 bytes, over the 4096") {
		t.Fatalf("answer over: %+v %+v", got, got[0].Error)
	}
	if err := b.write(Out{ID: 8, Event: proto.EventTranscriptReset, Data: big}); !errors.Is(err, errTooBig) {
		t.Fatalf("write event: %v", err)
	}
	got = lines()
	if len(got) != 1 || !got[0].End || got[0].Event != "" || got[0].Error == nil || got[0].Error.Code != ErrTooBig ||
		!strings.Contains(got[0].Error.Message, "transcript.watch: its event transcript.reset is ") {
		t.Fatalf("event over: %+v", got)
	}
	if len(logs) != 2 || !strings.HasPrefix(logs[0], "session.file's answer is 8213 bytes, over the link's 4096: not sent") ||
		!strings.HasPrefix(logs[1], "transcript.watch's event transcript.reset is ") {
		t.Fatalf("logs %q", logs)
	}
	small, _ := json.Marshal("ok")
	if err := b.write(Out{ID: 7, Result: small}); err != nil {
		t.Fatal(err)
	}
	if got := lines(); len(got) != 1 || string(got[0].Result) != `"ok"` || got[0].Error != nil {
		t.Fatalf("under: %+v", got)
	}
	// The Mac app's (no MaxOut) sends it all.
	b.o.MaxOut = 0
	if err := b.write(Out{ID: 7, Result: big}); err != nil || out.Len() < 8<<10 {
		t.Fatalf("no bound: %v, %d bytes", err, out.Len())
	}
}

// TestFitText: a long text is cut at a character to fit MaxText in
// JSON, escapes counted (an HTML-escaped "<" is six bytes); a short one
// stays.
func TestFitText(t *testing.T) {
	if s, cut := fitText("short é", MaxText); s != "short é" || cut {
		t.Fatalf("short: %q %v", s, cut)
	}
	for _, unit := range []string{"a", "<é\"\n", "日本語"} {
		long := strings.Repeat(unit, (1<<20)/len(unit))
		s, cut := fitText(long, MaxText)
		if !cut || !utf8.ValidString(s) || jsonLen(s) > MaxText || jsonLen(s) < MaxText-16 || !strings.HasPrefix(long, s) {
			t.Errorf("%q: cut %v, %d bytes in JSON, valid %v", unit, cut, jsonLen(s), utf8.ValidString(s))
		}
	}
	m := memoryTextOf(proto.MemoryReadResult{Path: "memory/x.md", Text: strings.Repeat("\"q\"", 1<<20/3)})
	if !m.Truncated || jsonLen(m.Text) > MaxText {
		t.Fatalf("memory.read: truncated %v, %d", m.Truncated, jsonLen(m.Text))
	}
	if m := memoryTextOf(proto.MemoryReadResult{Path: "CONTEXT.md", Text: "# hi"}); m.Truncated || m.Text != "# hi" {
		t.Fatalf("short note: %+v", m)
	}
}

// TestChunksFit (T892): a full chunk of each method that reads a file a
// chunk at a time, in base64 in its answer's line, fits a device's
// link (MaxLine); a 1 MiB chunk didn't.
func TestChunksFit(t *testing.T) {
	name := strings.Repeat("n", 255)
	for what, res := range map[string]any{
		"session.file":      sessionFileOf(proto.SessionFileResult{Name: name, Type: "image/png", Size: proto.MaxSentFile, Sendable: true, Offset: proto.MaxSentFile - proto.SentFileChunk, Data: make([]byte, proto.SentFileChunk)}),
		"library.read":      libraryChunkOf(proto.LibraryReadResult{Name: name, Size: proto.MaxLibraryRead, Offset: proto.MaxLibraryRead - proto.LibraryChunk, Data: make([]byte, proto.LibraryChunk)}),
		"peer.library-read": libraryChunkOf(proto.LibraryReadResult{Name: name, Size: proto.PeerLibraryMaxRead, Data: make([]byte, proto.PeerLibraryChunk)}),
	} {
		raw, err := json.Marshal(safe(res))
		if err != nil {
			t.Fatal(err)
		}
		line, _ := json.Marshal(Out{ID: 1 << 40, Result: raw})
		if len(line)+1 > MaxLine*3/4 {
			t.Errorf("%s: a full chunk's line is %d bytes", what, len(line)+1)
		}
	}
}
