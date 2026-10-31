//go:build !testhooks

package cli

import "time"

// testHookWait: a release build ignores TERMINATR_TEST_HOOK_WAIT.
func testHookWait(func(string) string) time.Duration { return 0 }
