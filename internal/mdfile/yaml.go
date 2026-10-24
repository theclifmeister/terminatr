package mdfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// maxYAML is the most YAML front matter Decode reads.
const maxYAML = 64 << 10

// yamlTags are the tags YAML front matter may have, said or implied:
// YAML's core schema and timestamps. Others (!!binary, !!merge, custom
// ones) are refused, so a value is only ever plain data.
var yamlTags = map[string]bool{
	"!!str": true, "!!int": true, "!!float": true, "!!bool": true, "!!null": true,
	"!!timestamp": true, "!!seq": true, "!!map": true,
}

// decodeYAML decodes YAML front matter into v, strictly: at most maxYAML
// bytes, one document, a mapping at the top, no anchors or aliases, no
// tags but yamlTags. Keys v has no field for are left alone, as Obsidian
// and people add their own.
func decodeYAML(front []byte, v any) error {
	if len(front) > maxYAML {
		return fmt.Errorf("YAML is %d bytes, more than %d", len(front), maxYAML)
	}
	dec := yaml.NewDecoder(bytes.NewReader(front))
	var doc yaml.Node
	if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
		return nil // only blank lines or comments
	} else if err != nil {
		return err
	}
	var more yaml.Node
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return errors.New("YAML has more than one document")
	}
	if err := checkYAML(&doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		return nil
	}
	top := doc.Content[0]
	if top.Kind == yaml.ScalarNode && top.ShortTag() == "!!null" {
		return nil
	}
	if top.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: YAML front matter is not a mapping of keys", top.Line)
	}
	return top.Decode(v)
}

// checkYAML refuses anchors, aliases and tags outside yamlTags in n and
// below.
func checkYAML(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("line %d: YAML aliases are not read", n.Line)
	}
	if n.Anchor != "" {
		return fmt.Errorf("line %d: YAML anchors are not read", n.Line)
	}
	if n.Kind != yaml.DocumentNode && !yamlTags[n.ShortTag()] {
		return fmt.Errorf("line %d: YAML tag %s is not read", n.Line, n.ShortTag())
	}
	for _, c := range n.Content {
		if err := checkYAML(c); err != nil {
			return err
		}
	}
	return nil
}
