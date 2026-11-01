package agent

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Transcript is a manifest's [transcript]: how the file [jsonl_tail]
// follows reads as a conversation (docs/SPEC.md §3.3, Transcript), for
// transcript.watch. The format is the agent's own and changes between
// its versions, so the table says which it was checked on, and the core
// reports a line it can't read rather than guess (TranscriptLine.Unknown).
type Transcript struct {
	// Tested are version prefixes the table was checked on ("2."), and
	// VersionField the line's path that gives the agent's version: a
	// transcript of another version is unmapped. Empty Tested takes
	// every version.
	Tested       []string `toml:"tested"`
	VersionField string   `toml:"version_field"`
	// TimeField is the line's path that gives when it was written
	// (RFC 3339): each of its items' At.
	TimeField string `toml:"time_field"`
	// Content is the path of a conversation line's blocks: a list of
	// objects, or a string that reads as one block of StringBlock's
	// kind. BlockKind is the path of a block's kind.
	Content     string `toml:"content"`
	BlockKind   string `toml:"block_kind"`
	StringBlock string `toml:"string_block"`
	// Ignore are block kinds that show nothing (thinking).
	Ignore []string         `toml:"ignore"`
	Items  []TranscriptRule `toml:"items"`
	// Files are the files the conversation puts in front of the user.
	Files *TranscriptFiles `toml:"files"`
	// Answers reads an answered question menu's result.
	Answers *TranscriptAnswers `toml:"answers"`
	// ToolArgs is the field of a call's input that its folded line
	// shows, by tool name ("Bash" = "command"); "*" for any other tool.
	ToolArgs map[string]string `toml:"tool_args"`
}

// TranscriptRule maps a block of a line to an item. A line is a
// conversation line when some rule's Match takes it; each of its blocks
// becomes the item of the first rule that takes both, or nothing for a
// rule of kind "skip". Text, ID, Name, Input and Error are the block's
// paths.
type TranscriptRule struct {
	Kind  string            `toml:"kind"` // prompt, text, tool, result, note or skip
	Match map[string]string `toml:"match"`
	Block string            `toml:"block"` // the block's kind
	// Prefix: the block's text starts with one of them.
	Prefix []string `toml:"prefix"`
	Text   string   `toml:"text"` // a string, or a list of blocks with a "text"
	// Note is a note's text, in place of the block's.
	Note  string `toml:"note"`
	ID    string `toml:"id"`
	Name  string `toml:"name"`
	Input string `toml:"input"`
	Error string `toml:"error"`
}

var transcriptKinds = []string{proto.ItemPrompt, proto.ItemText, proto.ItemTool, proto.ItemResult, proto.ItemNote, "skip"}

