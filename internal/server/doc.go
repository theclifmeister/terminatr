// Package server is the background process (`tm server`): it owns every PTY,
// emulator and agent process, the ticker and the control socket. It runs
// detached from any terminal (setsid, no controlling tty, stdio closed,
// SIGHUP ignored) and stops only on `tm server stop` (docs/SPEC.md §3).
//
// Tests build a bare &Server{}: a new field or method must work (be
// nil-safe) on one.
package server
