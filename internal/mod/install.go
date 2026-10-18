package mod

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
)

// Installing, updating and removing mods (mods design §8; tm mod). Each
// is a human call: an agent caller is refused, as for enabling. A mod is
// fetched and checked in a staging folder beside the installed ones, so
// what lands in ~/.terminatr/mods/<id> is always a whole, valid copy.

// Staged is a mod fetched from its source and checked, waiting in a
// staging folder to be installed or to replace an installed one.
type Staged struct {
	// Dir is the copy, laid out as it will be installed.
	Dir      string
	Manifest *Manifest
	Source   Source
	tmp      string
}

// Close removes the staging folder, unless the mod was installed from it.
func (s *Staged) Close() {
	if s != nil && s.tmp != "" {
		os.RemoveAll(s.tmp)
	}
}

// Fetch copies the mod at src into a staging folder (a git URL is cloned
// first, at src.Ref, and only its folder src.Subdir copied) and checks
// its manifest and mod API, a v2 layout's included (its other data files
// are CheckData's, the Mods tab's). An invalid mod
// comes back with its manifest, if any, and the reasons.
func Fetch(src Source) (*Staged, error) {
	mods, err := home.ModsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(mods, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(mods, ".staging-")
	if err != nil {
		return nil, err
	}
	st := &Staged{Dir: filepath.Join(tmp, "mod"), Source: src, tmp: tmp}
	from := src.Path
	if src.Git != "" {
		if src.Subdir != "" {
			// Checked again here: a Source can come from a .tm-source.json.
			if err := CheckSubdir(src.Subdir); err != nil {
				st.Close()
				return nil, err
			}
		}
		from = filepath.Join(tmp, "clone")
		if st.Source.Commit, st.Source.Tree, err = clone(src.Git, src.Ref, src.Subdir, from); err != nil {
			st.Close()
			return nil, err
		}
		if src.Subdir != "" {
			if from, err = subdirIn(from, src.Subdir); err != nil {
				st.Close()
				return nil, err
			}
		}
	}
	if err := copyMod(from, st.Dir); err != nil {
		st.Close()
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	if st.Source.Hash, err = TreeHash(st.Dir); err != nil {
		st.Close()
		return nil, err
	}
	st.Manifest, err = ReadManifest(st.Dir)
	if err == nil {
		err = CheckAPI(st.Manifest)
	}
	if err == nil {
		err = Mod{ID: st.Manifest.ID, Dir: st.Dir, Manifest: st.Manifest}.checkLayoutAPI()
	}
	if err == nil {
		err = Mod{ID: st.Manifest.ID, Dir: st.Dir, Manifest: st.Manifest}.checkThemeAPI()
	}
	if err != nil {
		return st, err
	}
	return st, nil
}

// Validate checks the mod folder dir as tm mod install would: its
// manifest, the mod API and what tm would copy in, and its data files
// load (CheckData; the console's checks when it is linked). It returns the
// manifest when there is one, even with errors.
func Validate(dir string) (*Manifest, error) {
	if err := walkMod(dir, func(string, fs.DirEntry, string) error { return nil }); err != nil {
		m, _ := ReadManifest(dir)
		return m, err
	}
	m, err := ReadManifest(dir)
	if err == nil {
		err = CheckAPI(m)
	}
	if err == nil {
		err = errors.Join(Mod{ID: m.ID, Dir: dir, Manifest: m}.CheckData()...)
	}
	return m, err
}

// Taken says why a mod with id can't be installed next to the mods in
// r, "" when it can: it is a built-in mod's id, or installed already,
// with what works instead (tm mod update only takes a mod that records
// where it came from, T301 B10). tm mod install and the Mods tab ask it
// before the consent, so nobody consents to what Install would refuse.
func Taken(r *Registry, id string) string {
	old, ok := r.Get(id)
	if !ok && !IsBundled(id) {
		return ""
	}
	hint := "tm mod update " + id
	if old.Builtin || IsBundled(id) {
		hint = "it is a built-in mod's id; give your copy another id in its mod.toml"
	} else if _, ok := ReadSource(old.Dir); !ok {
		hint = "it records no source (installed by hand, or by tm mod dev); tm mod remove " + id + " first"
	}
	return fmt.Sprintf("%s is already installed: %s", id, hint)
}

// Install installs the staged mod, disabled and with nothing granted:
// its id mustn't be installed yet, and a [mods.<id>] table left behind
// by an earlier one is removed so it grants nothing. A data folder kept
// from a removal is the mod's again.
func Install(c caller.Caller, st *Staged) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	id := st.Manifest.ID
	if IsBundled(id) {
		return fmt.Errorf("%s is a built-in mod's id: give your copy another id in its mod.toml", id)
	}
	mods, err := home.ModsDir()
	if err != nil {
		return err
	}
	dst := filepath.Join(mods, id)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s is already installed: tm mod update %s, or tm mod remove %s first", id, id, id)
	}
	if err := config.RemoveModsTable(id); err != nil {
		return fmt.Errorf("[mods.%s] in config.toml: %w", id, err)
	}
	st.Source.Installed = time.Now().UTC()
	if err := writeSource(st.Dir, st.Source); err != nil {
		return err
	}
	if err := os.Rename(st.Dir, dst); err != nil {
		return err
	}
	if d, err := home.ModStateDir(); err == nil {
		os.Remove(filepath.Join(d, id, removedFile))
	}
	return nil
}

