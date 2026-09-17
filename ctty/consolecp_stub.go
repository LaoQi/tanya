//go:build !windows

package ctty

func ConsoleCP() (uint32, bool) { return 0, false }

func ConsoleOutputCP() (uint32, bool) { return 0, false }

func SetConsoleCP(uint32) bool { return false }

func SetConsoleOutputCP(uint32) bool { return false }

func OEMCP() uint32 { return 0 }

func EnsureUTF8() {}

func RestoreUTF8() {}

func FallbackCP() uint32 { return 0 }

func DecodeCP(_ uint32, b []byte) []byte { return b }
