//go:build !unix || illumos || ios

package readline

func InitTerminalGuard() {}

func SecureTerminal() {}
