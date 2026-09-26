//go:build !linux && !darwin && !windows

package readline

func openTerminal() (device, error) {
	return nil, ErrUnsupported
}
