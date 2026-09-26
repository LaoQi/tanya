//go:build linux || darwin

package readline

import "github.com/LaoQi/tanya/ctty"

var lendOpenTTY = ctty.Open

func anchorTerminal() func() {
	tty, err := lendOpenTTY()
	if err != nil || tty == nil {
		return nil
	}
	fd := int(tty.Fd())
	saved, savedErr := ctty.GetTermios(fd)
	hasSaved := savedErr == nil
	if !ctty.SaveCursor(tty) {
		tty.Close()
		return nil
	}
	return func() {
		if hasSaved {
			_ = ctty.SetTermios(fd, saved)
		}
		ctty.ResetModes(tty)
		ctty.RestoreCursor(tty)
		tty.Close()
	}
}
