package moddev

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modapi"
)

// TestScaffoldsValidateAndPass: every kind's scaffold is a valid mod, and
// a code mod's own fixture passes tm mod test.
func TestScaffoldsValidateAndPass(t *testing.T) {
	for _, kind := range Kinds {
		t.Run(kind, func(t *testing.T) {
			files, err := Scaffold("my-"+kind, kind)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "my-"+kind)
			if _, err := Write(dir, files); err != nil {
				t.Fatal(err)
			}
			m, err := mod.Validate(dir)
			if err != nil {
				t.Fatalf("invalid scaffold: %v", err)
			}
			if m.IsCode() != IsCode(kind) {
				t.Fatalf("IsCode %v", m.IsCode())
			}
			if !IsCode(kind) {
				return
			}
			if files["types/tm.d.ts"] != modapi.TS() {
				t.Error("types/tm.d.ts isn't this tm's")
			}
			res, err := RunDir(dir)
			if err != nil || len(res) != 1 || !res[0].OK {
				t.Fatalf("tm mod test: %v %+v", err, res)
			}
		})
	}
}

func TestScaffoldRefuses(t *testing.T) {
	if _, err := Scaffold("Bad_ID", "hook"); err == nil {
		t.Error("a bad id")
	}
	if _, err := Scaffold("ok", "widget"); err == nil || !strings.Contains(err.Error(), "column, hook") {
		t.Errorf("an unknown kind: %v", err)
	}
	dir := t.TempDir()
	if _, err := Write(dir, map[string]string{"a": "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, map[string]string{"b": "2"}); err == nil {
		t.Error("wrote into a folder that isn't empty")
	}
}

func TestTitle(t *testing.T) {
	if got := Title("review-age"); got != "Review age" {
		t.Errorf("Title = %q", got)
	}
}
