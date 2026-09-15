//go:build linux

package ctty

import "golang.org/x/sys/unix"

type Termios = unix.Termios

func GetTermios(fd int) (Termios, error) {
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return Termios{}, err
	}
	return *t, nil
}

func SetTermios(fd int, t Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETS, &t)
}

func SetTermiosFlush(fd int, t Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETSF, &t)
}
