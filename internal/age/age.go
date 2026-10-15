// Package age writes spans of time as short words. It is a leaf package
// (standard library only), so internal/widget, internal/proto and
// internal/tui can share it without importing one another.
package age

import (
	"fmt"
	"time"
)

// Words writes a span of time, short: "40s", "1m20s", "2h5m".
// Negative or tiny spans are "0s".
func Words(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		if s := int(d.Seconds()) % 60; s != 0 {
			return fmt.Sprintf("%dm%ds", int(d.Minutes()), s)
		}
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if m := int(d.Minutes()) % 60; m != 0 {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), m)
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
