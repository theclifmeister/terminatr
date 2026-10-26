package cli

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/update"
)

// appZip is a terminatr-app.zip whose Terminatr.app has version v.
func appZip(t testing.TB, v string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("Terminatr.app/Contents/Info.plist")
	w.Write([]byte(plistOf(v)))
	w, _ = zw.Create("Terminatr.app/Contents/MacOS/Terminatr")
	w.Write([]byte("app " + v))
	zw.Close()
	return buf.Bytes()
}

func plistOf(v string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleShortVersionString</key><string>` + v + `</string></dict></plist>
`
}

// withApp gives the rig a Mac app: places are two empty folders (like
// /Applications and ~/Applications), with the app installed at version
// v in the first unless v is empty, by the cask when cask.
func (r *updateRig) withApp(t *testing.T, v string, cask bool) (places []string) {
	t.Helper()
	places = []string{t.TempDir(), t.TempDir()}
	room := t.TempDir()
	if v != "" {
		app := filepath.Join(places[0], update.AppBundle, "Contents")
		os.MkdirAll(app, 0o755)
		os.WriteFile(filepath.Join(app, "Info.plist"), []byte(plistOf(v)), 0o644)
	}
	if cask {
		os.MkdirAll(filepath.Join(room, update.Cask), 0o755)
	}
	r.u.AppPlaces = places
	r.u.FindApp = func() (update.App, bool) { return update.FindApp(places, []string{room}) }
	r.u.VerifyApp = func(string, string) (string, error) { return "ABCDE12345", nil }
	return places
}

func appVersion(dir string) string {
	a, _ := update.FindApp([]string{dir}, nil)
	return a.Version
}

func needDitto(t *testing.T) {
	if _, err := exec.LookPath("ditto"); err != nil {
		t.Skip("no ditto (macOS only)")
	}
}

func TestUpdateAppMissing(t *testing.T) {
	needDitto(t)
	r := newUpdateRig(t, "v0.2.0", "v0.2.0", "bin", false)
	r.u.Version = "v0.2.0"
	places := r.withApp(t, "", false)
	// Without --app it only says how.
	code, out, _ := r.run(t, "", "--yes")
	if code != ExitOK || !strings.Contains(out, "tm update --app installs it") || appVersion(places[0]) != "" {
		t.Fatalf("no --app: %d %q", code, out)
	}
	// --app installs into the first writable place.
	code, out, errs := r.run(t, "", "--yes", "--app")
	if code != ExitOK || appVersion(places[0]) != "0.2.0" || !strings.Contains(out, "signature  ok") {
		t.Fatalf("--app: %d\n%s%s", code, out, errs)
	}
	if entries, _ := os.ReadDir(places[0]); len(entries) != 1 {
		t.Errorf("leftovers beside the app: %v", entries)
	}
	// A Homebrew tm gets the cask.
	r = newUpdateRig(t, "v0.2.0", "v0.2.0", "Cellar/terminatr/0.2.0/bin", false)
	r.u.Version = "v0.2.0"
	r.withApp(t, "", false)
	if code, _, _ := r.run(t, "", "--yes", "--app"); code != ExitOK || len(r.brews) != 1 || strings.Join(r.brews[0], " ") != "install --cask "+update.CaskFull {
		t.Fatalf("brew tm, --app: %d %v", code, r.brews)
	}
}

func TestUpdateAppDirect(t *testing.T) {
	needDitto(t)
	r := newUpdateRig(t, "v0.2.0", "v0.2.0", "bin", false)
	places := r.withApp(t, "0.1.0", false)
	// tm and the app, both.
	code, out, errs := r.run(t, "", "--yes")
	if code != ExitOK || appVersion(places[0]) != "0.2.0" || !strings.Contains(out, "quit and reopen") {
		t.Fatalf("update: %d\n%s%s", code, out, errs)
	}
	if b, _ := os.ReadFile(r.exe); !strings.Contains(string(b), "v0.2.0") {
		t.Fatal("tm not updated")
	}
	// Up to date now.
	r.u.Version = "v0.2.0"
	if code, out, _ := r.run(t, "", "--yes"); code != ExitOK || !strings.Contains(out, "Terminatr.app 0.2.0 is the latest release") {
		t.Fatalf("current: %d %q", code, out)
	}
}

func TestUpdateAppRefusesBadSignature(t *testing.T) {
	needDitto(t)
	r := newUpdateRig(t, "v0.2.0", "v0.2.0", "bin", false)
	r.u.Version = "v0.2.0"
	places := r.withApp(t, "0.1.0", false)
	var gotTeam string
	r.u.Team = "ABCDE12345"
	r.u.VerifyApp = func(_, team string) (string, error) {
		gotTeam = team
		return "", errors.New(`signed by team "OTHER", want "ABCDE12345"`)
	}
	code, _, errs := r.run(t, "", "--yes")
	if code != ExitRefused || !strings.Contains(errs, "fails the signature check") || gotTeam != "ABCDE12345" {
		t.Fatalf("bad signature: %d %q (team %q)", code, errs, gotTeam)
	}
	if appVersion(places[0]) != "0.1.0" {
		t.Fatal("app replaced after a failed check")
	}
	if entries, _ := os.ReadDir(places[0]); len(entries) != 1 {
		t.Errorf("leftovers beside the app: %v", entries)
	}
	// Not a terminal, no --yes: refused, nothing changed.
	r.u.VerifyApp = func(string, string) (string, error) { return "ABCDE12345", nil }
	if code, _, errs := r.run(t, ""); code != ExitRefused || !strings.Contains(errs, "--yes") || appVersion(places[0]) != "0.1.0" {
		t.Fatalf("no --yes: %d %q", code, errs)
	}
}

func TestUpdateAppCask(t *testing.T) {
	r := newUpdateRig(t, "v0.2.0", "v0.2.0", "bin", false)
	r.u.Version = "v0.2.0"
	r.withApp(t, "0.1.0", true)
	code, out, _ := r.run(t, "")
	if code != ExitOK || !strings.Contains(out, "brew upgrade --cask terminatr-app") || len(r.brews) != 0 {
		t.Fatalf("cask, not asked: %d %v %q", code, r.brews, out)
	}
	if code, _, _ := r.run(t, "", "--yes"); code != ExitOK || len(r.brews) != 1 || strings.Join(r.brews[0], " ") != "upgrade --cask terminatr-app" {
		t.Fatalf("cask --yes: %d %v", code, r.brews)
	}
}

func TestUpdateAppCheck(t *testing.T) {
	r := newUpdateRig(t, "v0.2.0", "v0.2.0", "bin", false)
	r.withApp(t, "0.1.0", false)
	code, out, _ := r.run(t, "", "--check")
	if code != ExitOK || !strings.Contains(out, "app        0.1.0 (direct, ") || !strings.Contains(out, "Terminatr.app): tm update") {
		t.Fatalf("check: %d %q", code, out)
	}
	code, out, _ = r.run(t, "", "--check", "--json")
	if code != ExitOK || !strings.Contains(out, `"app": {`) || !strings.Contains(out, `"version": "0.1.0"`) || !strings.Contains(out, `"installed": true`) {
		t.Fatalf("check --json: %d %q", code, out)
	}
	r.withApp(t, "", false)
	if code, out, _ := r.run(t, "", "--check"); code != ExitOK || !strings.Contains(out, "app        not installed") {
		t.Fatalf("check, no app: %d %q", code, out)
	}
	// Source builds leave the app alone.
	r.u.Channel = ""
	r.withApp(t, "0.1.0", false)
	if code, _, _ := r.run(t, "", "--yes"); code != ExitRefused {
		t.Fatalf("source build: %d", code)
	}
}
