//go:build !linux && !darwin && !windows

package ctty

import (
	"errors"
	"os"
)

func Open() (*os.File, error) {
	return nil, errors.New("ctty: 控制终端能力在当前平台不可用")
}

func IgnoreCtrlEvents() {}

func RestoreCtrlEvents() {}

func IsTerminal(int) bool { return false }

func Size(int) (int, int, bool) { return 0, 0, false }

func EnableVT(int) bool { return false }

func ConsoleKind() string { return "" }
