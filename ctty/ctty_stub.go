//go:build !linux && !darwin

package ctty

import (
	"os"
)

const Supported = false

func OwnPgrp() int { return -1 }

func ForegroundPgrp(fd int) (int, bool) { return 0, false }

func SetForeground(fd, pgrp int) bool { return false }

func IsForeground(fd int) bool { return false }

func IgnoreJobSignals() {}

func ProtectJobSignals() {}

func ResetModes(tty *os.File) bool { return false }

func SaveCursor(tty *os.File) bool { return false }

func RestoreCursor(tty *os.File) bool { return false }
