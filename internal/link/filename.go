package link

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// MaxFileName bounds a file's name over a link, in bytes.
const MaxFileName = 100

// reserved are the names Windows keeps for devices, whatever follows
// the first dot.
var reserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CLOCK$": true}

func init() {
	for i := 1; i <= 9; i++ {
		reserved["COM"+strconv.Itoa(i)] = true
		reserved["LPT"+strconv.Itoa(i)] = true
	}
}

// nameRune reports whether r may be in a file's name over a link:
// letters, digits, . _ - + and space.
func nameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-+ ", r)
}

// CleanName is the name a file goes by over a link (docs/SPEC.md
// §16.13): only the base name, made safe to show (textsafe.Line), each
// other rune as _, no leading dot, dash or space, no "..", at most
// MaxFileName bytes and no Windows device name. A name that cleans to
// nothing is "file". The sender cleans the name it offers; the
// receiver refuses one that isn't clean already.
func CleanName(name string) string {
	// Each step can undo another's work at the edges (a cut that leaves
	// a dot at the front): again until nothing changes.
	for range 4 {
		next := cleanOnce(name)
		if next == name {
			break
		}
		name = next
	}
	return name
}

func cleanOnce(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if nameRune(r) {
			return r
		}
		return '_'
	}, textsafe.Line(strings.ToValidUTF8(name, ""), 0))
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	name = strings.TrimRight(strings.TrimLeft(name, ".- "), ". ")
	if len(name) > MaxFileName {
		ext := filepath.Ext(name)
		if len(ext) > 16 || len(ext) >= len(name) {
			ext = ""
		}
		stem := name[:len(name)-len(ext)]
		cut := MaxFileName - len(ext)
		for cut > 0 && !utf8.RuneStart(stem[cut]) {
			cut--
		}
		name = strings.TrimRight(stem[:cut], ". ") + ext
	}
	stem, _, _ := strings.Cut(name, ".")
	if reserved[strings.ToUpper(strings.TrimSpace(stem))] {
		name = "_" + name
		if len(name) > MaxFileName {
			name = strings.ToValidUTF8(name[:MaxFileName], "")
		}
	}
	if name == "" {
		return "file"
	}
	return name
}

// numbered is name with -n before its extension: report-2.pdf.
func numbered(name string, n int) string {
	ext := filepath.Ext(name)
	if ext == name || len(ext) > 16 {
		ext = ""
	}
	s := "-" + strconv.Itoa(n)
	stem := name[:len(name)-len(ext)]
	for len(stem)+len(s)+len(ext) > MaxFileName && stem != "" {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	if stem = strings.TrimRight(stem, ". "); stem == "" {
		stem = "file"
	}
	return CleanName(stem + s + ext)
}
