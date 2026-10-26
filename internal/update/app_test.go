package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// infoPlist is an app's Info.plist as app.sh writes it, of version v.
func infoPlist(v string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>dev.terminatr.app</string>
	<key>Nested</key>
	<dict>
		<key>CFBundleShortVersionString</key>
		<string>9.9.9</string>
	</dict>
	<key>CFBundleShortVersionString</key>
	<string>` + v + `</string>
</dict>
</plist>
`
}

// appZip is a terminatr-app.zip whose Terminatr.app has version v.
func appZip(t testing.TB, v string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"Terminatr.app/Contents/Info.plist":          infoPlist(v),
		"Terminatr.app/Contents/MacOS/Terminatr":     "binary " + v,
		"Terminatr.app/Contents/Resources/x.bundle/": "",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func makeApp(t *testing.T, dir, v string) string {
	t.Helper()
	app := filepath.Join(dir, AppBundle)
	os.MkdirAll(filepath.Join(app, "Contents"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(infoPlist(v)), 0o644)
	return app
}

func TestPlistString(t *testing.T) {
	if got := plistString([]byte(infoPlist("0.54.0")), "CFBundleShortVersionString"); got != "0.54.0" {
		t.Errorf("version = %q", got)
	}
	if got := plistString([]byte(infoPlist("0.54.0")), "CFBundleIdentifier"); got != AppID {
		t.Errorf("identifier = %q", got)
	}
	for _, b := range []string{"", "bplist00junk", "<plist><dict><key>CFBundleShortVersionString</key><integer>1</integer></dict></plist>"} {
		if got := plistString([]byte(b), "CFBundleShortVersionString"); got != "" {
			t.Errorf("%q: %q", b, got)
		}
	}
}

func TestFindApp(t *testing.T) {
	sys, home, room := t.TempDir(), t.TempDir(), t.TempDir()
	places := []string{sys, home}
	if _, ok := FindApp(places, []string{room}); ok {
		t.Fatal("found an app in empty folders")
	}
	makeApp(t, home, "0.53.0")
	a, ok := FindApp(places, []string{room})
	if !ok || a.Method != Direct || a.Path != filepath.Join(home, AppBundle) || a.Version != "0.53.0" || a.Tag() != "v0.53.0" {
		t.Fatalf("~/Applications: %+v %v", a, ok)
	}
	// /Applications first; the cask's record makes it Homebrew's.
	makeApp(t, sys, "0.54.0")
	os.MkdirAll(filepath.Join(room, Cask, "0.54.0"), 0o755)
	a, _ = FindApp(places, []string{filepath.Join(t.TempDir(), "none"), room})
	if a.Path != filepath.Join(sys, AppBundle) || a.Method != Homebrew || a.Upgrade != "brew upgrade --cask terminatr-app" {
		t.Fatalf("cask: %+v", a)
	}
}

func TestDownloadAndInstallApp(t *testing.T) {
	if _, err := exec.LookPath("ditto"); err != nil {
		t.Skip("no ditto (macOS only)")
	}
	z := appZip(t, "0.54.0")
	sum := fmt.Sprintf("%x", sha256.Sum256(z))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zip":
			w.Write(z)
		case "/sums":
			fmt.Fprintf(w, "%s  %s\n", sum, AppArchive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	rel := Release{Tag: "v0.54.0", Assets: []Asset{{AppArchive, srv.URL + "/zip"}, {"checksums.txt", srv.URL + "/sums"}}}
	c := NewClient(func(string) string { return "" })

	dir := t.TempDir()
	old := makeApp(t, dir, "0.53.0")
	s, err := c.DownloadApp(context.Background(), rel, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(s.Dir) != dir {
		t.Errorf("staged in %s, not beside the app", s.Dir)
	}
	if got := bundleVersion(s.Path); got != "0.54.0" {
		t.Fatalf("staged version %q", got)
	}
	if err := InstallApp(s, old); err != nil {
		t.Fatal(err)
	}
	s.Remove()
	if got := bundleVersion(old); got != "0.54.0" {
		t.Fatalf("installed version %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(old, "Contents", "MacOS", "Terminatr")); string(b) != "binary 0.54.0" {
		t.Errorf("binary %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("leftovers beside the app: %v", entries)
	}
	// A fresh install, into a folder with no app yet.
	fresh := t.TempDir()
	s, err = c.DownloadApp(context.Background(), rel, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallApp(s, filepath.Join(fresh, AppBundle)); err != nil {
		t.Fatal(err)
	}
	s.Remove()
	if got := bundleVersion(filepath.Join(fresh, AppBundle)); got != "0.54.0" {
		t.Fatalf("fresh install: %q", got)
	}
	if err := InstallApp(s, filepath.Join(fresh, "Other.app")); err == nil {
		t.Error("installed under another name")
	}

	// A wrong checksum refuses before anything is unpacked.
	sum = strings.Repeat("0", 64)
	if _, err := c.DownloadApp(context.Background(), rel, dir); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("bad checksum: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("leftovers after a refusal: %v", entries)
	}
	// A release without the app (older than the app) says so.
	if _, err := c.DownloadApp(context.Background(), Release{Tag: "v0.50.0"}, dir); err == nil || !strings.Contains(err.Error(), "has no terminatr-app.zip") {
		t.Errorf("no app: %v", err)
	}
}

// TestVerifyAppRefusesUnsigned: an ad-hoc signed bundle is no Developer
// ID's, whatever else it has.
func TestVerifyAppRefusesUnsigned(t *testing.T) {
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("no codesign (macOS only)")
	}
	app := makeApp(t, t.TempDir(), "0.54.0")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	b, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Skipf("no /usr/bin/true to copy: %v", err)
	}
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "Terminatr"), b, 0o755)
	if _, err := VerifyApp(app, "ABCDE12345"); err == nil {
		t.Fatal("an unsigned app passed")
	}
	if out, err := exec.Command("codesign", "--force", "--sign", "-", "--options", "runtime", app).CombinedOutput(); err != nil {
		t.Skipf("ad-hoc signing failed: %s", out)
	}
	if _, err := VerifyApp(app, ""); err == nil || !strings.Contains(err.Error(), "Developer ID") {
		t.Fatalf("ad-hoc: %v", err)
	}
}
