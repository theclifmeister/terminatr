package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/update"
)

// updateApp is tm update's second half on macOS: the Mac app, after tm
// (docs/SPEC.md §10.1). A cask install is upgraded by brew; a direct one
// is replaced from the release's terminatr-app.zip after its checksum,
// its Developer ID signature of tm's team and its notarisation check
// out. With --app a missing app is installed the way tm was: by the
// cask for a Homebrew tm, else from the zip.
func (u *updater) updateApp(e *Env, in update.Install, rel update.Release, opts updateOpts) error {
	a, ok := u.FindApp()
	if !ok {
		if !opts.app {
			fmt.Fprintln(e.Stdout, "\nThe Mac app isn't installed; tm update --app installs it.")
			return nil
		}
		if in.Method == update.Homebrew {
			fmt.Fprintf(e.Stdout, "\ntm was installed with Homebrew; the Mac app installs with:\n  brew install --cask %s\n", update.CaskFull)
			if !opts.yes && !(u.TTY && ask(e, "Run it now? [y/N] ")) {
				return nil
			}
			return u.brew(e, "install", "--cask", update.CaskFull)
		}
		dir, err := u.appDir()
		if err != nil {
			fmt.Fprintf(e.Stderr, "tm update: %v\n", err)
			return &exitError{ExitIO}
		}
		fmt.Fprintf(e.Stdout, "\nTerminatr.app %s goes into %s\n", rel.Tag, dir)
		return u.downloadApp(e, rel, filepath.Join(dir, update.AppBundle), opts)
	}
	if !appNewer(rel.Tag, a) {
		fmt.Fprintf(e.Stdout, "Terminatr.app %s is the latest release\n", a.Version)
		return nil
	}
	if a.Method == update.Homebrew {
		// Homebrew owns the cask's app, as it owns the formula's tm.
		fmt.Fprintf(e.Stdout, "\nTerminatr.app was installed with Homebrew (%s); it updates with:\n  %s\n", a.Path, a.Upgrade)
		if !opts.yes && !(u.TTY && ask(e, "Run it now? [y/N] ")) {
			return nil
		}
		return u.brew(e, "upgrade", "--cask", update.Cask)
	}
	fmt.Fprintf(e.Stdout, "\nTerminatr.app %s is available (installed: %s at %s)\n", rel.Tag, orUnknown(a.Version), a.Path)
	return u.downloadApp(e, rel, a.Path, opts)
}

func (u *updater) brew(e *Env, args ...string) error {
	if err := u.Brew(args...); err != nil {
		fmt.Fprintf(e.Stderr, "tm update: brew %s: %v\n", strings.Join(args, " "), err)
		return &exitError{ExitIO}
	}
	return nil
}

// appDir is where a new app goes: the first writable of AppPlaces,
// ~/Applications made if it comes to that.
func (u *updater) appDir() (string, error) {
	for i, dir := range u.AppPlaces {
		if i > 0 {
			os.MkdirAll(dir, 0o755)
		}
		if update.Writable(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("none of %s is writable for the Mac app", strings.Join(u.AppPlaces, ", "))
}

// downloadApp installs the release's app at dst, asking first unless
// --yes, after the checks; nothing changes when one fails.
func (u *updater) downloadApp(e *Env, rel update.Release, dst string, opts updateOpts) error {
	if !opts.yes {
		if !u.TTY {
			fmt.Fprintln(e.Stderr, "tm update: not a terminal; pass --yes to install the Mac app")
			return &exitError{ExitRefused}
		}
		if !ask(e, "Install it? [y/N] ") {
			fmt.Fprintln(e.Stdout, "nothing changed")
			return &exitError{ExitRefused}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	staged, err := u.Client.DownloadApp(ctx, rel, filepath.Dir(dst))
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			err = fmt.Errorf("%w: %s is not writable; update the app as its owner", err, filepath.Dir(dst))
		}
		fmt.Fprintf(e.Stderr, "tm update: %v\n", err)
		return &exitError{ExitIO}
	}
	defer staged.Remove()
	fmt.Fprintf(e.Stdout, "checksum   ok (%s)\n", update.AppArchive)
	if u.VerifyApp != nil {
		team, err := u.VerifyApp(staged.Path, u.Team)
		if err != nil {
			fmt.Fprintf(e.Stderr, "tm update: the downloaded Terminatr.app fails the signature check: %v; nothing changed\n", err)
			return &exitError{ExitRefused}
		}
		fmt.Fprintf(e.Stdout, "signature  ok (Developer ID, team %s, notarised)\n", team)
	}
	got, _ := update.FindApp([]string{staged.Dir}, nil)
	if want, _, _ := strings.Cut(rel.Tag, "-"); got.Tag() != want {
		fmt.Fprintf(e.Stderr, "tm update: the downloaded Terminatr.app is version %q, not %s; nothing changed\n", got.Version, want)
		return &exitError{ExitRefused}
	}
	if err := update.InstallApp(staged, dst); err != nil {
		fmt.Fprintf(e.Stderr, "tm update: %v\n", err)
		return &exitError{ExitIO}
	}
	fmt.Fprintf(e.Stdout, "installed  Terminatr.app %s at %s\n", got.Version, dst)
	fmt.Fprintln(e.Stdout, "If the app is open, quit and reopen it to run the new one.")
	return nil
}

// appNewer reports whether release tag's app is newer than a; an app
// whose version can't be read counts as older.
func appNewer(tag string, a update.App) bool {
	newer, ok := update.Newer(tag, a.Tag())
	return newer || !ok
}

func appHint(a update.App) string {
	if a.Method == update.Homebrew {
		return a.Upgrade
	}
	return "tm update"
}

func orUnknown(v string) string {
	if v == "" {
		return "unknown version"
	}
	return v
}
