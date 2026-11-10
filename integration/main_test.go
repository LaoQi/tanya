//go:build linux

package integration

import (
	"os"
	"testing"

	"github.com/LaoQi/battlelab/tools/shell"
)

// TestMain 让测试二进制也能作为「会话宿主」被自举（run_shell 的 interactive 路径
// 会用当前可执行文件启动宿主进程）。宿主模式下不返回。
func TestMain(m *testing.M) {
	shell.RunSessionHostIfRequested()
	os.Exit(m.Run())
}
