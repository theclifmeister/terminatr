package appapi

import (
	"reflect"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// safe is v with every string under it, map keys among them, through
// textsafe.Text: an agent's, a mod's or a paired server's words reach
// the app as plain text, newlines and tabs kept. v itself is left as it
// was: lists, maps and pointers under it are copied.
func safe(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.New(reflect.TypeOf(v)).Elem()
	rv.Set(reflect.ValueOf(v))
	clean(rv)
	return rv.Interface()
}

func clean(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if s := v.String(); !textsafe.Safe(s) {
			v.SetString(textsafe.Text(s))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			c := reflect.New(v.Type().Elem())
			c.Elem().Set(v.Elem())
			clean(c.Elem())
			v.Set(c)
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(c, v)
		for i := range c.Len() {
			clean(c.Index(i))
		}
		v.Set(c)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				clean(v.Field(i))
			}
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		m := reflect.MakeMapWithSize(v.Type(), v.Len())
		it := v.MapRange()
		for it.Next() {
			k := reflect.New(v.Type().Key()).Elem()
			k.Set(it.Key())
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(it.Value())
			clean(k)
			clean(e)
			m.SetMapIndex(k, e)
		}
		v.Set(m)
	}
}
