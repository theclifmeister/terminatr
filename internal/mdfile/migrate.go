package mdfile

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"
)

// IsTOML reports whether data starts with TOML front matter, which
// ToYAML would rewrite.
func IsTOML(data []byte) bool {
	_, _, f, err := split(data)
	return err == nil && f == fence
}

// ToYAML rewrites data's TOML front matter as the YAML Join writes, its
// keys in the same order and its full-line comments kept above the key
// they came before; the body stays byte for byte. Keys are lowercased:
// the TOML reader matches a struct's tags regardless of case, the YAML
// one exactly, and every tag tm has is lowercase. Data with no TOML
// front matter comes back as it is. It fails, rather than change what a
// reader gets, when the YAML wouldn't read back as the same values.
func ToYAML(data []byte) ([]byte, error) {
	front, body, f, err := split(data)
	if err != nil {
		return nil, err
	}
	if f != fence {
		return data, nil
	}
	var m map[string]any
	md, err := toml.Decode(string(front), &m)
	if err != nil {
		return nil, fmt.Errorf("front matter: %w", err)
	}
	var keys []string
	lower := map[string]any{}
	for _, k := range md.Keys() {
		if len(k) != 1 {
			continue
		}
		l := strings.ToLower(k[0])
		if _, dup := lower[l]; dup {
			return nil, fmt.Errorf("front matter: key %q twice, in different cases", l)
		}
		keys, lower[l] = append(keys, k[0]), m[k[0]]
	}
	heads, foot := tomlComments(front, keys)
	top := &yaml.Node{Kind: yaml.MappingNode, FootComment: foot}
	for _, k := range keys {
		var v yaml.Node
		if err := v.Encode(m[k]); err != nil {
			return nil, err
		}
		top.Content = append(top.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.ToLower(k), HeadComment: heads[k]}, &v)
	}
	y, err := renderYAML(top)
	if err != nil {
		return nil, err
	}
	var back map[string]any
	if err := decodeYAML(y, &back); err != nil {
		return nil, fmt.Errorf("front matter as YAML: %w", err)
	}
	if !reflect.DeepEqual(plain(lower), plain(back)) {
		return nil, fmt.Errorf("front matter as YAML doesn't read back the same:\n%s", y)
	}
	var b bytes.Buffer
	b.WriteString(yamlFence + "\n")
	b.Write(y)
	b.WriteString(yamlFence + "\n")
	b.Write(body)
	return b.Bytes(), nil
}

// tomlKey matches a TOML line that sets a top-level key: its name.
var tomlKey = regexp.MustCompile(`^\s*([A-Za-z0-9_-]+|"[^"]*"|'[^']*')\s*=`)

// tomlComments are front's full-line comments, by the key (of keys, in
// order) whose line comes next; foot are those after the last one.
func tomlComments(front []byte, keys []string) (heads map[string]string, foot string) {
	heads = map[string]string{}
	var pending []string
	next := 0
	for _, l := range strings.Split(string(front), "\n") {
		l = strings.TrimRight(l, "\r")
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") {
			pending = append(pending, t)
			continue
		}
		mk := tomlKey.FindStringSubmatch(l)
		if mk == nil || next >= len(keys) || strings.Trim(mk[1], `"'`) != keys[next] {
			continue
		}
		heads[keys[next]] = strings.Join(pending, "\n")
		pending = nil
		next++
	}
	return heads, strings.Join(pending, "\n")
}

// plain is v with times as UTC strings and integers as int64, so a value
// read from TOML compares with the same value read from YAML.
func plain(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = plain(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = plain(e)
		}
		return out
	case []map[string]any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = plain(e)
		}
		return out
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case int:
		return int64(v)
	}
	return v
}
