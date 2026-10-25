package vault

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
)

// The shared files (docs/SPEC.md §5.1, Vault: write-back). PROJECT.md
// stays the human's to edit, in Obsidian too; a change made outside tm
// raises vault-edit with tm's fixed words (the file, the lines it
// touched), so the coordinator rereads it. The coordinator writes
// CONTEXT.md and memory/ itself, so tm can't tell a human's edit there
// from its own, and doesn't watch them.

// sharedNames are the shared files tm watches, in a project's folder.
var sharedNames = []string{"PROJECT.md"}

// sharedFile is what tm last saw of a shared file: its hash and a short
// hash of each line, to count the lines a change adds and removes.
type sharedFile struct {
	Hash  string   `json:"hash"`
	Lines []string `json:"lines,omitempty"`
}

func readShared(data []byte) sharedFile {
	f := sharedFile{Hash: hash(data)}
	for _, l := range bytes.SplitAfter(data, []byte("\n")) {
		if len(l) > 0 {
			f.Lines = append(f.Lines, hash(bytes.TrimRight(l, "\r\n"))[:12])
		}
	}
	return f
}

// lineChanges counts the lines in now that weren't in was, and those in
// was that aren't in now, as multisets.
func lineChanges(was, now []string) (added, removed int) {
	count := map[string]int{}
	for _, l := range was {
		count[l]++
	}
	for _, l := range now {
		if count[l] > 0 {
			count[l]--
		} else {
			added++
		}
	}
	for _, n := range count {
		removed += n
	}
	return added, removed
}

// CheckShared raises a vault-edit item in p for each shared file that
// settled (Settle) changed since tm last saw it, and reports how many
// it raised. The first look only records the file.
func CheckShared(p *project.Project, now time.Time) (raised int, err error) {
	if !On() {
		return 0, nil
	}
	err = updateLedger(func(l *ledger) error {
		if l.Shared == nil {
			l.Shared = map[string]sharedFile{}
		}
		var errs []error
		for _, name := range sharedNames {
			key := p.Slug + "/" + name
			fi, err := os.Lstat(p.Path(name))
			if err != nil || !fi.Mode().IsRegular() {
				delete(l.Shared, key)
				continue
			}
			if now.Sub(fi.ModTime()) < Settle {
				continue
			}
			data, err := os.ReadFile(p.Path(name))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			was, ok := l.Shared[key]
			if was.Hash == hash(data) {
				continue
			}
			cur := readShared(data)
			if ok {
				added, removed := lineChanges(was.Lines, cur.Lines)
				summary := fmt.Sprintf("%s changed outside tm: %s added, %s removed · reread it (tm context)", name, plural(added, "line"), plural(removed, "line"))
				if _, err := p.AddItem(project.KindVaultEdit, name, summary, false); err != nil {
					errs = append(errs, err)
					continue
				}
				raised++
			}
			l.Shared[key] = cur
		}
		return errors.Join(errs...)
	})
	return raised, err
}
