//go:build windows

package ctty

import (
	"os"

	"golang.org/x/sys/windows"
)

func IsTerminal(fd int) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(fd), &mode) == nil
}

func Size(fd int) (int, int, bool) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(fd), &info); err != nil {
		return 0, 0, false
	}
	cols := int(info.Window.Right-info.Window.Left) + 1
	rows := int(info.Window.Bottom-info.Window.Top) + 1
	if cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return cols, rows, true
}

func ConsoleMode(fd int) (uint32, bool) {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &mode); err != nil {
		return 0, false
	}
	return mode, true
}

func SetConsoleMode(fd int, mode uint32) bool {
	return windows.SetConsoleMode(windows.Handle(fd), mode) == nil
}

func EnableVT(fd int) bool {
	mode, ok := ConsoleMode(fd)
	if !ok {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return SetConsoleMode(fd, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}

var procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")

func Open() (*os.File, error) {
	return os.OpenFile("CONIN$", os.O_RDWR, 0)
}

func IgnoreCtrlEvents() {
	procSetConsoleCtrlHandler.Call(0, 1)
}

func RestoreCtrlEvents() {
	procSetConsoleCtrlHandler.Call(0, 0)
}

func ConsoleKind() string {
	if os.Getenv("WT_SESSION") != "" {
		return "windows-terminal"
	}
	if os.Getenv("TERM_PROGRAM") != "" {
		return "conpty"
	}
	return "console"
}
