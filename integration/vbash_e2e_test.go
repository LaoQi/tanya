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

// 回归：stdin 被重定向的前台命令必须仍能取得控制终端。
// 旧实现拿「子进程的 stdin 是否正好是借出的 pty slave」当判据，把
// `sudo ... < /dev/null` 这类命令误判成后台，它们进了新会话却没有控制终端，
// sudo 于是报「读取密码需要一个终端；请使用 -S 选项…」。
// 现在判据是解释器给出的前台标记，控制终端经额外 fd 交付，与 stdin 形态解耦。
func TestVbashInteractiveRedirectedStdinKeepsTTYE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty（同一组用例共用开关）")
	}
	cases := []struct{ name, command string }{
		{"devnull", `sh -c '(exec 3</dev/tty) 2>/dev/null && echo devtty-ok || echo devtty-fail' < /dev/null`},
		{"left-pipe", `printf x | sh -c '(exec 3</dev/tty) 2>/dev/null && echo devtty-ok || echo devtty-fail'`},
		{"brace", `{ sh -c '(exec 3</dev/tty) 2>/dev/null && echo devtty-ok || echo devtty-fail'; } < /dev/null`},
	}
	for _, c := range cases {
		r := runVbashArgs(t, newReadlineConsole(), map[string]any{
			"command": c.command, "timeout": 15, "interactive": true,
		})
		if out := stdoutOf(r); !strings.Contains(out, "devtty-ok") {
			t.Errorf("%s: stdin 重定向不应影响控制终端: %q", c.name, out)
		}
	}
}

// 回归：多段管道不得因抢控制终端而失败。
// 一个 pts 同时只能属于一个会话，管道各段并发存活；若中间段也 setsid+TIOCSCTTY，
// 第二段会拿到 EPERM，表现为 fork/exec 失败（`cmd | cmd` 报 Permission denied）。
// 现在只有管道末段取终端，其余段只建进程组。
func TestVbashInteractiveMultiSegmentPipelineE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty（同一组用例共用开关）")
	}
	for _, cmd := range []string{
		`echo x | cat`,
		`echo x | cat | cat`,
		`printf 'a\nb\n' | grep b | tr a-z A-Z`,
	} {
		r := runVbashArgs(t, newReadlineConsole(), map[string]any{
			"command": cmd, "timeout": 15, "interactive": true,
		})
		out := stdoutOf(r)
		if strings.Contains(out, "Permission denied") {
			t.Errorf("%q: 管道段抢终端导致失败: %q", cmd, out)
		}
	}
}

// 回归：管道任意位置的 sudo 都必须能取到终端。
// 会话由独立宿主进程持有、各命令只建自己的进程组，所以并发存活的管道各段共享
// 同一个控制终端（与真实 bash 一致），不会出现「读取密码需要一个终端」。
func TestVbashInteractiveSudoAnywhereInPipelineE2E(t *testing.T) {
	if os.Getenv("TTY_E2E") == "" {
		t.Skip("需真实 tty（同一组用例共用开关）")
	}
	cases := []struct{ name, command string }{
		{"first-segment", `sudo -k true | cat`},
		{"middle-segment", `echo x | sudo -k true | cat`},
		{"last-segment", `echo x | sudo -k true`},
		{"redirected-stdin", `sudo -k true < /dev/null`},
	}
	for _, c := range cases {
		r := runVbashArgs(t, newReadlineConsole(), map[string]any{
			"command": c.command, "timeout": 10, "interactive": true,
		})
		out := stdoutOf(r)
		if strings.Contains(out, "读取密码需要一个终端") || strings.Contains(out, "Permission denied") {
			t.Errorf("%s: 管道位置不应影响取终端: %q", c.name, out)
		}
	}
}
