// Package server is the background process (`tm server`): it owns every PTY,
// emulator and agent process, the ticker and the control socket. It runs
// detached from any terminal (setsid, no controlling tty, stdio closed,
// SIGHUP ignored) and stops only on `tm server stop` (docs/SPEC.md §3).
//
// Tests build a bare &Server{}: a new field or method must work (be
// nil-safe) on one.
//
// A new control method takes its constant in proto, its row in methods
// (methods.go), and its name in humansocket_test.go's humanOnlyMethods or
// notHumanOnly (TestEveryMethodHasItsGate). One the app uses also takes its
// entry in appapi/methods.go, a higher appapi.Version, and in appapi's
// copy_test.go its copy's check (TestCopies, TestParamsCopies) and its name
// in TestAllowlist.
package server
