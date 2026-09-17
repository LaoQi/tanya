//go:build windows

package ctty

type InputModes struct{ Mode uint32 }

func SnapshotInput(fd int) (InputModes, bool) {
	m, ok := ConsoleMode(fd)
	return InputModes{Mode: m}, ok
}

func RestoreInput(fd int, m InputModes) bool {
	return SetConsoleMode(fd, m.Mode)
}
