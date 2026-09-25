package shell

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestPlatformComplete(t *testing.T) {
	if platform.GOOS == "" || platform.GOOS != runtime.GOOS {
		t.Errorf("平台表 GOOS 应与运行时一致: 表=%q 运行时=%q", platform.GOOS, runtime.GOOS)
	}
	if len(platform.Candidates) == 0 {
		t.Fatal("平台候选链不得为空")
	}
	for _, c := range platform.Candidates {
		if c == "" {
			t.Errorf("候选不得为空串: %v", platform.Candidates)
		}
	}
	if platform.ConfigureGroup == nil || platform.KillGroup == nil || platform.ProtectSignals == nil ||
		platform.ExitCode == nil || platform.ProcessStopped == nil || platform.Capabilities == nil ||
		platform.DecodeOutput == nil {
		t.Errorf("平台能力存在缺项: %+v", platform)
	}
	for _, p := range platform.Programs {
		if p == "" {
			t.Errorf("程序清单不得含空串: %v", platform.Programs)
		}
	}
}

func TestFillDefaults(t *testing.T) {
	p := fillDefaults(shellPlatform{})
	if p.ConfigureGroup == nil || p.KillGroup == nil || p.ProtectSignals == nil ||
		p.ExitCode == nil || p.ProcessStopped == nil || p.Capabilities == nil ||
		p.DecodeOutput == nil {
		t.Fatalf("零值表应补全函数字段: %+v", p)
	}
	cmd := &exec.Cmd{}
	p.ConfigureGroup(cmd)
	if cmd.SysProcAttr != nil {
		t.Errorf("默认进程组配置应为 no-op: %+v", cmd.SysProcAttr)
	}
	p.ProtectSignals()
	if got := p.Capabilities(&profile{Name: "bash", Kind: KindPosix}); got != "" {
		t.Errorf("默认能力句应为空: %q", got)
	}
	if p.ProcessStopped(1) {
		t.Error("默认挂起探测应恒 false")
	}
	if err := p.KillGroup(&exec.Cmd{}); err != os.ErrProcessDone {
		t.Errorf("默认杀进程应返回 ErrProcessDone: %v", err)
	}
	if code, ok := p.ExitCode(nil); ok || code != 0 {
		t.Errorf("nil 错误不应给出退出码: code=%d ok=%v", code, ok)
	}
	if code, ok := p.ExitCode(errors.New("boom")); ok || code != 0 {
		t.Errorf("非 ExitError 不应给出退出码: code=%d ok=%v", code, ok)
	}
}
