//go:build !linux && !darwin

package readline

func anchorTerminal() func() { return nil }
