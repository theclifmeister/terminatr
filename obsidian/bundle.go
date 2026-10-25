// Package obsidian is terminatr's Obsidian plugin (docs/SPEC.md §5.1,
// Vault: the Obsidian plugin): its TypeScript sources, manifest and
// styles, embedded in tm and bundled by tm vault init --plugin into
// .obsidian/plugins/terminatr/. package.json and tsconfig.json are for
// CI's type check and tests; tm needs neither node nor npm.
package obsidian

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

//go:embed manifest.json styles.css src/*.ts
var sources embed.FS

// ID is the plugin's id: its folder under .obsidian/plugins/.
const ID = "terminatr"

// External are the modules the bundle may require: Obsidian's API and
// the node built-ins it runs tm with. Anything else is a bundling error.
var External = []string{"obsidian", "child_process", "fs", "os", "path"}

// semver is a version Obsidian compares: x.y.z.
var semver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Files are the plugin's files as installed, by name: manifest.json
// (with version, when it is x.y.z), main.js (src/main.ts bundled) and
// styles.css.
func Files(version string) (map[string][]byte, error) {
	manifest, err := sources.ReadFile("manifest.json")
	if err != nil {
		return nil, err
	}
	if v := strings.TrimPrefix(version, "v"); semver.MatchString(v) {
		var m map[string]any
		if err := json.Unmarshal(manifest, &m); err != nil {
			return nil, err
		}
		m["version"] = v
		if manifest, err = json.MarshalIndent(m, "", "  "); err != nil {
			return nil, err
		}
		manifest = append(manifest, '\n')
	}
	styles, err := sources.ReadFile("styles.css")
	if err != nil {
		return nil, err
	}
	main, err := Bundle()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"manifest.json": manifest, "main.js": main, "styles.css": styles}, nil
}

// Bundle is src/main.ts and what it imports as one CommonJS script, the
// form Obsidian loads, read only from the embedded sources.
func Bundle() ([]byte, error) {
	embedded := api.Plugin{
		Name: "tm-obsidian",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `^\.`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
				p := path.Clean(a.Path)
				if a.Kind != api.ResolveEntryPoint {
					p = path.Join(path.Dir(a.Importer), a.Path)
				}
				if !strings.HasSuffix(p, ".ts") {
					p += ".ts"
				}
				return api.OnResolveResult{Path: p, Namespace: "embed"}, nil
			})
			b.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "embed"}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
				data, err := sources.ReadFile(a.Path)
				if errors.Is(err, fs.ErrNotExist) {
					return api.OnLoadResult{}, fmt.Errorf("%s isn't one of the plugin's sources", a.Path)
				}
				if err != nil {
					return api.OnLoadResult{}, err
				}
				s := string(data)
				return api.OnLoadResult{Contents: &s, Loader: api.LoaderTS}, nil
			})
		},
	}
	r := api.Build(api.BuildOptions{
		EntryPoints: []string{"./src/main.ts"},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatCommonJS,
		Platform:    api.PlatformNode,
		Target:      api.ES2020,
		Charset:     api.CharsetUTF8,
		External:    External,
		LogLevel:    api.LogLevelSilent,
		Sourcemap:   api.SourceMapNone,
		Banner:      map[string]string{"js": "/* terminatr's Obsidian plugin, bundled by tm vault init --plugin from obsidian/src in the tm repo. */"},
		Plugins:     []api.Plugin{embedded},
	})
	if len(r.Errors) > 0 {
		var b strings.Builder
		for _, e := range r.Errors {
			if e.Location != nil {
				fmt.Fprintf(&b, "%s:%d:%d: ", e.Location.File, e.Location.Line, e.Location.Column)
			}
			b.WriteString(e.Text + "\n")
		}
		return nil, errors.New(strings.TrimSpace(b.String()))
	}
	return r.OutputFiles[0].Contents, nil
}
