//go:build testhooks

package cli

import "time"

// testHookWait is the least total tm hook gives an event, from
// TERMINATR_TEST_HOOK_WAIT (a Go duration) in a tm built with -tags
// testhooks (docs/SPEC.md §15.2, Test hooks in the server); 0 when unset. The e2e
// harness sets it: on a loaded CI runner a hook process can be held up
// past hookAck before it writes, and the event the scenario counts on is
// lost (T890).
func testHookWait(getenv func(string) string) time.Duration {
	d, err := time.ParseDuration(getenv("TERMINATR_TEST_HOOK_WAIT"))
	if err != nil || d < 0 {
		return 0
	}
	return d
}
