package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// filesManifest is conv with a tool call and the files table.
var filesManifest = convManifest + `
[[transcript.items]]
kind = "tool"
match = { type = "assistant" }
block = "tool_use"
id = "id"
name = "name"
input = "input"
[transcript.files]
tool = "SendUserFile"
paths = "files"
display = "display"
image_block = "image"
image_type = "source.media_type"
image_data = "source.data"
`

func setupFiles(t *testing.T) (client.Paths, *client.Client) {
	t.Helper()
	p := testPaths(t)
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	m := []byte(fmt.Sprintf(filesManifest, testSh(t)))
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "conv.toml"), m, 0o600); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return p, c
}

func sendLine(paths ...string) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "t", "name": "SendUserFile", "input": map[string]any{"files": paths, "caption": "here"}},
	}})
	return string(b) + "\n"
}

// fileErr is a session.file call's error code, "" for none.
func fileErr(c *client.Client, id, ref string, stat bool) (proto.SessionFileResult, string) {
	var res proto.SessionFileResult
	err := c.Call(proto.MethodSessionFile, proto.SessionFileParams{Session: id, Ref: ref, Stat: stat}, &res)
	var perr *proto.Error
	if errors.As(err, &perr) {
		return res, perr.Code
	}
	if err != nil {
		return res, err.Error()
	}
	return res, ""
}

// readAll reads a file by its ref, chunk by chunk.
func readAll(t *testing.T, c *client.Client, id, ref string) ([]byte, int) {
	t.Helper()
	var out []byte
	chunks := 0
	for {
		var res proto.SessionFileResult
		call(t, c, proto.MethodSessionFile, proto.SessionFileParams{Session: id, Ref: ref, Offset: int64(len(out))}, &res)
		if res.Offset != int64(len(out)) || len(res.Data) > proto.SentFileChunk {
			t.Fatalf("chunk at %d: offset %d, %d bytes", len(out), res.Offset, len(res.Data))
		}
		out = append(out, res.Data...)
		chunks++
		if int64(len(out)) >= res.Size || len(res.Data) == 0 {
			return out, chunks
		}
	}
}

