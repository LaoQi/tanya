//go:build linux

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/LaoQi/battlelab/tools/shell"
)

// vbashTTYTool 装配 shell: vbash 的 Tool：解释器在进程内执行，终端仍由 readline 借出。
func vbashTTYTool(t *testing.T, c shell.Console) *shell.Tool {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("UserHomeDir 不可用: err=%v home=%q", err, home)
	}
	tool, err := shell.New(shell.Config{
		LookPath:  exec.LookPath,
		Home:      home,
		Workspace: func() string { return cwd },
		Console:   c,
		Override:  shell.BuiltinShellName,
		Environ:   os.Environ,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func runVbashArgs(t *testing.T, c shell.Console, args map[string]any) *shell.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res := vbashTTYTool(t, c).Invoke(context.Background(), string(raw))
	r, ok := res.Meta.(*shell.Result)
	if !ok || r == nil {
		t.Fatalf("应返回 *shell.Result: %+v", res)
	}
	return r
}

// vbash 的 interactive 由解释器自己承担：借来的 pty 既是它的 stdio，
// 也是前台子进程的控制终端。命令行上的 read 由解释器内建实现。
func TestVbashInteractiveRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty: printf 'hello\n' | script -qec 'TTY_E2E=1 go test -run TestVbashInteractiveRealTTYE2E -v ./integration' /dev/null")
	}
	r := runVbashArgs(t, newReadlineConsole(), map[string]any{
		"command": `read -r x; echo "got:$x"; sh -c 'tty; (exec 3</dev/tty) && echo devtty-ok'`,
		"timeout": 15, "interactive": true,
	})
	out := stdoutOf(r)
	if !strings.Contains(out, "got:hello") {
		t.Fatalf("内建 read 未读到用户输入: %+v", r)
	}
	if !strings.Contains(out, "/dev/pts/") {
		t.Fatalf("外部命令应看到 pty: %q", out)
	}
	if !strings.Contains(out, "devtty-ok") {
		t.Fatalf("外部命令应能打开 /dev/tty: %q", out)
	}
}

// 非交互对照：stdin 是空设备，read 立即 EOF。
// 注意 /dev/tty 在非交互场景下仍可打开——那是独立的既有问题（普通命令只 Setpgid、
// 不 Setsid，因此沿用调用方的会话与控制终端），不在 interactive 这条路径的范围内，
// 这里只钉住 stdin 语义，不把该现状当成承诺。
func TestVbashNonInteractiveRealTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty（同一组用例共用开关）")
	}
	r := runVbashArgs(t, newReadlineConsole(), map[string]any{
		"command": `read -r x; echo "rc=$? x=[$x]"`,
		"timeout": 15,
	})
	if out := stdoutOf(r); !strings.Contains(out, "rc=1 x=[]") {
		t.Errorf("非交互的 stdin 应为空设备: %q", out)
	}
}

// 模拟 sudo/pinentry 的取终端方式：**不使用 stdin**，直接打开 /dev/tty 读写。
// 这是 interactive 存在的本质理由——只有真正的控制终端才走得通。
func TestVbashInteractiveTTYPasswordStyle(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty（同一组用例共用开关）")
	}
	r := runVbashArgs(t, newReadlineConsole(), map[string]any{
		"command": `sh -c 'IFS= read -r p < /dev/tty; echo "via-dev-tty:$p"'`,
		"timeout": 15, "interactive": true,
	})
	if out := stdoutOf(r); !strings.Contains(out, "via-dev-tty:sekret") {
		t.Fatalf("/dev/tty 上应能读到用户输入: %+v", r)
	}
}