func (t *Transcript) validate() error {
	var errs []error
	if t.Content == "" || t.BlockKind == "" || len(t.Items) == 0 {
		errs = append(errs, errors.New("needs content, block_kind and items"))
	}
	if len(t.Tested) > 0 && t.VersionField == "" {
		errs = append(errs, errors.New("tested needs version_field"))
	}
	for i, r := range t.Items {
		switch {
		case !slices.Contains(transcriptKinds, r.Kind):
			errs = append(errs, fmt.Errorf("items[%d]: unknown kind %q", i, r.Kind))
		case len(r.Match) == 0 || r.Block == "":
			errs = append(errs, fmt.Errorf("items[%d]: needs match and block", i))
		case r.Kind == proto.ItemTool && r.Name == "":
			errs = append(errs, fmt.Errorf("items[%d]: a tool needs name", i))
		case (r.Kind == proto.ItemPrompt || r.Kind == proto.ItemText || r.Kind == proto.ItemResult) && r.Text == "":
			errs = append(errs, fmt.Errorf("items[%d]: %s needs text", i, r.Kind))
		case len(r.Prefix) > 0 && r.Text == "":
			errs = append(errs, fmt.Errorf("items[%d]: prefix needs text", i))
		}
	}
	if t.Files != nil {
		if err := t.Files.validate(); err != nil {
			errs = append(errs, err)
		}
	}
	if t.Answers != nil {
		if err := t.Answers.validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// TranscriptVersion says whether version is one the table was checked
// on; "" (a line without one) is.
func (t *Transcript) TranscriptVersion(version string) bool {
	if version == "" || len(t.Tested) == 0 {
		return true
	}
	for _, p := range t.Tested {
		if strings.HasPrefix(version, p) {
			return true
		}
	}
	return false
}

// TranscriptLine is what one line of a transcript says.
type TranscriptLine struct {
	Items []proto.TranscriptItem
	// Version is the agent's version the line gives.
	Version string
	// Units counts the line's conversation blocks (a line that isn't
	// JSON counts one), Unknown those no rule or Ignore knows: the
	// format moved on.
	Units, Unknown int
}

// Map reads one line. Its items' strings are clean (textsafe) and
// bounded (proto.TranscriptItemText, proto.TranscriptSummary). The files
// its items put in front of the user (Files) have a Ref that names them
// in the line, for FileAt.
func (t *Transcript) Map(line []byte) TranscriptLine {
	var out TranscriptLine
	var obj map[string]any
	if json.Unmarshal(line, &obj) != nil {
		if len(strings.TrimSpace(string(line))) > 0 {
			out.Units, out.Unknown = 1, 1
		}
		return out
	}
	if t.VersionField != "" {
		out.Version, _ = lookupString(obj, t.VersionField)
	}
	rules := t.rulesOf(obj)
	if len(rules) == 0 {
		return out // not a conversation line
	}
	var at time.Time
	if t.TimeField != "" {
		if s, ok := lookupString(obj, t.TimeField); ok {
			at, _ = time.Parse(time.RFC3339Nano, s)
		}
	}
	blocks, ok := t.blocksOf(obj)
	if !ok {
		out.Units, out.Unknown = 1, 1
		return out
	}
	lineObj := obj
	var images []proto.TranscriptFile
	prompt := -1
	for i, b := range blocks {
		out.Units++
		kind, obj, str := t.StringBlock, map[string]any(nil), ""
		switch b := b.(type) {
		case string:
			str = b
		case map[string]any:
			obj = b
			kind, _ = lookupString(b, t.BlockKind)
		default:
			out.Unknown++
			continue
		}
		if f := t.Files; f != nil && f.ImageBlock != "" && kind == f.ImageBlock && obj != nil && slices.ContainsFunc(rules, isPrompt) {
			if img, ok := f.imageOf(obj, i, len(images)+1); ok && len(images) < MaxSentFiles {
				images = append(images, img)
			}
			continue
		}
		r := t.rule(rules, kind, obj, str)
		if r == nil {
			if !slices.Contains(t.Ignore, kind) {
				out.Unknown++
			}
			continue
		}
		if it, ok := t.item(r, obj, str, i); ok {
			it.At = at
			if it.Kind == proto.ItemResult && !it.Error {
				it.Answers = t.Answers.answersOf(lineObj, blockText(obj, str, r.Text))
			}
			if it.Kind == proto.ItemPrompt {
				prompt = len(out.Items)
			}
			out.Items = append(out.Items, it)
		}
	}
	if len(images) > 0 {
		// Pasted images go with the line's prompt, or are one.
		if prompt < 0 {
			prompt = len(out.Items)
			out.Items = append(out.Items, proto.TranscriptItem{Kind: proto.ItemPrompt, At: at})
		}
		out.Items[prompt].Files = images
	}
	return out
}

// rulesOf are the rules whose Match takes a line's obj: none for a line
// that isn't a conversation line.
func (t *Transcript) rulesOf(obj map[string]any) []*TranscriptRule {
	var rules []*TranscriptRule
	for i := range t.Items {
		if matches(t.Items[i].Match, obj) {
			rules = append(rules, &t.Items[i])
		}
	}
	return rules
}

// blocksOf are a line's blocks; false when its content is neither a
// list nor a string that reads as one block.
func (t *Transcript) blocksOf(obj map[string]any) ([]any, bool) {
	switch c, _ := lookup(obj, t.Content); c := c.(type) {
	case string:
		if t.StringBlock == "" {
			return nil, false
		}
		return []any{c}, true
	case []any:
		return c, true
	}
	return nil, false
}

// lineBlocks is a line as an object and its blocks; a nil object for
// one that isn't JSON.
func (t *Transcript) lineBlocks(line []byte) (map[string]any, []any) {
	var obj map[string]any
	if json.Unmarshal(line, &obj) != nil || obj == nil {
		return nil, nil
	}
	blocks, _ := t.blocksOf(obj)
	return obj, blocks
}

// rule is the first of rules that takes a block of kind (obj, or the
// string str).
func (t *Transcript) rule(rules []*TranscriptRule, kind string, obj map[string]any, str string) *TranscriptRule {
	for _, r := range rules {
		if r.Block != kind {
			continue
		}
		if len(r.Prefix) > 0 {
			text := strings.TrimSpace(blockText(obj, str, r.Text))
			if !slices.ContainsFunc(r.Prefix, func(p string) bool { return strings.HasPrefix(text, p) }) {
				continue
			}
		}
		return r
	}
	return nil
}

// item is r's item of a block, the line's block'th; false for a skip, or
// a block with nothing to show.
func (t *Transcript) item(r *TranscriptRule, obj map[string]any, str string, block int) (proto.TranscriptItem, bool) {
	it := proto.TranscriptItem{Kind: r.Kind}
	field := func(path string) string {
		if obj == nil || path == "" {
			return ""
		}
		s, _ := lookupString(obj, path)
		return s
	}
	switch r.Kind {
	case "skip":
		return it, false
	case proto.ItemNote:
		it.Text = textsafe.Line(cmp.Or(r.Note, blockText(obj, str, r.Text)), proto.TranscriptSummary)
	case proto.ItemPrompt, proto.ItemText:
		text := strings.TrimSpace(textsafe.Text(blockText(obj, str, r.Text)))
		if text == "" {
			return it, false
		}
		it.Text = textsafe.Cut(text, proto.TranscriptItemText)
		it.Truncated = len(it.Text) != len(text)
	case proto.ItemTool:
		it.Tool = textsafe.Line(field(r.Name), proto.TranscriptSummary)
		it.Call = textsafe.Line(field(r.ID), proto.TranscriptSummary)
		if in, ok := lookup(obj, r.Input); ok && obj != nil {
			arg, ok := t.ToolArgs[field(r.Name)]
			if !ok {
				arg = t.ToolArgs["*"]
			}
			if m, isMap := in.(map[string]any); isMap && arg != "" {
				s, _ := lookupString(m, arg)
				it.Text = textsafe.Line(s, proto.TranscriptSummary)
			}
			it.Files, it.Display = t.Files.filesOfCall(field(r.Name), in, block)
		}
	case proto.ItemResult:
		it.Call = textsafe.Line(field(r.ID), proto.TranscriptSummary)
		it.Error = field(r.Error) == "true"
		text := strings.TrimRight(blockText(obj, str, r.Text), "\n")
		if text != "" {
			it.Lines = strings.Count(text, "\n") + 1
		}
		for l := range strings.SplitSeq(text, "\n") {
			if l = textsafe.Line(l, proto.TranscriptSummary); l != "" {
				it.Text = l
				break
			}
		}
	}
	return it, true
}

// blockText is the text at path of a block, obj or the string str: a
// string, or the "text" of each block of a list, joined by newlines.
func blockText(obj map[string]any, str, path string) string {
	if obj == nil {
		return str
	}
	v, ok := lookup(obj, path)
	if !ok {
		return ""
	}
	switch v := v.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, b := range v {
			if m, ok := b.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// TranscriptTodos is the todo list as a todos item: clean, bounded
// (proto.TranscriptTodos entries).
func TranscriptTodos(todos []Todo) proto.TranscriptItem {
	it := proto.TranscriptItem{Kind: proto.ItemTodos, Todos: []proto.TranscriptTodo{}}
	for _, td := range todos[:min(len(todos), proto.TranscriptTodos)] {
		it.Todos = append(it.Todos, proto.TranscriptTodo{
			Text:   textsafe.Line(td.Text, proto.TranscriptSummary),
			Status: textsafe.Line(string(td.Status), proto.TranscriptSummary),
		})
	}
	return it
}
