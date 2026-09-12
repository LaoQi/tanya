package agent

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func withShellRuntime(t *testing.T, rt *shellRuntime) {
	t.Helper()
	shellRuntimeMu.Lock()
	oldCur, oldSet, oldErr := shellRuntimeCur, shellRuntimeSet, shellRuntimeErr
	shellRuntimeCur, shellRuntimeSet, shellRuntimeErr = rt, true, nil
	shellRuntimeMu.Unlock()
	t.Cleanup(func() {
		shellRuntimeMu.Lock()
		shellRuntimeCur, shellRuntimeSet, shellRuntimeErr = oldCur, oldSet, oldErr
		shellRuntimeMu.Unlock()
	})
}

func stubShellLookPath(t *testing.T, existing ...string) {
	t.Helper()
	shellRuntimeMu.Lock()
	oldCur, oldSet, oldErr, oldPath := shellRuntimeCur, shellRuntimeSet, shellRuntimeErr, shellLookPath
	shellRuntimeCur, shellRuntimeSet, shellRuntimeErr = nil, false, nil
	shellLookPath = lookPathStub(existing...)
	shellRuntimeMu.Unlock()
	t.Cleanup(func() {
		shellRuntimeMu.Lock()
		shellRuntimeCur, shellRuntimeSet, shellRuntimeErr, shellLookPath = oldCur, oldSet, oldErr, oldPath
		shellRuntimeMu.Unlock()
	})
}

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

func TestResolveProfilePosixChain(t *testing.T) {
	p, err := resolveProfile("", "linux", lookPathStub("bash", "sh"))
	if err != nil || p == nil || p.Name != "bash" || p.Kind != KindPosix || p.arg() != "-c" {
		t.Errorf("bash 应优先: %+v err=%v", p, err)
	}
	p, err = resolveProfile("", "linux", lookPathStub("sh"))
	if err != nil || p == nil || p.Name != "sh" {
		t.Errorf("无 bash 应落 sh: %+v err=%v", p, err)
	}
	p, err = resolveProfile("", "linux", lookPathStub("ash"))
	if err != nil || p == nil || p.Name != "ash" {
		t.Errorf("仅 ash 应落 ash: %+v err=%v", p, err)
	}
	if p, err := resolveProfile("", "linux", lookPathStub()); err == nil || p != nil {
		t.Errorf("全落空应报错: %+v err=%v", p, err)
	}
}

func TestResolveProfileWindows(t *testing.T) {
	p, err := resolveProfile("", "windows", lookPathStub("pwsh"))
	if err != nil || p == nil || p.Kind != KindPowerShell || p.arg() != "-Command" {
		t.Fatalf("pwsh: %+v err=%v", p, err)
	}
	if strings.Join(p.ExtraArgs, " ") != "-NoProfile -NonInteractive" {
		t.Errorf("ExtraArgs: %v", p.ExtraArgs)
	}
	if p, err := resolveProfile("", "windows", lookPathStub("cmd")); err == nil {
		t.Errorf("windows 不应回退 cmd: %+v", p)
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
	p, err := resolveProfile("/usr/bin/fish", "linux", lookPathStub("fish", "bash"))
	if err != nil || p == nil || p.Name != "fish" || p.Path != "/usr/bin/fish" {
		t.Errorf("override 优先: %+v err=%v", p, err)
	}
	if p, err := resolveProfile("/no/such/shell", "linux", lookPathStub("bash")); err == nil || p != nil {
		t.Errorf("override 落空应报错: %+v err=%v", p, err)
	}
}

func TestProbePrograms(t *testing.T) {
	got := probePrograms(lookPathStub("ls", "cat", "rg", "python"))
	if strings.Join(got, ", ") != "ls, cat, rg, python" {
		t.Errorf("probePrograms = %v", got)
	}
}

func TestResolveShellRuntime(t *testing.T) {
	if rt, err := resolveShellRuntime("", "linux", lookPathStub()); err == nil || rt != nil {
		t.Errorf("无 shell 应报错: %+v err=%v", rt, err)
	}
	rt, err := resolveShellRuntime("", "linux", lookPathStub("bash", "ls"))
	if err != nil || rt.profile == nil || strings.Join(rt.programs, ",") != "ls" {
		t.Errorf("有 shell 才探测: %+v err=%v", rt, err)
	}
}

func TestToolDefsHasRunShell(t *testing.T) {
	withShellRuntime(t, &shellRuntime{profile: &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix}})
	defs := ToolDefs()
	if len(defs) == 0 || defs[0].Function.Name != "run_shell" {
		t.Errorf("run_shell 应恒定注册在首位: %+v", defs)
	}
}

func TestToolDefsRunShellDesc(t *testing.T) {
	withShellRuntime(t, &shellRuntime{
		profile:  &shellProfile{Path: "/usr/bin/bash", Name: "bash", Kind: KindPosix},
		programs: []string{"ls", "grep"},
	})
	var desc string
	for _, d := range ToolDefs() {
		if d.Function.Name == "run_shell" {
			desc = d.Function.Description
		}
	}
	if !strings.Contains(desc, "在 "+runtime.GOOS+" bash 中执行") || !strings.Contains(desc, "可用程序: ls, grep") {
		t.Errorf("desc = %q", desc)
	}
	if !strings.Contains(desc, "默认在会话启动目录（进程 cwd）下执行") || !strings.Contains(desc, "cwd 参数") {
		t.Errorf("desc 应说明默认工作目录与 cwd 参数: %q", desc)
	}
	params := runShellParams()
	if !strings.Contains(params, "默认 60（interactive 时 300），最大 900") {
		t.Error("timeout 参数描述应为默认 60 interactive 时 300 最大 900")
	}
	if !strings.Contains(params, `"interactive":{"type":"boolean"`) {
		t.Error("params 缺少 interactive 参数声明")
	}
	if !strings.Contains(params, `"cwd":{"type":"string"`) {
		t.Error("params 缺少 cwd 参数声明")
	}
}

func TestInitShellUnavailable(t *testing.T) {
	stubShellLookPath(t)
	err := InitShell("")
	if err == nil || !strings.Contains(err.Error(), "未找到可用 shell") {
		t.Fatalf("无 shell 应报错: %v", err)
	}
	if err2 := InitShell("bash"); err2 == nil {
		t.Error("重复调用应返回缓存的错误")
	}
}

func TestInitShellOverrideUnavailable(t *testing.T) {
	stubShellLookPath(t, "bash")
	err := InitShell("zsh")
	if err == nil || !strings.Contains(err.Error(), "配置的 shell") {
		t.Fatalf("override 无效应报错: %v", err)
	}
}

func TestNewWithoutShell(t *testing.T) {
	isolatePromptEnv(t)
	stubShellLookPath(t)
	cfg := defaultConfig()
	cfg.GlobalSession = t.TempDir()
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "未找到可用 shell") {
		t.Fatalf("无 shell 时 New 应报错: %v", err)
	}
}
