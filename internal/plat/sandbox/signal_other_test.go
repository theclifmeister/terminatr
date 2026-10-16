//go:build !linux

package sandbox

func signalProbes(func(string, error)) {}
