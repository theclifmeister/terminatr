// Package caps is what the platform tm runs on can do, as constants the
// rest of tm reads instead of runtime.GOOS.
package caps

import "runtime"

// OS is the platform tm was built for, as runtime.GOOS names it: what
// the per-platform tables (config.SandboxSettings) are keyed by.
const OS = runtime.GOOS
