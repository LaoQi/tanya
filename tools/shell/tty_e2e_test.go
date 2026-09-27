//go:build linux

// Package shell_test 承载需要真终端（readline.Console）的集成用例。
// 放在外部测试包是刻意的：内部测试包导入 readline 会让 readline 永远无法依赖 tools/shell
// （测试二进制成环），外部测试包没有这个约束。
package shell_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/tools/shell"
	"golang.org/x/sys/unix"
)

// rlConsole 把 readline.Console 适配成 shell.Console（方法签名相同、返回值需转型）。
type rlConsole struct{ con readline.Console }

func (c rlConsole) LendStdin() (shell.Lease, error) {
	l, err := c.con.LendStdin()
	if l == nil {
		return nil, err
	}
	return l.(shell.Lease), err
}

func (c rlConsole) LendFull(cmd *exec.Cmd, capture io.Writer) (shell.Lease, error) {
	l, err := c.con.LendFull(cmd, capture)
	if l == nil {
		return nil, err
	}
	return l.(shell.Lease), err
}

func realTTYTool(t *testing.T, c shell.Console) *shell.Tool {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("UserHomeDir 不可用: err=%v home=%q", err, home)
	}
	tool, err := shell.New(shell.Config{LookPath: exec.LookPath, Home: home, Workspace: func() string { return cwd }, Console: c})
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func runArgs(t *testing.T, c shell.Console, args map[string]any) *shell.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res := realTTYTool(t, c).Invoke(context.Background(), string(raw))
	r, ok := res.Meta.(*shell.Result)
	if !ok || r == nil {
		t.Fatalf("应返回 *shell.Result: %+v", res)
	}
	return r
}

func newReadlineConsole() shell.Console { return rlConsole{readline.NewConsole()} }

func stdoutOf(r *shell.Result) string {
	var b strings.Builder
	for _, c := range r.Stdout {
		b.WriteString(c.Data)
	}
	return b.String()
}

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
	shell.ProtectTerminalSignals()
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
	r := runArgs(t, newReadlineConsole(), map[string]any{
		"command": `awk '{print "TPGID=" $8}' /proc/self/stat`,
		"timeout": 10,
	})
	if want := "TPGID=" + strconv.Itoa(mine); !strings.Contains(stdoutOf(r), want) {
		t.Fatalf("子进程所见终端前台组应为 tanya 自身（%s）: %+v", want, r)
	}
	pgrp, ok := foregroundPgrp(t)
	if !ok || pgrp != mine {
		t.Fatalf("执行后终端前台组漂移: %d want %d", pgrp, mine)
	}
}

func TestRunShellFullRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty: printf 'hello\n' | script -qec 'TTY_E2E=1 go test -run TestRunShellFullRealTTYE2E -v ./tools/shell' /dev/null")
	}
	r := runArgs(t, newReadlineConsole(), map[string]any{
		"command": `read x < /dev/tty; echo got:$x; tty`, "timeout": 15, "interactive": true,
	})
	out := stdoutOf(r)
	if !strings.Contains(out, "got:hello") {
		t.Fatalf("真实 tty 桥接未读到输入: %+v", r)
	}
	if strings.Contains(out, "not a tty") || strings.Contains(out, "/dev/tty\n") {
		t.Fatalf("子进程 tty 非 pty: %q", out)
	}
}

func TestRunShellFullRealTTYReuse(t *testing.T) {
	if os.Getenv("TTY_E2E_REUSE") == "" {
		t.Skip("需真实 tty: (printf 'hello\n'; sleep 3; printf 'world\n'; sleep 3) | script -qec 'TTY_E2E_REUSE=1 go test -count=1 -run TestRunShellFullRealTTYReuse -v ./tools/shell' /dev/null（两次输入必须间隔喂入：一次性写入会被第一个命令的 pty 吃掉）")
	}
	for _, want := range []string{"hello", "world"} {
		r := runArgs(t, newReadlineConsole(), map[string]any{
			"command": `read -r x < /dev/tty; echo got:$x`, "timeout": 15, "interactive": true,
		})
		if out := stdoutOf(r); !strings.Contains(out, "got:"+want) {
			t.Fatalf("第 %q 次运行未读到输入: %+v", want, r)
		}
	}
}

func TestRunShellFullCwdRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty: printf '\n' | script -qec 'TTY_E2E=1 go test -count=1 -run TestRunShellFullCwdRealTTYE2E -v ./tools/shell' /dev/null")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runArgs(t, newReadlineConsole(), map[string]any{"command": "pwd", "timeout": 15, "interactive": true, "cwd": dir})
	got, err := filepath.EvalSymlinks(strings.TrimSpace(stdoutOf(r)))
	if err != nil || got != dir {
		t.Fatalf("桥接下 cwd 未生效: got %q (%v) want %q; res=%+v", got, err, dir, r)
	}
	if r.Cwd != dir {
		t.Errorf("Cwd = %q want %q", r.Cwd, dir)
	}
}

var _ = time.Second
