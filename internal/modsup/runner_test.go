package modsup

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// A host's $.on can subscribe, on the client's read loop, before once has
// recorded its hello: both go through hostEvents' lock (T317).
func TestSubscribeBeforeHello(t *testing.T) {
	dir := t.TempDir()
	s := New(Options{CacheDir: filepath.Join(dir, "cache"), LogDir: filepath.Join(dir, "logs")})
	t.Cleanup(s.Close)
	r := newRunner(s, Spec{ID: "m"}, nil)
	r.log = openLog(s.logPath("m"))
	t.Cleanup(func() { r.log.Close() })
	ev := &hostEvents{r: r, bundle: "main.js"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ev.add([]string{"clock.minute"}, nil) // the notification path
	}()
	// Wait for the subscribe's hello record on disk: that orders the two
	// without a happens-before edge the race detector would see.
	for {
		if _, err := os.Stat(filepath.Join(s.cacheDir("m"), helloFile)); err == nil {
			break
		}
		runtime.Gosched()
	}
	ev.hello([]string{"t"}, []string{"session.started"}, nil)
	<-done
	if got := r.status().Events; !slices.Equal(got, []string{"clock.minute", "session.started"}) {
		t.Fatalf("events %v", got)
	}
}
