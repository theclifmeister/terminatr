package vault

import (
	"os"
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// The visible link to the vault (T873): the projects folder lives under
// the hidden ~/.terminatr, which Obsidian's "Open folder as vault" dialog
// makes hard to pick, so tm vault init also links ~/terminatr-obsidian to
// it (a symlink; a directory junction on Windows, fsx.LinkDir). tm never
// overwrites: a path that is anything but a link to the vault is left as
// it is, with a message. .obsidian/tm.json records the link tm made, so
// off removes it only while it is still ours and still points there.

// LinkName is the default link's name in the user's home folder.
const LinkName = "terminatr-obsidian"

// DefaultLink is where init links the vault: ~/terminatr-obsidian.
func DefaultLink() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, LinkName), nil
}

// LinkPlan is what init would do about the link.
type LinkPlan struct {
	Path string // the link
	// What is "new" (init makes it), "ours" (already a link to the vault)
	// or "kept" (something else is there; Why says what).
	What string
	Why  string
}

// pointsAtVault reports whether path is a link (symlink or junction)
// that leads to root.
func pointsAtVault(path, root string) bool {
	if _, err := os.Readlink(path); err != nil {
		return false
	}
	a, err := os.Stat(path)
	if err != nil {
		return false
	}
	b, err := os.Stat(root)
	return err == nil && os.SameFile(a, b)
}

// PlanLink plans the link at path, which must be absolute.
func (pl *Plan) PlanLink(path string) {
	lp := &LinkPlan{Path: path}
	pl.link = lp
	fi, err := os.Lstat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		lp.What = "new"
	case err != nil:
		lp.What, lp.Why = "kept", err.Error()
	case pointsAtVault(path, pl.Root):
		lp.What = "ours"
	default:
		lp.What = "kept"
		if _, err := os.Readlink(path); err == nil {
			lp.Why = "a link to somewhere else"
		} else if fi.IsDir() {
			lp.Why = "a folder"
		} else {
			lp.Why = "a file"
		}
	}
}

// Link is the planned link, nil without PlanLink.
func (pl *Plan) Link() *LinkPlan { return pl.link }

// makeLink does the link plan: creates a new link and records it, or
// records one that is already ours and was recorded before. The parent
// folder must exist; an error leaves the vault set up without the link.
func (pl *Plan) makeLink() error {
	lp := pl.link
	if lp == nil {
		return nil
	}
	switch lp.What {
	case "new":
		if err := fsx.LinkDir(pl.Root, lp.Path); err != nil {
			return err
		}
		pl.state.Link = lp.Path
	case "ours":
		// A link the user made by hand stays theirs: off won't remove it.
	}
	return nil
}

// removeLink removes the link st recorded, only while it still is a link
// to the vault; it reports whether it did.
func removeLink(st *tmState, root string) bool {
	path := st.Link
	if path == "" {
		return false
	}
	st.Link = ""
	if !pointsAtVault(path, root) {
		return false
	}
	return os.Remove(path) == nil
}

// linkFinding checks the link tm recorded; nil when it recorded none.
func linkFinding(root string) *Finding {
	st := readState(root)
	if st.Link == "" {
		return nil
	}
	f := Finding{Name: "folder link", Files: nil}
	_, lerr := os.Lstat(st.Link)
	switch {
	case lerr != nil:
		f.Status, f.Detail = "warn", st.Link+" is gone (tm vault init makes it again)"
	case pointsAtVault(st.Link, root):
		f.Status, f.Detail = "ok", st.Link+" opens the vault"
	default:
		f.Status, f.Detail = "warn", st.Link+" no longer leads to the vault (remove it and run tm vault init)"
	}
	return &f
}

// RecordedLink is the link tm made and recorded, "" if none.
func RecordedLink() string {
	root, err := home.ProjectsDir()
	if err != nil {
		return ""
	}
	return readState(root).Link
}
