// Package ticker is the server's event loop and sweep (docs/SPEC.md
// §7.5, M7). It turns thread state, report and PR changes into inbox
// items, nudges an idle coordinator about new items, follows up on a
// thread's failing PR checks, and resolves a thread once its PR merged.
//
// Inbox items and coordinator prompts are built from fixed text and ids
// only: report bodies, PR titles and comments never reach them, because
// data is not instructions (§11.2). The one exception is a thread's
// follow-up on failing PR checks: it carries a bounded excerpt of the
// failing CI job's log (internal/codehost cilog.go: logBytes, logRuns),
// marked as data, not instructions, and goes only to that thread.
package ticker
