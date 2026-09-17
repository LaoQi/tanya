//go:build !linux && !darwin && !windows

package ctty

type InputModes struct{}

func SnapshotInput(fd int) (InputModes, bool) {
	return InputModes{}, false
}

func RestoreInput(fd int, _ InputModes) bool {
	return false
}
