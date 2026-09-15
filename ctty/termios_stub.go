//go:build !linux && !darwin

package ctty

import "errors"

type Termios struct{}

func GetTermios(fd int) (Termios, error) {
	return Termios{}, errors.New("ctty: termios 在当前平台不可用")
}

func SetTermios(fd int, t Termios) error {
	return errors.New("ctty: termios 在当前平台不可用")
}

func SetTermiosFlush(fd int, t Termios) error {
	return errors.New("ctty: termios 在当前平台不可用")
}
