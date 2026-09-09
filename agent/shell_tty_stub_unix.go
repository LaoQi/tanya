//go:build illumos || ios

package agent

import "os"

func openForegroundTTY() *os.File { return nil }

func handoverForeground(tty *os.File, pid int) bool { return false }

func restoreForeground(tty *os.File, handed bool) {}
