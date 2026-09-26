//go:build linux || darwin

package ctty

import (
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

const Supported = true

func IsTerminal(fd int) bool {
	_, err := GetTermios(fd)
	return err == nil
}

func Size(fd int) (int, int, bool) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0, false
	}
	return int(ws.Col), int(ws.Row), true
}

func EnableVT(int) bool { return true }

func ConsoleKind() string { return "" }

func Open() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

func IgnoreCtrlEvents() {}

func RestoreCtrlEvents() {}

func IgnoreJobSignals() {
	signal.Ignore(unix.SIGTTIN, unix.SIGTTOU)
}

func ProtectJobSignals() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, unix.SIGTSTP)
	IgnoreJobSignals()
}

const resetModes = "\x1b[0m" +
	"\x0f\x1b(B\x1b)B" +
	"\x1b[?25h\x1b[?7h\x1b[?6l\x1b[?1l" +
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" +
	"\x1b[?2004l\x1b[?1004l" +
	"\x1b[?1049l\x1b[r"

func ResetModes(tty *os.File) bool {
	if tty == nil {
		return false
	}
	_, err := tty.WriteString(resetModes)
	return err == nil
}

func SaveCursor(tty *os.File) bool {
	if tty == nil {
		return false
	}
	_, err := tty.WriteString("\x1b7")
	return err == nil
}

func RestoreCursor(tty *os.File) bool {
	if tty == nil {
		return false
	}
	_, err := tty.WriteString("\x1b8")
	return err == nil
}
