//go:build !linux && !darwin && !windows

package readline

func openTerminal() (Terminal, error) {
	return nil, ErrUnsupported
}
