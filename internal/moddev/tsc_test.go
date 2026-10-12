package moddev

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/modapi"
)

// TestTypesCompileStrict runs tsc --strict on the code scaffolds and the
// mod API's examples (internal/modapi/testdata) against this tm's
// tm.d.ts, so a schema change that breaks a mod's types fails CI. It
// needs TypeScript: CI's mod-types job sets TERMINATR_TSC_CHECK=1 with
// tsc on PATH.
func TestTypesCompileStrict(t *testing.T) {
	if os.Getenv("TERMINATR_TSC_CHECK") == "" {
		t.Skip("TERMINATR_TSC_CHECK unset")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{}
	for _, kind := range Kinds {
		if !IsCode(kind) {
			continue
		}
		files, err := Scaffold("x-"+kind, kind)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(t.TempDir(), kind)
		if _, err := Write(dir, files); err != nil {
			t.Fatal(err)
		}
		dirs["scaffold "+kind] = dir
	}
	examples, err := filepath.Glob(filepath.Join("..", "modapi", "testdata", "*", "main.ts"))
	if err != nil || len(examples) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	// terminatr's examples/mods, the built-in code mods' sources among them.
	shipped, err := filepath.Glob(filepath.Join("..", "..", "examples", "mods", "*", "main.ts"))
	if err != nil || len(shipped) == 0 {
		t.Fatalf("no examples/mods: %v", err)
	}
	examples = append(examples, shipped...)
	for _, main := range examples {
		src, err := os.ReadFile(main)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		files := map[string]string{"main.ts": string(src), "types/tm.d.ts": modapi.TS(), "tsconfig.json": tsconfig}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := Write(dir, files); err != nil {
			t.Fatal(err)
		}
		dirs["example "+filepath.Base(filepath.Dir(main))] = dir
	}
	for name, dir := range dirs {
		out, err := exec.Command(tsc, "-p", filepath.Join(dir, "tsconfig.json")).CombinedOutput()
		if err != nil {
			t.Errorf("%s: tsc --strict: %v\n%s", name, err, out)
		}
	}
}
