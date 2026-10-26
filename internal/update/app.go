package update

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The Mac app (macos/, docs/SPEC.md §3.8) as releases ship it
// (scripts/release/app.sh) and the tap's cask installs it
// (scripts/release/cask.sh). `tm update` on macOS updates it beside tm:
// with brew when the cask installed it, else from the release's zip,
// after the same checks as tm's binary and Gatekeeper's notarisation.
const (
	// AppBundle is the app's folder name.
	AppBundle = "Terminatr.app"
	// AppArchive is the release asset holding AppBundle, stapled.
	AppArchive = "terminatr-app.zip"
	// AppID is the app's bundle identifier, which its signature names.
	AppID = "dev.terminatr.app"
	// Cask is the tap's cask of the app.
	Cask = "terminatr-app"
	// CaskFull names the cask with its tap, for brew install.
	CaskFull = "theclifmeister/terminatr/" + Cask
)

// App is an installed Terminatr.app.
type App struct {
	// Method is Homebrew when the cask installed it, else Direct.
	Method Method `json:"method"`
	Path   string `json:"path"`
	// Version is its CFBundleShortVersionString ("0.54.0"); empty when
	// Info.plist doesn't say.
	Version string `json:"version,omitempty"`
	// Upgrade is the command that upgrades a cask install.
	Upgrade string `json:"upgrade,omitempty"`
}

// Tag is the release tag of the app's version, "" when unknown.
func (a App) Tag() string {
	if a.Version == "" {
		return ""
	}
	return "v" + a.Version
}

// AppPlaces are the folders an app is looked for in, in order:
// /Applications, then ~/Applications. A new install goes into the first
// one that is writable.
func AppPlaces(home string) []string {
	return []string{"/Applications", filepath.Join(home, "Applications")}
}

// CaskRooms are Homebrew's Caskroom folders: Apple silicon's, then Intel's.
var CaskRooms = []string{"/opt/homebrew/Caskroom", "/usr/local/Caskroom"}

// FindApp looks for AppBundle in places; caskRooms say whether the cask
// installed it (Homebrew moves the app into /Applications and keeps its
// record in <Caskroom>/terminatr-app).
func FindApp(places, caskRooms []string) (App, bool) {
	for _, dir := range places {
		p := filepath.Join(dir, AppBundle)
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			continue
		}
		a := App{Method: Direct, Path: p, Version: bundleVersion(p)}
		for _, room := range caskRooms {
			if fi, err := os.Stat(filepath.Join(room, Cask)); err == nil && fi.IsDir() {
				a.Method = Homebrew
				a.Upgrade = "brew upgrade --cask " + Cask
				break
			}
		}
		return a, true
	}
	return App{}, false
}

// bundleVersion reads CFBundleShortVersionString from an app's XML
// Info.plist (app.sh writes XML; check.sh requires it).
func bundleVersion(app string) string {
	b, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	return plistString(b, "CFBundleShortVersionString")
}

// plistString is the <string> after <key>key</key> in the top dict of an
// XML property list, or "".
func plistString(b []byte, key string) string {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	depth := 0
	next := false // the last element of the top dict was <key>key</key>
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			// plist > dict > key|string: depth 3.
			if depth != 3 {
				continue
			}
			var v string
			if err := d.DecodeElement(&v, &t); err != nil {
				return ""
			}
			depth--
			switch {
			case t.Name.Local == "key":
				next = v == key
			case next && t.Name.Local == "string":
				return strings.TrimSpace(v)
			default:
				next = false
			}
		case xml.EndElement:
			depth--
		}
	}
}

// StagedApp is a downloaded app in a folder of its own beside where it
// will go.
type StagedApp struct {
	// Dir is the staging folder; Remove deletes it.
	Dir string
	// Path is the app in it.
	Path string
}

// Remove deletes the staging folder and whatever is still in it.
func (s StagedApp) Remove() { os.RemoveAll(s.Dir) }

// DownloadApp fetches AppArchive and checksums.txt from r, checks the
// archive's sha256, and unpacks it with ditto (which keeps what a signed
// bundle holds as it is) into a new folder in dir, on the same file
// system as the app it replaces. The caller removes it (StagedApp.Remove).
func (c *Client) DownloadApp(ctx context.Context, r Release, dir string) (StagedApp, error) {
	archive, err := c.fetchChecked(ctx, r, AppArchive)
	if err != nil {
		return StagedApp{}, err
	}
	tmp, err := os.MkdirTemp(dir, ".terminatr-app-update-*")
	if err != nil {
		return StagedApp{}, err
	}
	s := StagedApp{Dir: tmp, Path: filepath.Join(tmp, AppBundle)}
	zip := filepath.Join(tmp, AppArchive)
	if err := os.WriteFile(zip, archive, 0o600); err != nil {
		s.Remove()
		return StagedApp{}, err
	}
	out, err := exec.Command("ditto", "-x", "-k", zip, filepath.Join(tmp, "x")).CombinedOutput()
	if err != nil {
		s.Remove()
		return StagedApp{}, fmt.Errorf("unpacking %s: %s", AppArchive, firstLine(string(out), err))
	}
	os.Remove(zip)
	// The zip holds Terminatr.app alone (check.sh), at its top.
	if err := os.Rename(filepath.Join(tmp, "x", AppBundle), s.Path); err != nil {
		s.Remove()
		return StagedApp{}, fmt.Errorf("%s holds no %s", AppArchive, AppBundle)
	}
	return s, nil
}

// VerifyApp is the app's Verifier on macOS: tm's checks on the bundle
// and everything in it (Developer ID of team, hardened runtime), its
// identifier, and Gatekeeper's assessment, which must find it notarised.
// tm downloads without the quarantine flag, so Gatekeeper wouldn't look
// on its own.
func VerifyApp(path, team string) (string, error) {
	got, info, err := codesign(path, team)
	if err != nil {
		return "", err
	}
	if id := infoField(info, "Identifier"); id != AppID {
		return "", fmt.Errorf("signed as %q, want %q", id, AppID)
	}
	out, err := exec.Command("spctl", "--assess", "--type", "execute", "-v", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rejected by Gatekeeper: %s", firstLine(string(out), err))
	}
	if !strings.Contains(string(out), "source=Notarized Developer ID") {
		return "", fmt.Errorf("not notarised: %s", firstLine(string(out), errors.New("no assessment")))
	}
	return got, nil
}

// InstallApp puts the staged app at dst, a path ending in AppBundle. An
// app already there is moved aside first and removed once the new one
// is in place, or moved back if that fails. A running app keeps running
// the old code it loaded; it runs the new one once reopened.
func InstallApp(s StagedApp, dst string) error {
	if filepath.Base(dst) != AppBundle {
		return fmt.Errorf("%s is not a %s", dst, AppBundle)
	}
	aside := ""
	if _, err := os.Lstat(dst); err == nil {
		aside = filepath.Join(s.Dir, "old-"+AppBundle)
		if err := os.Rename(dst, aside); err != nil {
			return fmt.Errorf("can't move %s aside: %w", dst, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(s.Path, dst); err != nil {
		if aside != "" {
			os.Rename(aside, dst)
		}
		return fmt.Errorf("can't install %s: %w", dst, err)
	}
	return nil // s.Remove deletes the old app with the staging folder
}

// Writable reports whether dir accepts a new file (a new app goes into
// the first writable of AppPlaces).
func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".tm-write-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}
