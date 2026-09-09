//go:build !linux

package agent

func processStopped(pid int) bool { return false }
