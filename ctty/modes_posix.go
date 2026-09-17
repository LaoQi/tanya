//go:build linux || darwin

package ctty

type InputModes struct{ T Termios }

func SnapshotInput(fd int) (InputModes, bool) {
	t, err := GetTermios(fd)
	return InputModes{T: t}, err == nil
}

func RestoreInput(fd int, m InputModes) bool {
	return SetTermios(fd, m.T) == nil
}
