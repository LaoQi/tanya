//go:build illumos || ios

package readline

func newUnixTerminal() (Terminal, error) {
	return nil, ErrUnsupported
}
