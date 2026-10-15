package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// fakeServer answers each request on conn with answer(req), after delay.
func fakeServer(t *testing.T, conn net.Conn, delay time.Duration, answer func(proto.Request) proto.Response) {
	t.Helper()
	go func() {
		br := bufio.NewReader(conn)
		for {
			var req proto.Request
			if err := proto.ReadLine(br, &req); err != nil {
				return
			}
			time.Sleep(delay)
			if err := proto.WriteLine(conn, answer(req)); err != nil {
				return
			}
		}
	}()
}

// TestCallWithinTimeoutBreaksTheClient: an answer that comes after
// CallWithin gave up is never taken for the next call's.
func TestCallWithinTimeoutBreaksTheClient(t *testing.T) {
	cc, sc := net.Pipe()
	t.Cleanup(func() { cc.Close(); sc.Close() })
	fakeServer(t, sc, 100*time.Millisecond, func(req proto.Request) proto.Response {
		return proto.Response{ID: req.ID, Result: json.RawMessage(`{"method":"` + req.Method + `"}`)}
	})
	c := StreamOver(cc)
	var res struct{ Method string }
	err := c.CallWithin(20*time.Millisecond, "slow", nil, &res)
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("first call: %v, want a timeout", err)
	}
	time.Sleep(200 * time.Millisecond) // the late answer arrives
	start := time.Now()
	// Within a deadline, so a Client that tries the connection again
	// fails the test instead of hanging it (net.Pipe has no buffer).
	err = c.CallWithin(time.Second, "next", nil, &res)
	if err == nil || res.Method != "" {
		t.Fatalf("second call: %v, result %+v; want it refused", err, res)
	}
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("second call: %v, want it to wrap the timeout", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("second call took %v, want it to fail at once", time.Since(start))
	}
}

// TestCallChecksTheAnswersID: an answer to another request is an error,
// and the Client is broken after.
func TestCallChecksTheAnswersID(t *testing.T) {
	cc, sc := net.Pipe()
	t.Cleanup(func() { cc.Close(); sc.Close() })
	fakeServer(t, sc, 0, func(req proto.Request) proto.Response {
		return proto.Response{ID: req.ID + 1, Result: json.RawMessage(`{}`)}
	})
	c := StreamOver(cc)
	if err := c.Call("a", nil, nil); err == nil {
		t.Fatal("an answer to another request was taken")
	}
	if err := c.Call("b", nil, nil); err == nil {
		t.Fatal("a broken Client called again")
	}
}

// TestCallsAreOneAtATime: Calls from many goroutines each get their own
// answer.
func TestCallsAreOneAtATime(t *testing.T) {
	cc, sc := net.Pipe()
	t.Cleanup(func() { cc.Close(); sc.Close() })
	fakeServer(t, sc, 0, func(req proto.Request) proto.Response {
		return proto.Response{ID: req.ID, Result: json.RawMessage(`{"method":"` + req.Method + `"}`)}
	})
	c := StreamOver(cc)
	errs := make(chan error, 20)
	for i := range 20 {
		go func() {
			method := string(rune('a' + i))
			var res struct{ Method string }
			err := c.Call(method, nil, &res)
			if err == nil && res.Method != method {
				err = errors.New(method + " got " + res.Method + "'s answer")
			}
			errs <- err
		}()
	}
	for range 20 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