// Replace puts the staged mod in place of installed mod id, keeping its
// [mods.<id>] table, with grants extended by grant (the permissions the
// human just accepted for it). The old copy is restored if the swap
// fails.
func Replace(c caller.Caller, id string, st *Staged, grant []string) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	if st.Manifest.ID != id {
		return fmt.Errorf("its source now holds mod %q, not %s", st.Manifest.ID, id)
	}
	m, err := installed(id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	st.Source.Installed, st.Source.Updated = now, now
	if was, ok := ReadSource(m.Dir); ok && !was.Installed.IsZero() {
		st.Source.Installed = was.Installed
	}
	if err := writeSource(st.Dir, st.Source); err != nil {
		return err
	}
	old := filepath.Join(st.tmp, "old")
	if err := os.Rename(m.Dir, old); err != nil {
		return err
	}
	if err := os.Rename(st.Dir, m.Dir); err != nil {
		if rerr := os.Rename(old, m.Dir); rerr != nil {
			// Keep the staging folder: it holds the only copy.
			st.tmp = ""
			return fmt.Errorf("%w; putting the old copy back failed too (%v): it is kept in %s", err, rerr, old)
		}
		return err
	}
	if len(grant) == 0 {
		return nil
	}
	c2, err := config.Load()
	if err != nil && c2 == nil {
		return err
	}
	grants := slices.Clone(c2.Mods[id].Grants)
	for _, g := range grant {
		if !slices.Contains(grants, g) {
			grants = append(grants, g)
		}
	}
	if err := SetGrants(c, id, grants); err != nil {
		return fmt.Errorf("%s is updated, but granting %s failed: %w", id, strings.Join(grant, ", "), err)
	}
	return nil
}

// removedFile marks a removed mod's data folder with when it was
// removed; SweepRemoved deletes it after KeepRemoved.
const removedFile = ".removed"

// KeepRemoved is how long a removed mod's data folder is kept.
const KeepRemoved = 30 * 24 * time.Hour

// Remove uninstalls mod id: its folder (a link tm mod dev made is only
// unlinked), its [mods.<id>] table and its cache folder (its bundle,
// the supervisor's). Its data folder is kept for KeepRemoved, so a
// reinstall finds it, unless purge, which also removes its log.
func Remove(c caller.Caller, id string, purge bool) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	if err := CheckID(id); err != nil {
		return err
	}
	if IsBundled(id) {
		return fmt.Errorf("%s is built in: it can be turned off, not removed", id)
	}
	mods, err := home.ModsDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(mods, id)
	if _, err := os.Lstat(dir); err != nil {
		return fmt.Errorf("no mod %q is installed", id)
	}
	if err := config.RemoveModsTable(id); err != nil {
		return fmt.Errorf("[mods.%s] in config.toml: %w", id, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if cache, err := home.ModCacheDir(); err == nil {
		os.RemoveAll(filepath.Join(cache, id))
	}
	state, err := home.ModStateDir()
	if err != nil {
		return err
	}
	data := filepath.Join(state, id)
	if purge {
		if logs, err := home.ModLogsDir(); err == nil {
			os.Remove(filepath.Join(logs, id+".log"))
		}
		return os.RemoveAll(data)
	}
	if fi, err := os.Stat(data); err == nil && fi.IsDir() {
		stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
		return os.WriteFile(filepath.Join(data, removedFile), []byte(stamp), 0o644)
	}
	return nil
}

// SweepRemoved deletes the data folders, and logs, of mods removed more
// than KeepRemoved before now.
func SweepRemoved(now time.Time) {
	state, err := home.ModStateDir()
	if err != nil {
		return
	}
	ents, _ := os.ReadDir(state)
	for _, e := range ents {
		data, err := os.ReadFile(filepath.Join(state, e.Name(), removedFile))
		if err != nil {
			continue
		}
		at, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil || now.Sub(time.Unix(at, 0)) < KeepRemoved {
			continue
		}
		os.RemoveAll(filepath.Join(state, e.Name()))
		if logs, err := home.ModLogsDir(); err == nil {
			os.Remove(filepath.Join(logs, e.Name()+".log"))
		}
	}
}

// Enable turns mod id on with every permission its manifest asks for
// granted: the human has seen them (Consent). With project, it also
// runs for that project: added to its projects list, or the list begun
// with it when the mod was off.
func Enable(c caller.Caller, id, project string) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	m, err := installed(id)
	if err != nil {
		return err
	}
	if m.State == Invalid || m.State == Incompatible {
		return fmt.Errorf("%s can't be enabled: %w", id, m.Err)
	}
	if project != "" {
		switch {
		case m.State == Enabled && len(m.Projects) == 0:
			// It runs for every project already.
		case !slices.Contains(m.Projects, project):
			if err := SetProjects(c, id, append(slices.Clone(m.Projects), project)); err != nil {
				return err
			}
		}
	}
	if err := SetGrants(c, id, m.Manifest.Permissions.Requested()); err != nil {
		return err
	}
	return SetEnabled(c, id, true)
}

// ErrAllProjects refuses to disable a mod for one project while it runs
// for all of them.
var ErrAllProjects = errors.New("runs for every project")

// Disable turns mod id off; with project, only for that project, taken
// off its projects list (the last one off turns the mod off). Its
// grants stay, for when it is enabled again.
func Disable(c caller.Caller, id, project string) error {
	if c.IsAgent() {
		return ErrHumanOnly
	}
	m, err := installed(id)
	if err != nil {
		return err
	}
	if project == "" {
		return SetEnabled(c, id, false)
	}
	if !slices.Contains(m.Projects, project) {
		if len(m.Projects) == 0 && m.State == Enabled {
			return fmt.Errorf("%s %w: list the ones it should with tm mod enable %s --project <slug>, or disable it", id, ErrAllProjects, id)
		}
		return nil
	}
	rest := slices.DeleteFunc(slices.Clone(m.Projects), func(p string) bool { return p == project })
	if len(rest) == 0 {
		if err := SetEnabled(c, id, false); err != nil {
			return err
		}
	}
	return SetProjects(c, id, rest)
}
