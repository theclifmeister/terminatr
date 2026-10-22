package link

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanName(t *testing.T) {
	long := strings.Repeat("a", 120) + ".pdf"
	for in, want := range map[string]string{
		"report.pdf":            "report.pdf",
		"T12/report.pdf":        "report.pdf",
		`C:\x\y\report.pdf`:     "report.pdf",
		"../../etc/passwd":      "passwd",
		"..":                    "file",
		"":                      "file",
		".hidden":               "hidden",
		"-rf":                   "rf",
		"a..b":                  "a.b",
		"my report (final).pdf": "my report _final_.pdf",
		"evil\u202egpj.exe":     "evilgpj.exe",
		"bell\x07.txt":          "bell.txt",
		"tab\tname":             "tab name",
		"CON":                   "_CON",
		"nul.txt":               "_nul.txt",
		"com1.tar.gz":           "_com1.tar.gz",
		"console.txt":           "console.txt",
		"Übersicht ä+b.md":      "Übersicht ä+b.md",
		"trailing. ":            "trailing",
		long:                    strings.Repeat("a", 96) + ".pdf",
		"$(rm -rf).sh":          "__rm -rf_.sh",
		"a/b/":                  "file",
		"name\nwith\nlines.txt": "name with lines.txt",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNumbered(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"report.pdf", 2, "report-2.pdf"},
		{"notes", 3, "notes-3"},
		{strings.Repeat("b", 98) + ".md", 2, strings.Repeat("b", 95) + "-2.md"},
	} {
		if got := numbered(tc.in, tc.n); got != tc.want || CleanName(got) != got {
			t.Errorf("numbered(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// FuzzCleanName: a clean name is clean (the receiver's check passes on
// what any sender cleans), short, one base name and never hidden.
func FuzzCleanName(f *testing.F) {
	for _, s := range []string{"report.pdf", "../x", "CON.txt", ". . . .pdf", strings.Repeat("é", 80) + ".txt", "a\u200fb", "..\\..\\x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := CleanName(in)
		if CleanName(got) != got {
			t.Fatalf("CleanName(%q) = %q, not stable: %q", in, got, CleanName(got))
		}
		if got == "" || len(got) > MaxFileName || !utf8.ValidString(got) || strings.ContainsAny(got, "/\\\x00") ||
			strings.HasPrefix(got, ".") || strings.HasPrefix(got, "-") || strings.Contains(got, "..") {
			t.Fatalf("CleanName(%q) = %q", in, got)
		}
		for i := 2; i < 4; i++ {
			if n := numbered(got, i); CleanName(n) != n {
				t.Fatalf("numbered(%q) = %q, not clean", got, n)
			}
		}
	})
}
