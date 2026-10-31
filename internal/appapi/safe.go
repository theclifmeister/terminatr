package appapi

import (
	"encoding/json"
	"reflect"
	"unicode/utf8"

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

// MaxText bounds a long text field of an answer (a memory note's
// Markdown) in JSON, escapes included: with the rest of the answer it
// stays under a device's link's line (MaxLine, T892).
const MaxText = 768 << 10

// fitText is s cut, at a character, to the longest start whose JSON is
// at most max bytes, and whether it was cut.
func fitText(s string, max int) (string, bool) {
	if jsonLen(s) <= max {
		return s, false
	}
	lo, hi := 0, len(s) // lo fits, hi doesn't
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if jsonLen(s[:mid]) <= max {
			lo = mid
		} else {
			hi = mid
		}
	}
	for lo > 0 && !utf8.RuneStart(s[lo]) {
		lo--
	}
	return s[:lo], true
}

// jsonLen is the length of s as a JSON string.
func jsonLen(s string) int {
	b, _ := json.Marshal(s)
	return len(b)
}
