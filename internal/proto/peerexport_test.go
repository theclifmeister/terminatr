package proto

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

// fill sets every field under v to a value that isn't zero, strings to
// "SECRET".
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("SECRET")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64:
		v.SetInt(7)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0))
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(7, 0)))
			return
		}
		for i := range v.NumField() {
			fill(v.Field(i))
		}
	default:
		panic("fill: " + v.Type().String())
	}
}

// zeros are the paths of the fields under v that are zero.
func zeros(path string, v reflect.Value, out *[]string) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			*out = append(*out, path)
			return
		}
		zeros(path, v.Elem(), out)
	case reflect.Slice:
		if v.Len() == 0 {
			*out = append(*out, path)
			return
		}
		zeros(path+"[]", v.Index(0), out)
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			if v.IsZero() {
				*out = append(*out, path)
			}
			return
		}
		for i := range v.NumField() {
			zeros(path+"."+v.Type().Field(i).Name, v.Field(i), out)
		}
	default:
		if v.IsZero() {
			*out = append(*out, path)
		}
	}
}

// TestPeerProjectWatch: of a ProjectWatch with every field set, a paired
// server gets every field but the mods' cells and the inbox's words, and
// questions as PeerQuestion. A field added to any of its records fails
// here until it is decided: copied in PeerProjectWatch, or listed below
// as staying home.
func TestPeerProjectWatch(t *testing.T) {
	var w ProjectWatch
	fill(reflect.ValueOf(&w).Elem())
	out := PeerProjectWatch(w)
	var home []string
	zeros("", reflect.ValueOf(out), &home)
	want := []string{".Inbox[].ID", ".Inbox[].Subject", ".Inbox[].Summary", ".Inbox[].What", ".Cells"}
	slices.Sort(home)
	slices.Sort(want)
	if !slices.Equal(home, want) {
		t.Errorf("fields left home %v, want %v", home, want)
	}
	if out.NeedsYou[0].Question != PeerQuestion || out.Threads[0].NeedsYou != PeerQuestion {
		t.Errorf("questions: %q, %q", out.NeedsYou[0].Question, out.Threads[0].NeedsYou)
	}
	if w.Threads[0].NeedsYou != "SECRET" {
		t.Error("PeerProjectWatch changed its argument")
	}
	if e := PeerProjectWatch(ProjectWatch{}); e.NeedsYou == nil || e.Inbox == nil || e.Threads == nil || e.Ready == nil {
		t.Errorf("empty lists became null: %+v", e)
	}
}
