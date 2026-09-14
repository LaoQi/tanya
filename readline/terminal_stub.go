//go:build !linux && !darwin && !windows

package readline

func newUnixTerminal() (Terminal, error) {
	return nil, ErrUnsupported
}
