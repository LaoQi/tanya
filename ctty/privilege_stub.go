//go:build !linux && !darwin

package ctty

func IsRoot() bool { return false }
