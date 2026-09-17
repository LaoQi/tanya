//go:build !linux && !darwin

package ctty

import (
	"os"
	"syscall"
)

var exitSignals = []os.Signal{syscall.SIGTERM}

var interruptSignals = []os.Signal{os.Interrupt}

func emergencyRestore() {}
