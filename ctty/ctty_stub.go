//go:build !linux && !darwin

package ctty

import (
	"errors"
	"os"
)

const Supported = false

func Open() (*os.File, error) {
	return nil, errors.New("ctty: 控制终端能力在当前平台不可用")
}

func OwnPgrp() int { return -1 }

func ForegroundPgrp(fd int) (int, bool) { return 0, false }

func SetForeground(fd, pgrp int) bool { return false }

func IsForeground(fd int) bool { return false }

func IgnoreJobSignals() {}

func ResetModes(tty *os.File) bool { return false }
