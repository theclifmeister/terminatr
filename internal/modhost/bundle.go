package modhost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// loaders are the files a mod's script may import, by extension.
var loaders = map[string]api.Loader{
	".ts":   api.LoaderTS,
	".mts":  api.LoaderTS,
	".js":   api.LoaderJS,
	".mjs":  api.LoaderJS,
	".json": api.LoaderJSON,
}

// Load returns the script to run for entry: a .js file is a bundle and
// runs as is; a .ts or .mts file is bundled first (tm mod dev and tests;
// installed mods run from the bundle Build cached).
func Load(entry string) (string, error) {
	switch strings.ToLower(filepath.Ext(entry)) {
	case ".js":
		b, err := os.ReadFile(entry)
		return string(b), err
	case ".ts", ".mts":
		code, _, err := Bundle(entry)
		return code, err
	}
	return "", fmt.Errorf("%s: a mod's main is a .ts, .mts or .js file", entry)
}

// Bundle transpiles entry and everything it imports into one ES2017 script
// for goja, and returns it with the sha256 of each file it read, keyed by
// the path relative to entry's folder. Imports are relative and stay
// inside that folder: there's no node_modules, no network, and nothing
// outside the mod can be read into it.
func Bundle(entry string) (code string, inputs map[string]string, err error) {
	abs, err := filepath.Abs(entry)
	if err != nil {
		return "", nil, err
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", nil, err
	}
	var mu sync.Mutex
	inputs = map[string]string{}
	confine := api.Plugin{
		Name: "tm-mod",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
				if a.Kind == api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil
				}
				if !strings.HasPrefix(a.Path, "./") && !strings.HasPrefix(a.Path, "../") {
					return api.OnResolveResult{}, fmt.Errorf("import %q: a mod imports only its own files, by a relative path", a.Path)
				}
				return api.OnResolveResult{}, nil // esbuild resolves; OnLoad checks where
			})
			b.OnLoad(api.OnLoadOptions{Filter: ".*"}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
				real, err := filepath.EvalSymlinks(a.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				rel, err := filepath.Rel(root, real)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
					return api.OnLoadResult{}, fmt.Errorf("%s is outside the mod's folder", a.Path)
				}
				loader, ok := loaders[strings.ToLower(filepath.Ext(real))]
				if !ok {
					return api.OnLoadResult{}, fmt.Errorf("%s: a mod imports only .ts, .mts, .js, .mjs and .json files", rel)
				}
				b, err := os.ReadFile(real)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				sum := sha256.Sum256(b)
				mu.Lock()
				inputs[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
				mu.Unlock()
				s := string(b)
				return api.OnLoadResult{Contents: &s, Loader: loader, ResolveDir: filepath.Dir(real)}, nil
			})
		},
	}
	r := api.Build(api.BuildOptions{
		EntryPoints: []string{abs},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatIIFE,
		Platform:    api.PlatformNeutral,
		// goja runs ES2017 (async/await) natively but has no async
		// generators: ES2017 lowers those and keeps async functions.
		Target:    api.ES2017,
		Charset:   api.CharsetUTF8,
		LogLevel:  api.LogLevelSilent,
		Sourcemap: api.SourceMapNone,
		Plugins:   []api.Plugin{confine},
	})
	if len(r.Errors) > 0 {
		var b strings.Builder
		for _, e := range r.Errors {
			if e.Location != nil {
				fmt.Fprintf(&b, "%s:%d:%d: ", e.Location.File, e.Location.Line, e.Location.Column)
			}
			b.WriteString(e.Text + "\n")
		}
		return "", nil, errors.New(strings.TrimSpace(b.String()))
	}
	return string(r.OutputFiles[0].Contents), inputs, nil
}

// bundleRecord is what Build keeps beside a cached bundle.
type bundleRecord struct {
	Bundler string            `json:"bundler"`
	Entry   string            `json:"entry"`
	Inputs  map[string]string `json:"inputs"` // path relative to the mod → sha256
	Bundle  string            `json:"bundle"` // file name in the cache folder
}

const recordName = "bundle.json"

// Build bundles entry into cacheDir and returns the bundle's path. The
// bundle is named by the sha256 of its content, and bundle.json records
// the sha256 of every file it was made from: while those files are
// unchanged, Build returns the cached bundle without running esbuild.
// tm mod install, update and dev call it; the host then loads the .js.
func Build(entry, cacheDir string) (string, error) {
	abs, err := filepath.Abs(entry)
	if err != nil {
		return "", err
	}
	if p, ok := cached(abs, cacheDir); ok {
		return p, nil
	}
	code, inputs, err := Bundle(abs)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(code))
	name := hex.EncodeToString(sum[:16]) + ".js"
	path := filepath.Join(cacheDir, name)
	if err := writeAtomic(path, []byte(code)); err != nil {
		return "", err
	}
	rec, _ := json.MarshalIndent(bundleRecord{Bundler: bundler(), Entry: abs, Inputs: inputs, Bundle: name}, "", "  ")
	if err := writeAtomic(filepath.Join(cacheDir, recordName), rec); err != nil {
		return "", err
	}
	// Older bundles of this mod.
	old, _ := filepath.Glob(filepath.Join(cacheDir, "*.js"))
	for _, o := range old {
		if filepath.Base(o) != name {
			_ = os.Remove(o)
		}
	}
	return path, nil
}

// cached returns the cached bundle if every input still has its hash.
func cached(entry, cacheDir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(cacheDir, recordName))
	if err != nil {
		return "", false
	}
	var rec bundleRecord
	if json.Unmarshal(b, &rec) != nil || rec.Bundler != bundler() || rec.Entry != entry || len(rec.Inputs) == 0 {
		return "", false
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(entry))
	if err != nil {
		return "", false
	}
	names := make([]string, 0, len(rec.Inputs))
	for n := range rec.Inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(n)))
		if err != nil {
			return "", false
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != rec.Inputs[n] {
			return "", false
		}
	}
	path := filepath.Join(cacheDir, rec.Bundle)
	if filepath.Base(rec.Bundle) != rec.Bundle {
		return "", false
	}
	code, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(code)
	if hex.EncodeToString(sum[:16])+".js" != rec.Bundle {
		return "", false
	}
	return path, true
}

// bundler names esbuild's version and the options, so a tm with another
// esbuild or other options bundles again.
func bundler() string {
	v := "?"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/evanw/esbuild" {
				v = d.Version
			}
		}
	}
	return "esbuild " + v + " es2017 iife 1"
}

func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
