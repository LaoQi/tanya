//go:build windows

package ctty

import (
	"os"
	"syscall"
)

var exitSignals = []os.Signal{syscall.SIGTERM}

var interruptSignals = []os.Signal{os.Interrupt}

// resizeSignals 为空：Win32 无窗口尺寸变更信号（resize 事件不产出）
var resizeSignals []os.Signal

func emergencyRestore() { RestoreUTF8() }
