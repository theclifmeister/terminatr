package mdfile

import (
	"bytes"
	"errors"
	"time"

	"go.yaml.in/yaml/v3"
)

// encodeYAML renders front as the YAML Join writes: one key per line in
// the struct's order, every string double-quoted (so no, on, 1e3 or a
// slug like yes read back as strings), every time UTC ISO 8601
// (2026-10-24T18:12:56Z) and lists as block lists.
func encodeYAML(front any) ([]byte, error) {
	var n yaml.Node
	if err := n.Encode(front); err != nil {
		return nil, err
	}
	return renderYAML(&n)
}

// renderYAML styles n as encodeYAML writes it and renders it.
func renderYAML(n *yaml.Node) ([]byte, error) {
	if err := styleYAML(n, false); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if b.String() == "{}\n" {
		return nil, nil
	}
	return b.Bytes(), nil
}

// styleYAML quotes n's strings and turns its times to UTC, below n too;
// key says n is a mapping key, left plain.
func styleYAML(n *yaml.Node, key bool) error {
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!str":
			if !key {
				n.Tag, n.Style = "", yaml.DoubleQuotedStyle
			}
		case "!!timestamp":
			t, err := time.Parse(time.RFC3339Nano, n.Value)
			if err != nil {
				return err
			}
			n.Tag, n.Style, n.Value = "", 0, t.UTC().Format(time.RFC3339Nano)
		case "!!merge":
			// A string "<<", which a reader would take for a merge key.
			n.Tag, n.Style = "", yaml.DoubleQuotedStyle
		case "!!binary":
			// What the encoder makes of a string that isn't UTF-8: no
			// reader would get the string back.
			return errors.New("front matter: invalid UTF-8")
		}
	case yaml.MappingNode:
		n.Style = 0
		for i, c := range n.Content {
			if err := styleYAML(c, i%2 == 0); err != nil {
				return err
			}
		}
	default:
		n.Style = 0
		for _, c := range n.Content {
			if err := styleYAML(c, false); err != nil {
				return err
			}
		}
	}
	return nil
}
