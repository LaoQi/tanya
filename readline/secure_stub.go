//go:build !linux && !darwin

package readline

func InitTerminalGuard() {}

func SecureTerminal() {}
