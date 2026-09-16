//go:build !linux && !darwin && !windows

package ctty

func IsTerminal(int) bool { return false }

func Size(int) (int, int, bool) { return 0, 0, false }

func EnableVT(int) bool { return false }

func ConsoleKind() string { return "" }
