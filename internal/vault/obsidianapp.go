package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// Obsidian's own vault list (T874), for tm vault open: Obsidian opens by
// URI only a vault in obsidian.json, in its settings folder (Electron's
// userData for "obsidian"). Checked against Obsidian 1.14.5's main.js:
// {"vaults":{"<16 hex>":{"path":"/abs","ts":<ms>,"open":true}},…};
// paths compare with path.resolve, links not followed; the app reads the
// file at start and writes the whole of it back, so tm adds an entry only
// while Obsidian is not running, after a backup, inserting its bytes and
// changing no others.

// ObsidianConfig is the obsidian.json of this user's Obsidian: the first
// that exists of the standard settings folder (os.UserConfigDir), then
// Flatpak's and Snap's; "" when Obsidian has none (not installed, or
// never started).
func ObsidianConfig() string {
	var dirs []string
	if d, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(d, "obsidian"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(h, ".var", "app", "md.obsidian.Obsidian", "config", "obsidian"),
			filepath.Join(h, "snap", "obsidian", "current", ".config", "obsidian"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, "obsidian.json")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// obsidianList is the part of obsidian.json tm reads.
type obsidianList struct {
	Vaults map[string]struct {
		Path string `json:"path"`
	} `json:"vaults"`
}

// KnownVault is the id under which cfg lists the vault root, by the
// listed folder's real path (the link and the projects folder are the
// same vault to tm, two to Obsidian); "" when cfg doesn't list it.
func KnownVault(cfg, root string) (string, error) {
	b, err := os.ReadFile(cfg)
	if err != nil {
		return "", err
	}
	var l obsidianList
	if err := json.Unmarshal(b, &l); err != nil {
		return "", fmt.Errorf("%s: %w", cfg, err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	for id, v := range l.Vaults {
		if got, err := filepath.EvalSymlinks(v.Path); err == nil && got == want {
			return id, nil
		}
	}
	return "", nil
}

// OpenURI is the obsidian:// address that opens the vault Obsidian lists
// as id.
func OpenURI(id string) string {
	return "obsidian://open?vault=" + url.QueryEscape(id)
}

// VaultPath is the folder to give Obsidian for the vault: the link tm
// made, while it still leads there, else the projects folder.
func VaultPath() (string, error) {
	root, err := home.ProjectsDir()
	if err != nil {
		return "", err
	}
	if l := readState(root).Link; l != "" && pointsAtVault(l, root) {
		return l, nil
	}
	return root, nil
}

// ObsidianRunning reports whether an Obsidian process runs, by the
// program's name in the process list; an error when the list can't be
// read, which callers take as running.
func ObsidianRunning() (bool, error) {
	ps, err := proc.List()
	if err != nil {
		return false, err
	}
	for _, p := range ps {
		name := p.Exe
		if name == "" && len(p.Argv) > 0 {
			name = p.Argv[0]
		}
		if isObsidian(name) {
			return true, nil
		}
	}
	return false, nil
}

// isObsidian reports whether the program at path is Obsidian's main
// process (Obsidian on macOS, obsidian on Linux, Obsidian.exe on
// Windows), not one of its helpers.
func isObsidian(path string) bool {
	base := path[strings.LastIndexAny(path, `/\`)+1:]
	return strings.EqualFold(base, "obsidian") || strings.EqualFold(base, "obsidian.exe")
}

// Register adds path to cfg's vault list as a new vault and returns its
// id and the backup it made first (cfg's bytes, beside it). Only the new
// entry's bytes go in; every other byte of the file stays. The caller has
// made sure Obsidian is not running.
func Register(cfg, path string, now time.Time) (id, backup string, err error) {
	b, err := os.ReadFile(cfg)
	if err != nil {
		return "", "", err
	}
	var l obsidianList
	if err := json.Unmarshal(b, &l); err != nil {
		return "", "", fmt.Errorf("%s: %w", cfg, err)
	}
	for id == "" || l.Vaults[id].Path != "" {
		if id, err = vaultID(); err != nil {
			return "", "", err
		}
	}
	entry, _ := json.Marshal(map[string]any{"path": path, "ts": now.UnixMilli()})
	out, err := insertVault(b, id, entry)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", cfg, err)
	}
	fi, err := os.Stat(cfg)
	if err != nil {
		return "", "", err
	}
	backup = cfg + ".tm-" + now.Format("20060102-150405") + ".bak"
	if err := fsx.WriteAtomic(backup, b, fi.Mode().Perm()); err != nil {
		return "", "", err
	}
	if err := fsx.WriteAtomic(cfg, out, fi.Mode().Perm()); err != nil {
		return "", backup, err
	}
	return id, backup, nil
}

// vaultID is a new vault id in Obsidian's form: 16 lowercase hex digits.
func vaultID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// insertVault returns data, a JSON object, with "id":entry first in its
// "vaults" object (a new "vaults" object first in data when it has
// none), every other byte as it was.
func insertVault(data []byte, id string, entry []byte) ([]byte, error) {
	key, _ := json.Marshal(id)
	member := append(append(key, ':'), entry...)
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	top := int(dec.InputOffset())
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if t != "vaults" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
			continue
		}
		if t, err := dec.Token(); err != nil || t != json.Delim('{') {
			return nil, errors.New(`"vaults" is not an object`)
		}
		at := int(dec.InputOffset())
		if dec.More() {
			member = append(member, ',')
		}
		return splice(data, at, member), nil
	}
	member = append([]byte(`"vaults":{`), append(member, '}')...)
	if bytes.TrimSpace(data[top:])[0] != '}' {
		member = append(member, ',')
	}
	return splice(data, top, member), nil
}

func splice(data []byte, at int, ins []byte) []byte {
	out := make([]byte, 0, len(data)+len(ins))
	return append(append(append(out, data[:at]...), ins...), data[at:]...)
}
