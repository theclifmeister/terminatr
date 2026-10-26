package appapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Schema is the app API as data, for generating the app's Codable types
// (tm app schema): JSON Schema (2020-12) of every type under $defs,
// named as here, each method's params, result and stream data by
// reference, and the bridge's envelope (Request, Out).
type Schema struct {
	Schema       string                  `json:"$schema"`
	Title        string                  `json:"title"`
	AppAPI       int                     `json:"app_api"`
	Capabilities []string                `json:"capabilities"`
	Envelope     map[string]JSONSchema   `json:"envelope"`
	Methods      map[string]SchemaMethod `json:"methods"`
	Defs         map[string]JSONSchema   `json:"$defs"`
}

// SchemaMethod is one method in the Schema: Params is nil for a method
// that takes none.
type SchemaMethod struct {
	Write  bool        `json:"write"`
	Stream bool        `json:"stream"`
	Params *JSONSchema `json:"params"`
	Result JSONSchema  `json:"result"`
	Event  string      `json:"event,omitempty"`
	Data   *JSONSchema `json:"data,omitempty"`
}

// JSONSchema is the part of JSON Schema the app API's types need.
type JSONSchema struct {
	Ref                  string                `json:"$ref,omitempty"`
	Type                 string                `json:"type,omitempty"`
	Format               string                `json:"format,omitempty"`
	Properties           map[string]JSONSchema `json:"properties,omitempty"`
	Required             []string              `json:"required,omitempty"`
	Items                *JSONSchema           `json:"items,omitempty"`
	AdditionalProperties *JSONSchema           `json:"additionalProperties,omitempty"`
	// Order is the properties in the Go type's order, which JSON's
	// objects don't keep: a generator may want it.
	Order []string `json:"x-order,omitempty"`
}

// MakeSchema is the app API's Schema.
func MakeSchema() Schema {
	g := schemaGen{defs: map[string]JSONSchema{}}
	s := Schema{Schema: "https://json-schema.org/draft/2020-12/schema", Title: "terminatr app API",
		AppAPI: Version, Capabilities: Capabilities, Methods: map[string]SchemaMethod{},
		Envelope: map[string]JSONSchema{
			"request":       g.of(reflect.TypeFor[Request]()),
			"out":           g.of(reflect.TypeFor[Out]()),
			"cancel_params": g.of(reflect.TypeFor[CancelParams]()),
		}}
	for _, m := range Methods() {
		sm := SchemaMethod{Write: m.Write, Stream: m.Stream, Result: g.of(m.Result), Event: m.Event}
		if m.Params != nil {
			p := g.of(m.Params)
			sm.Params = &p
		}
		if m.Data != nil {
			d := g.of(m.Data)
			sm.Data = &d
		}
		s.Methods[m.Name] = sm
	}
	s.Defs = g.defs
	return s
}

type schemaGen struct{ defs map[string]JSONSchema }

var (
	timeType = reflect.TypeFor[time.Time]()
	rawType  = reflect.TypeFor[json.RawMessage]()
)

// of is t's schema: a $ref for a named struct, whose definition goes
// under $defs.
func (g *schemaGen) of(t reflect.Type) JSONSchema {
	switch {
	case t == timeType:
		return JSONSchema{Type: "string", Format: "date-time"}
	case t == rawType:
		return JSONSchema{}
	}
	switch t.Kind() {
	case reflect.String:
		return JSONSchema{Type: "string"}
	case reflect.Bool:
		return JSONSchema{Type: "boolean"}
	case reflect.Int, reflect.Int64, reflect.Uint16, reflect.Uint64:
		return JSONSchema{Type: "integer"}
	case reflect.Float64:
		return JSONSchema{Type: "number"}
	case reflect.Pointer:
		return g.of(t.Elem())
	case reflect.Slice:
		items := g.of(t.Elem())
		return JSONSchema{Type: "array", Items: &items}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			panic("appapi schema: a map's keys must be strings: " + t.String())
		}
		v := g.of(t.Elem())
		return JSONSchema{Type: "object", AdditionalProperties: &v}
	case reflect.Struct:
		name := t.Name()
		if _, ok := g.defs[name]; !ok {
			g.defs[name] = JSONSchema{} // a cycle refers to it
			g.defs[name] = g.object(t)
		}
		return JSONSchema{Ref: "#/$defs/" + name}
	}
	panic(fmt.Sprintf("appapi schema: no schema for %s", t))
}

// object is struct t's schema: its JSON fields, required unless
// omitempty or omitzero.
func (g *schemaGen) object(t reflect.Type) JSONSchema {
	s := JSONSchema{Type: "object", Properties: map[string]JSONSchema{}}
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		s.Properties[name] = g.of(f.Type)
		s.Order = append(s.Order, name)
		if !strings.Contains(","+opts+",", ",omitempty,") && !strings.Contains(","+opts+",", ",omitzero,") {
			s.Required = append(s.Required, name)
		}
	}
	return s
}
