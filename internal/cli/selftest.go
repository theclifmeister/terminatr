package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// Selftest feeds a line through the emulator to show libghostty-vt is linked.
func Selftest() error {
	t, err := emu.New(80, 24)
	if err != nil {
		return err
	}
	defer t.Close()
	if _, err := t.Write([]byte("ok\r\n")); err != nil {
		return err
	}
	got, err := t.PlainText()
	if err != nil {
		return err
	}
	if got != "ok" {
		return fmt.Errorf("unexpected screen %q", got)
	}
	return nil
}
