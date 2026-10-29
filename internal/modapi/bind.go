package modapi

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/dop251/goja"

	"github.com/theclifmeister/terminatr/internal/modhost"
)

// Bind adds Methods to the host's $ (modhost.Options.Bind): each one
// sends its call to the server's broker and returns a promise of the
// answer, except the local ones, which hello answers.
func Bind(h *modhost.Host) error {
	vm := h.VM()
	parse, ok := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("parse"))
	if !ok {
		return fmt.Errorf("no JSON.parse")
	}
	for _, m := range Methods {
		obj, name, err := parent(vm, h.Dollar(), m.Name)
		if err != nil {
			return err
		}
		var fn func(goja.FunctionCall) goja.Value
		switch {
		case m.Name == "settings":
			fn = func(c goja.FunctionCall) goja.Value {
				hello := h.Hello()
				raw := string(hello.Settings)
				// $.settings(project): that project's, when it sets
				// some of its own; all projects' otherwise.
				if p := c.Argument(0); !goja.IsUndefined(p) && !goja.IsNull(p) {
					if _, ok := p.Export().(string); !ok {
						panic(vm.NewTypeError("settings: project must be a project's slug"))
					}
					if own, ok := hello.Projects[p.String()]; ok {
						raw = string(own)
					}
				}
				if raw == "" || raw == "null" {
					return vm.NewObject()
				}
				v, err := parse(goja.Undefined(), vm.ToValue(raw))
				if err != nil {
					panic(err)
				}
				return v
			}
		case m.Name == "api.has":
			fn = func(c goja.FunctionCall) goja.Value {
				return vm.ToValue(Has(args(vm, m, c).Get("name").String()))
			}
		case m.Local:
			return fmt.Errorf("%s: a local method with no binding", m.Name)
		default:
			fn = func(c goja.FunctionCall) goja.Value {
				params := goja.Value(goja.Undefined())
				if len(m.Params) > 0 {
					params = args(vm, m, c)
				}
				return vm.ToValue(h.Call(m.Name, params))
			}
		}
		if err := obj.Set(name, fn); err != nil {
			return err
		}
	}
	return nil
}

// parent returns the object under $ that holds the method named path
// ("ui.cell.set": $.ui.cell), making the objects on the way, and the
// method's own name.
func parent(vm *goja.Runtime, dollar *goja.Object, path string) (*goja.Object, string, error) {
	parts := strings.Split(path, ".")
	obj := dollar
	for _, p := range parts[:len(parts)-1] {
		v := obj.Get(p)
		if v == nil || goja.IsUndefined(v) {
			next := vm.NewObject()
			if err := obj.Set(p, next); err != nil {
				return nil, "", err
			}
			obj = next
			continue
		}
		next, ok := v.(*goja.Object)
		if _, fn := goja.AssertFunction(v); !ok || fn {
			return nil, "", fmt.Errorf("%s: $.%s is not an object", path, p)
		}
		obj = next
	}
	return obj, parts[len(parts)-1], nil
}

// args names a call's positional arguments by m's params, as the params
// object sent on the wire. A required argument that is missing, or a
// string one that isn't a string, throws a TypeError in the mod.
func args(vm *goja.Runtime, m Method, c goja.FunctionCall) *goja.Object {
	o := vm.NewObject()
	for i, p := range m.Params {
		a := c.Argument(i)
		if goja.IsUndefined(a) {
			if !p.Optional {
				panic(vm.NewTypeError(fmt.Sprintf("%s: %s is missing", signature(m), p.Name)))
			}
			continue
		}
		if p.Type.Kind() == reflect.String && a.ExportType() != reflect.TypeFor[string]() {
			panic(vm.NewTypeError(fmt.Sprintf("%s: %s is not a string", signature(m), p.Name)))
		}
		_ = o.Set(p.Name, a)
	}
	return o
}

// signature is "$.project.get(slug)", for errors.
func signature(m Method) string {
	names := make([]string, len(m.Params))
	for i, p := range m.Params {
		names[i] = p.Name
	}
	return "$." + m.Name + "(" + strings.Join(names, ", ") + ")"
}
