package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// TranscriptFiles is a [transcript]'s files: what a conversation puts
// in front of the user (docs/SPEC.md §3.3, Transcript). A call of Tool
// sends the files its input's Paths name (a list of paths, or one),
// shown as its input's Display says ("render", "attach"); a prompt's
// blocks of kind ImageBlock are images pasted into it, their media type
// and base64 bytes at ImageType and ImageData. session.file serves a
// file only as the transcript's line names it here.
type TranscriptFiles struct {
	Tool       string `toml:"tool"`
	Paths      string `toml:"paths"`
	Display    string `toml:"display"`
	ImageBlock string `toml:"image_block"`
	ImageType  string `toml:"image_type"`
	ImageData  string `toml:"image_data"`
}

func (f *TranscriptFiles) validate() error {
	var errs []error
	if (f.Tool == "") != (f.Paths == "") {
		errs = append(errs, errors.New("files: tool and paths go together"))
	}
	if (f.ImageBlock == "") != (f.ImageData == "") {
		errs = append(errs, errors.New("files: image_block and image_data go together"))
	}
	return errors.Join(errs...)
}

// MaxSentFiles bounds the files of one item.
const MaxSentFiles = 20

// maxPath bounds a sent file's path, as the item names it.
const maxPath = 4096

// sentFileTypes are the media types session.file sends, by extension:
// images, PDFs, text and common documents, media and archives. A file
// of any other name (keys, certificates, env files, binaries) it
// doesn't (.key is a Keynote deck's and a private key's: never).
var sentFileTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".heic": "image/heic", ".heif": "image/heif", ".svg": "image/svg+xml",
	".tif": "image/tiff", ".tiff": "image/tiff", ".bmp": "image/bmp",
	".pdf": "application/pdf",
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".tsv": "text/tab-separated-values",
	".log": "text/plain", ".json": "application/json", ".yaml": "application/yaml", ".yml": "application/yaml",
	".toml": "application/toml", ".xml": "application/xml", ".html": "text/html", ".diff": "text/x-diff",
	".patch": "text/x-diff",
	".docx":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx":  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".mp3":   "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav",
	".mp4": "video/mp4", ".mov": "video/quicktime",
	".zip": "application/zip",
}

// embeddedImageTypes are the media types of a pasted image session.file
// sends.
var embeddedImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// SentFileType is the media type session.file sends a file of name as,
// or "" for a name it doesn't send: of another type, or a dot file.
func SentFileType(name string) string {
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".") {
		return ""
	}
	return sentFileTypes[strings.ToLower(filepath.Ext(base))]
}

// filesOfCall is the files a call's input sends, and how they show; nil
// when the call isn't t's files tool.
func (f *TranscriptFiles) filesOfCall(tool string, input any, block int) ([]proto.TranscriptFile, string) {
	if f == nil || f.Tool == "" || tool != f.Tool {
		return nil, ""
	}
	in, _ := input.(map[string]any)
	if in == nil {
		return nil, ""
	}
	var paths []string
	switch v, _ := lookup(in, f.Paths); v := v.(type) {
	case string:
		paths = []string{v}
	case []any:
		for _, p := range v {
			if s, ok := p.(string); ok {
				paths = append(paths, s)
			}
		}
	}
	files := []proto.TranscriptFile{}
	for i, p := range paths[:min(len(paths), MaxSentFiles)] {
		p = textsafe.Line(p, maxPath)
		if p == "" {
			continue
		}
		files = append(files, proto.TranscriptFile{
			Ref:  fmt.Sprintf("%d.%d", block, i),
			Name: textsafe.Line(path.Base(filepath.ToSlash(p)), proto.TranscriptSummary),
			Path: p,
			Type: SentFileType(p),
		})
	}
	display := ""
	if f.Display != "" {
		display, _ = lookupString(in, f.Display)
		display = textsafe.Line(display, proto.TranscriptSummary)
	}
	return files, display
}

// imageOf is a prompt's image block as a file, the n'th of the line;
// false for one of a type session.file doesn't send, or without bytes.
func (f *TranscriptFiles) imageOf(obj map[string]any, block, n int) (proto.TranscriptFile, bool) {
	typ := ""
	if f.ImageType != "" {
		typ, _ = lookupString(obj, f.ImageType)
	}
	data, _ := lookupString(obj, f.ImageData)
	if !slices.Contains(embeddedImageTypes, typ) || data == "" {
		return proto.TranscriptFile{}, false
	}
	return proto.TranscriptFile{Ref: fmt.Sprintf("%d.0", block), Name: fmt.Sprintf("Image #%d", n), Type: typ}, true
}

// LineFile is the file a transcript line names at block (its index among
// the line's blocks) and n (the n'th path of a call): a sent file's
// Path, or a pasted image's Data and Type.
type LineFile struct {
	Path string
	Data []byte
	Type string
}

// ErrNoLineFile: the line names no file there.
var ErrNoLineFile = errors.New("the transcript names no such file")

// FileAt is the file line names at block and n, as Map's items give it
// (their files' Ref): only a call of the files tool's path, or a pasted
// image's bytes, of a conversation line.
func (t *Transcript) FileAt(line []byte, block, n int) (LineFile, error) {
	f := t.Files
	obj, blocks := t.lineBlocks(line)
	if f == nil || obj == nil || block < 0 || block >= len(blocks) || n < 0 {
		return LineFile{}, ErrNoLineFile
	}
	b, ok := blocks[block].(map[string]any)
	if !ok {
		return LineFile{}, ErrNoLineFile
	}
	kind, _ := lookupString(b, t.BlockKind)
	rules := t.rulesOf(obj)
	if f.ImageBlock != "" && kind == f.ImageBlock && n == 0 && slices.ContainsFunc(rules, isPrompt) {
		img, ok := f.imageOf(b, block, 1)
		s, _ := lookupString(b, f.ImageData)
		data, err := base64.StdEncoding.DecodeString(s)
		if !ok || err != nil {
			return LineFile{}, ErrNoLineFile
		}
		return LineFile{Data: data, Type: img.Type}, nil
	}
	r := t.rule(rules, kind, b, "")
	if r == nil || r.Kind != proto.ItemTool {
		return LineFile{}, ErrNoLineFile
	}
	name, _ := lookupString(b, r.Name)
	in, _ := lookup(b, r.Input)
	files, _ := f.filesOfCall(name, in, block)
	ref := fmt.Sprintf("%d.%d", block, n)
	for _, sf := range files {
		if sf.Ref == ref {
			return LineFile{Path: sf.Path}, nil
		}
	}
	return LineFile{}, ErrNoLineFile
}

func isPrompt(r *TranscriptRule) bool { return r.Kind == proto.ItemPrompt }