// TestSessionFile: session.file serves what the transcript's line names
// at a ref, a sent file's bytes in chunks or a pasted image's, and
// refuses everything else: a ref the transcript doesn't give, a link to
// another type, a directory or FIFO, a dot file or key, a file over the
// cap, a relative path; a file or line no longer there is gone.
func TestSessionFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks and FIFOs")
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return f
	}
	png := write("shot.png", []byte("\x89PNG small"))
	big := bytes.Repeat([]byte("0123456789abcdef"), (proto.SentFileChunk*5/2)/16)
	pdf := write("report.pdf", big)
	secret := write("secret.txt", []byte("SECRET"))
	key := write("id_rsa", []byte("KEY"))
	dot := write(".env", []byte("TOKEN=1"))
	huge := write("huge.png", nil)
	os.Truncate(huge, proto.MaxSentFile+1)
	gone := write("gone.png", []byte("x"))
	other := filepath.Join(t.TempDir(), "elsewhere.png")
	os.WriteFile(other, []byte("ELSEWHERE"), 0o600)
	linkOK := filepath.Join(dir, "link.png")
	os.Symlink(other, linkOK)
	linkSecret := filepath.Join(dir, "disguised.png")
	os.Symlink(secret, linkSecret)
	sub := filepath.Join(dir, "sub.png")
	os.Mkdir(sub, 0o700)
	fifo := filepath.Join(dir, "pipe.png")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	traversal := dir + "/sub.png/../shot.png"

	imgLine := `{"type":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` +
		base64.StdEncoding.EncodeToString([]byte("PASTED")) + `"}}]}` + "\n"
	lines := `{"type":"user","content":"show me"}` + "\n" +
		sendLine(png, pdf, secret, key, dot, huge, gone, linkOK, linkSecret, sub, fifo, traversal, "rel/x.png") +
		`{"type":"assistant","content":[{"type":"tool_use","id":"r","name":"Read","input":{"files":["` + secret + `"]}}]}` + "\n" +
		imgLine
	p, c := setupFiles(t)
	id, tf := startConv(t, p, c, lines)

	_, _, events := watchTranscript(t, p, id)
	ev := nextEvent(t, events)
	refs := map[string]string{}
	var image string
	for _, it := range ev.Items {
		for _, f := range it.Files {
			if f.Path != "" {
				refs[f.Path] = f.Ref
			} else {
				image = f.Ref
			}
		}
		if it.Tool == "Read" && len(it.Files) > 0 {
			t.Errorf("a Read call names files: %+v", it)
		}
	}
	if len(refs) != 13 || image == "" {
		t.Fatalf("items %+v", ev.Items)
	}

	if got, n := readAll(t, c, id, refs[png]); string(got) != "\x89PNG small" || n != 1 {
		t.Errorf("png %q in %d", got, n)
	}
	if got, n := readAll(t, c, id, refs[pdf]); !bytes.Equal(got, big) || n != 3 {
		t.Errorf("pdf %d bytes in %d chunks", len(got), n)
	}
	if got, _ := readAll(t, c, id, refs[linkOK]); string(got) != "ELSEWHERE" {
		t.Errorf("link %q", got)
	}
	if got, _ := readAll(t, c, id, refs[traversal]); string(got) != "\x89PNG small" {
		t.Errorf("traversal %q", got)
	}
	if got, _ := readAll(t, c, id, image); string(got) != "PASTED" {
		t.Errorf("pasted image %q", got)
	}
	if res, code := fileErr(c, id, refs[png], true); code != "" || !res.Sendable || res.Type != "image/png" || res.Size != 10 ||
		res.Name != "shot.png" || res.MTime.IsZero() || res.Data != nil {
		t.Errorf("stat %+v %s", res, code)
	}

	// Stat answers a file it doesn't send, without its bytes.
	for _, f := range []string{key, dot, huge, linkSecret} {
		res, code := fileErr(c, id, refs[f], true)
		if code != "" || res.Sendable || res.Data != nil {
			t.Errorf("stat %s: %+v %s", filepath.Base(f), res, code)
		}
		if res, code := fileErr(c, id, refs[f], false); code != proto.ErrRefused || res.Data != nil {
			t.Errorf("read %s: %+v %s", filepath.Base(f), res, code)
		}
	}
	// A text file the call names is sent; through a link named .png,
	// it isn't.
	if got, _ := readAll(t, c, id, refs[secret]); string(got) != "SECRET" {
		t.Errorf("secret.txt %q", got)
	}
	if res, _ := fileErr(c, id, refs[linkSecret], true); res.Type != "" || res.Size != 6 {
		t.Errorf("disguised.png: %+v", res)
	}
	for _, f := range []string{sub, fifo, "rel/x.png"} {
		if res, code := fileErr(c, id, refs[f], true); code != proto.ErrRefused {
			t.Errorf("%s: %+v %s", f, res, code)
		}
	}

	// Gone: the file, or the line naming it.
	os.Remove(gone)
	if _, code := fileErr(c, id, refs[gone], false); code != proto.ErrFileGone {
		t.Errorf("gone: %s", code)
	}

	// Refs the transcript didn't give.
	parts := strings.Split(refs[png], ".")
	for _, ref := range []string{
		parts[0] + "." + parts[1] + ".1.0",                // the line has one block
		parts[0] + "." + parts[1] + ".0.99",               // no such file
		parts[0] + "." + strings.Repeat("0", 16) + ".0.0", // another line's sum
		"0." + parts[1] + "." + parts[2] + "." + parts[3], // another line
		"1." + parts[1] + "." + parts[2] + "." + parts[3], // inside a line
	} {
		if res, code := fileErr(c, id, ref, false); code != proto.ErrFileGone || res.Data != nil {
			t.Errorf("ref %s: %+v %s", ref, res, code)
		}
	}
	// The Read call's line, at its own offset and sum.
	readOff := int64(strings.Index(lines, `{"type":"assistant","content":[{"type":"tool_use","id":"r"`))
	readLine := lines[readOff : readOff+int64(strings.IndexByte(lines[readOff:], '\n'))]
	if res, code := fileErr(c, id, lineRef([]byte(readLine), readOff)+"0.0", false); code != proto.ErrFileGone || res.Data != nil {
		t.Errorf("a Read call's path: %+v %s", res, code)
	}
	for _, ref := range []string{"", "x", "1.2.3", "-1.0123456789abcdef.0.0", "0.0123456789abcdef.-1.0", "0.short.0.0"} {
		if _, code := fileErr(c, id, ref, false); code != proto.ErrBadParams {
			t.Errorf("bad ref %q: %s", ref, code)
		}
	}
	if _, code := fileErr(c, "mini/s-1", refs[png], false); code == "" {
		t.Error("a paired server's session")
	}
	if _, code := fileErr(c, "s-nope", refs[png], false); code != proto.ErrUnknownSession {
		t.Errorf("unknown session: %s", code)
	}

	// The line rewritten: its refs are gone.
	os.WriteFile(tf, []byte(strings.Replace(lines, `"here"`, `"HERE"`, 1)), 0o600)
	if _, code := fileErr(c, id, refs[png], false); code != proto.ErrFileGone {
		t.Errorf("rewritten: %s", code)
	}
}
