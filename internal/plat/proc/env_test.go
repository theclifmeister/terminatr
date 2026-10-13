package proc

import (
	"slices"
	"testing"
)

// TestMallocEnv checks Linux gets one malloc arena unless the user set
// glibc's malloc tunables, which it gets instead (T360), and that other
// systems get nothing.
func TestMallocEnv(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	for _, tc := range []struct {
		goos string
		kv   map[string]string
		want []string
	}{
		{"linux", nil, []string{"MALLOC_ARENA_MAX=1"}},
		{"linux", map[string]string{"HOME": "/home/u", "GH_TOKEN": "x"}, []string{"MALLOC_ARENA_MAX=1"}},
		{"linux", map[string]string{"MALLOC_ARENA_MAX": "4"}, []string{"MALLOC_ARENA_MAX=4"}},
		{"linux", map[string]string{"GLIBC_TUNABLES": "glibc.malloc.hugetlb=0"}, []string{"GLIBC_TUNABLES=glibc.malloc.hugetlb=0"}},
		{"linux", map[string]string{"GLIBC_TUNABLES": "glibc.malloc.hugetlb=1", "MALLOC_ARENA_MAX": "2"},
			[]string{"GLIBC_TUNABLES=glibc.malloc.hugetlb=1", "MALLOC_ARENA_MAX=2"}},
		{"darwin", map[string]string{"MALLOC_ARENA_MAX": "4"}, nil},
		{"windows", nil, nil},
	} {
		if got := mallocEnv(tc.goos, env(tc.kv)); !slices.Equal(got, tc.want) {
			t.Errorf("mallocEnv(%s, %v) = %q, want %q", tc.goos, tc.kv, got, tc.want)
		}
	}
}
