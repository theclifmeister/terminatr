package moddev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// Stamp is a cheap fingerprint of the mod folder dir: each file's path,
// size and modification time, skipping .git, node_modules and other
// hidden folders. A save changes it.
func Stamp(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir && d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s %d %d\n", filepath.ToSlash(rel), fi.Size(), fi.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

// Watch calls changed each time the folder dir changes, once its stamp
// has held still for one more look (an editor's save may be several
// writes), until ctx ends. It looks every every.
func Watch(ctx context.Context, dir string, every time.Duration, changed func()) {
	last, _ := Stamp(dir)
	pending := ""
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now, err := Stamp(dir)
		if err != nil {
			continue
		}
		switch {
		case now == last:
			pending = ""
		case now != pending:
			pending = now
		default:
			last, pending = now, ""
			changed()
		}
	}
}
