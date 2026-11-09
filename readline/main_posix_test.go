//go:build linux || darwin

package readline

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// ttyE2EEnv 是「本进程有真实 tty 可用」的开关：由 TestBridgeProductionTTYE2E 等
// 需要真终端的用例识别（用法见用例的 Skip 文案——用 script 包一层再置位）。
const ttyE2EEnv = "TTY_BRIDGE_E2E"

// detachControllingTTY 让测试进程脱离调用方的控制终端（有真实 tty 用例时不动）。
//
// 为什么必须脱离：不少用例会走到 anchorTerminal()——它 ctty.Open() 打开真实
// /dev/tty 并写入光标锚点（`ESC 7`），Release 时再写复位序列。交互式 shell 里
// 这是正常的；但 CI / agent 环境常出现「继承了控制终端、却没人读它的写端」：
// 终端输出缓冲填满后 Write 永久阻塞在内核态，连 -test.timeout 的 SIGQUIT 栈转储
// 都投递不进去（实测同一用例：继承终端时永久卡死，setsid 后 0.02s 通过）。
//
// Setsid 之后 ctty.Open() 失败、anchorTerminal 返回 nil，普通用例不受影响；
// 而真需要终端的用例（ttyE2EEnv 置位时）**必须保留**控制终端，否则
// /dev/tty 打不开（ENXIO），故先判开关。已是会话首进程时 Setsid 返回 EPERM，容错即可。
func detachControllingTTY() {
	if os.Getenv(ttyE2EEnv) != "" {
		return
	}
	_, _ = unix.Setsid()
}

func TestMain(m *testing.M) {
	detachControllingTTY()
	os.Exit(m.Run())
}
