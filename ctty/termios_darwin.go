//go:build darwin

package ctty

import "golang.org/x/sys/unix"

type Termios = unix.Termios

func GetTermios(fd int) (Termios, error) {
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return Termios{}, err
	}
	return *t, nil
}

func SetTermios(fd int, t Termios) error {
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, &t)
}

func SetTermiosFlush(fd int, t Termios) error {
	return unix.IoctlSetTermios(fd, unix.TIOCSETAF, &t)
}
