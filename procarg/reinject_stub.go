//go:build !linux

package procarg

// Flag 是携带工作区目录的选项名（tanya -C <dir>）。
const Flag = "C"

// Reinject 在非 Linux 平台不做任何事：直接返回 false，由调用方按原名继续运行。
func Reinject(string) bool { return false }
