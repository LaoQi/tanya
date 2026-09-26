//go:build !linux && !darwin

package ctty

import (
	"os"
)

const Supported = false

func IgnoreJobSignals() {}

func ProtectJobSignals() {}

func ResetModes(tty *os.File) bool { return false }

func SaveCursor(tty *os.File) bool { return false }

func RestoreCursor(tty *os.File) bool { return false }
