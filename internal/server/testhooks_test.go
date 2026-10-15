package server

import "testing"

// useTestHooks makes this process's server honour the test hooks.
func useTestHooks(t *testing.T) {
	t.Helper()
	testHooksOn = true
	t.Cleanup(func() { testHooksOn = testHooksBuilt })
}

// TestTestHooksNeedTheTag: without -tags testhooks a hook in the
// environment changes nothing.
func TestTestHooksNeedTheTag(t *testing.T) {
	if testHooksBuilt {
		t.Skip("built with testhooks")
	}
	t.Setenv(testHelloEnv, "deaf")
	if n, deaf := testHello(); n != 0 || deaf {
		t.Fatalf("hello hook honoured: %d %v", n, deaf)
	}
	t.Setenv(testHelloEnv, "protocol=1")
	if n, _ := testHello(); n != 0 {
		t.Fatalf("protocol hook honoured: %d", n)
	}
}

func TestTestHooksParse(t *testing.T) {
	useTestHooks(t)
	t.Setenv(testHelloEnv, "protocol=3")
	if n, deaf := testHello(); n != 3 || deaf {
		t.Fatalf("got %d %v", n, deaf)
	}
	t.Setenv(testHelloEnv, "deaf")
	if _, deaf := testHello(); !deaf {
		t.Fatal("deaf not honoured")
	}
}
