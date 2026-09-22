//go:build linux || darwin

package ctty

import "os"

func IsRoot() bool { return os.Geteuid() == 0 }
