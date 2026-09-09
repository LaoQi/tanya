//go:build linux || android || aix || solaris

package readline

import "golang.org/x/sys/unix"

func getTermios(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TCGETS)
}

func setTermios(fd int, t *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETS, t)
}

func setTermiosFlush(fd int, t *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETSF, t)
}
