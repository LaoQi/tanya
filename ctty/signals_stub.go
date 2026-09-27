//go:build !linux && !darwin && !windows

package ctty

import (
	"os"
	"syscall"
)

var exitSignals = []os.Signal{syscall.SIGTERM}

var interruptSignals = []os.Signal{os.Interrupt}

var resizeSignals []os.Signal

func emergencyRestore() {}
