//go:build linux

package procarg

import (
	"os"
	"syscall"
)

// Flag 是携带工作区目录的选项名（tanya -C <dir>）。
const Flag = "C"

// Reinject 把当前工作区写进进程 argv 后重启自身，使 ps 显示的命令行可直接复跑。
// 已注入、取不到 cwd/exe、exec 失败时一律返回 false，调用方按原名继续运行。
func Reinject(workspace string) bool {
	if workspace == "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	argv := append([]string{os.Args[0], "-" + Flag, workspace}, os.Args[1:]...)
	if err := syscall.Exec(exe, argv, os.Environ()); err != nil {
		return false
	}
	return true
}
