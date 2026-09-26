//go:build linux

package shell

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LaoQi/tanya/readline"
	"golang.org/x/sys/unix"
)

func foregroundPgrp(t *testing.T) (int, bool) {
	t.Helper()
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return 0, false
	}
	defer tty.Close()
	pgrp, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return 0, false
	}
	return pgrp, true
}

func requireForegroundTTY(t *testing.T) int {
	t.Helper()
	ProtectTerminalSignals()
	pgrp, ok := foregroundPgrp(t)
	if !ok {
		t.Skip("无控制终端")
	}
	if pgrp != syscall.Getpgrp() {
		t.Skip("当前进程组非前台（嵌套/后台环境）")
	}
	return pgrp
}

func TestRunShellKeepsTerminalForeground(t *testing.T) {
	mine := requireForegroundTTY(t)
	res := consoleTool(t, rlConsole{readline.NewConsole()}).run(context.Background(), request{
		Command:    `awk '{print "TPGID=" $8}' /proc/self/stat`,
		TimeoutSec: 10,
	})
	want := "TPGID=" + strconv.Itoa(mine)
	if !strings.Contains(shellOut(res), want) {
		t.Fatalf("子进程所见终端前台组应为 tanya 自身（%s）: %+v", want, res)
	}
	pgrp, ok := foregroundPgrp(t)
	if !ok || pgrp != mine {
		t.Fatalf("执行后终端前台组漂移: %d want %d", pgrp, mine)
	}
}

func TestRunShellStdinIsNullDevice(t *testing.T) {
	start := time.Now()
	res := testShellTool(t).run(context.Background(), request{
		Command:    `read -r line; echo "RC=$? LINE=${line:-none}"`,
		TimeoutSec: 10,
	})
	elapsed := time.Since(start)
	out := shellOut(res)
	if !strings.Contains(out, "RC=1") || !strings.Contains(out, "LINE=none") {
		t.Fatalf("stdin 应接空设备（read 立即 EOF）: %+v", res)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stdin 未接空设备，read 阻塞了 %v", elapsed)
	}
}
