package agent

import (
	"errors"
	"strings"
	"testing"
)

func lookPathStub(existing ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, e := range existing {
			if e == name {
				return name, nil
			}
		}
		base := name
		if i := strings.LastIndexAny(base, `/\`); i >= 0 {
			base = base[i+1:]
		}
		for _, e := range existing {
			if e == base {
				return name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestFirstAvailablePosixChain(t *testing.T) {
	posix := []string{"bash", "sh", "ash"}
	p, err := firstAvailable(posix, lookPathStub("bash", "sh"))
	if err != nil || p == nil || p.Name != "bash" || p.Kind != KindPosix || p.arg() != "-c" {
		t.Errorf("bash 应优先: %+v err=%v", p, err)
	}
	p, err = firstAvailable(posix, lookPathStub("sh"))
	if err != nil || p == nil || p.Name != "sh" {
		t.Errorf("无 bash 应落 sh: %+v err=%v", p, err)
	}
	p, err = firstAvailable(posix, lookPathStub("ash"))
	if err != nil || p == nil || p.Name != "ash" {
		t.Errorf("仅 ash 应落 ash: %+v err=%v", p, err)
	}
	if p, err := firstAvailable(posix, lookPathStub()); err == nil || p != nil {
		t.Errorf("全落空应报错: %+v err=%v", p, err)
	}
}

func TestFirstAvailablePowerShellChain(t *testing.T) {
	ps := []string{"pwsh", "powershell"}
	p, err := firstAvailable(ps, lookPathStub("pwsh"))
	if err != nil || p == nil || p.Kind != KindPowerShell || p.arg() != "-Command" {
		t.Fatalf("pwsh: %+v err=%v", p, err)
	}
	if strings.Join(p.ExtraArgs, " ") != "-NoProfile -NonInteractive" {
		t.Errorf("ExtraArgs: %v", p.ExtraArgs)
	}
	p, err = firstAvailable(ps, lookPathStub("pwsh", "powershell"))
	if err != nil || p == nil || p.Name != "pwsh" {
		t.Errorf("pwsh 应优先于 powershell: %+v err=%v", p, err)
	}
	p, err = firstAvailable(ps, lookPathStub("powershell"))
	if err != nil || p == nil || p.Name != "powershell" || p.Kind != KindPowerShell || p.arg() != "-Command" {
		t.Fatalf("无 pwsh 应兜底 powershell: %+v err=%v", p, err)
	}
	if strings.Join(p.ExtraArgs, " ") != "-NoProfile -NonInteractive" {
		t.Errorf("powershell ExtraArgs: %v", p.ExtraArgs)
	}
	if p, err := firstAvailable(ps, lookPathStub("cmd")); err == nil {
		t.Errorf("候选链不含 cmd: %+v", p)
	} else if !strings.Contains(err.Error(), "pwsh/powershell") {
		t.Errorf("报错应列出候选 pwsh/powershell: %v", err)
	}
	if p, err := firstAvailable(ps, lookPathStub()); err == nil || p != nil {
		t.Errorf("全落空应报错: %+v err=%v", p, err)
	}
}

func TestResolveProfileUsesPlatformCandidates(t *testing.T) {
	first := platform.Candidates[0]
	p, err := resolveProfile("", lookPathStub(first))
	if err != nil || p == nil || p.Name != first {
		t.Fatalf("平台首选 %q 应被选中: %+v err=%v", first, p, err)
	}
	_, err = resolveProfile("", lookPathStub("tanya-no-such-shell"))
	if err == nil || !strings.Contains(err.Error(), strings.Join(platform.Candidates, "/")) {
		t.Errorf("报错应列出平台候选链: %v", err)
	}
}

func TestNewProfileKinds(t *testing.T) {
	p := newProfile(`C:\Windows\system32\cmd.exe`)
	if p.Kind != KindCmd || p.arg() != "/c" || p.Name != "cmd" {
		t.Errorf("cmd: %+v", p)
	}
	if strings.Join(p.ExtraArgs, " ") != "/d /s" {
		t.Errorf("ExtraArgs: %v", p.ExtraArgs)
	}
	p = newProfile("/usr/bin/zsh")
	if p.Kind != KindPosix || p.Name != "zsh" || p.arg() != "-c" {
		t.Errorf("未知名按 posix: %+v", p)
	}
	p = newProfile("/opt/pwsh/pwsh")
	if p.Kind != KindPowerShell || p.Name != "pwsh" {
		t.Errorf("pwsh: %+v", p)
	}
}

func TestResolveProfileOverride(t *testing.T) {
	p, err := resolveProfile("/usr/bin/fish", lookPathStub("fish", "bash"))
	if err != nil || p == nil || p.Name != "fish" || p.Path != "/usr/bin/fish" {
		t.Errorf("override 优先: %+v err=%v", p, err)
	}
	if p, err := resolveProfile("/no/such/shell", lookPathStub("bash")); err == nil || p != nil {
		t.Errorf("override 落空应报错: %+v err=%v", p, err)
	}
}

func TestProbePrograms(t *testing.T) {
	got := probePrograms([]string{"ls", "cat", "rg", "python"}, lookPathStub("ls", "cat", "rg", "python"))
	if strings.Join(got, ", ") != "ls, cat, rg, python" {
		t.Errorf("probePrograms = %v", got)
	}
}

func TestToolDefsHasRunShell(t *testing.T) {
	defs := newToolRegistry(&shellTool{profile: &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}}).defs()
	if len(defs) == 0 || defs[0].Function.Name != "run_shell" {
		t.Errorf("run_shell 应恒定注册在首位: %+v", defs)
	}
}

func TestToolDefsRunShellDesc(t *testing.T) {
	tool := &shellTool{
		profile:  &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix},
		programs: []string{"ls", "grep"},
	}
	defs := newToolRegistry(tool).defs()
	if len(defs) == 0 || defs[0].Function.Name != "run_shell" {
		t.Fatalf("run_shell 应注册在首位: %+v", defs)
	}
	if got := defs[0].Function.Description; got != tool.toolDesc() {
		t.Errorf("描述应与 toolDesc 一致:\n got %q\nwant %q", got, tool.toolDesc())
	}
	if got := string(defs[0].Function.Parameters); got != runShellParams() {
		t.Errorf("参数应与 runShellParams 一致:\n got %q\nwant %q", got, runShellParams())
	}
}

func TestNewRejectsUnavailableShellOverride(t *testing.T) {
	isolatePromptEnv(t)
	cfg := defaultConfig()
	cfg.Shell = "/no/such/shell-tanya"
	cfg.DataDir = t.TempDir()
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "配置的 shell") {
		t.Fatalf("无可用 shell 时 New 应报错: %v", err)
	}
}

func TestResolveShell(t *testing.T) {
	inv, err := ResolveShell(&Config{})
	if err != nil {
		t.Skipf("当前环境无可解析 shell: %v", err)
	}
	if len(inv.Argv) < 2 {
		t.Fatalf("argv 应含解释器与执行参数: %q", inv.Argv)
	}
	last := inv.Argv[len(inv.Argv)-1]
	want := map[ShellKind]string{KindPosix: "-c", KindPowerShell: "-Command", KindCmd: "/c"}[inv.Kind]
	if last != want {
		t.Errorf("kind %v 的执行参数应为 %q: %q", inv.Kind, want, last)
	}
}

func TestResolveShellBadOverride(t *testing.T) {
	if _, err := ResolveShell(&Config{Shell: "tanya-no-such-shell-xyz"}); err == nil {
		t.Error("不可用的 shell 覆盖应报错")
	}
}
