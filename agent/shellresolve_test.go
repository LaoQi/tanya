package agent

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func withShellRuntime(t *testing.T, rt *shellRuntime) {
	t.Helper()
	shellRuntimeMu.Lock()
	oldCur, oldSet := shellRuntimeCur, shellRuntimeSet
	shellRuntimeCur = rt
	shellRuntimeSet = true
	shellRuntimeMu.Unlock()
	t.Cleanup(func() {
		shellRuntimeMu.Lock()
		shellRuntimeCur, shellRuntimeSet = oldCur, oldSet
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
	p := resolveProfile("", "linux", lookPathStub("bash", "sh"))
	if p == nil || p.Name != "bash" || p.Kind != KindPosix || p.arg() != "-c" {
		t.Errorf("bash 应优先: %+v", p)
	}
	p = resolveProfile("", "linux", lookPathStub("sh"))
	if p == nil || p.Name != "sh" {
		t.Errorf("无 bash 应落 sh: %+v", p)
	}
	p = resolveProfile("", "linux", lookPathStub("ash"))
	if p == nil || p.Name != "ash" {
		t.Errorf("仅 ash 应落 ash: %+v", p)
	}
	if p := resolveProfile("", "linux", lookPathStub()); p != nil {
		t.Errorf("全落空应为 nil: %+v", p)
	}
}

func TestResolveProfileWindows(t *testing.T) {
	p := resolveProfile("", "windows", lookPathStub("pwsh"))
	if p == nil || p.Kind != KindPowerShell || p.arg() != "-Command" {
		t.Fatalf("pwsh: %+v", p)
	}
	if strings.Join(p.ExtraArgs, " ") != "-NoProfile -NonInteractive" {
		t.Errorf("ExtraArgs: %v", p.ExtraArgs)
	}
	if p := resolveProfile("", "windows", lookPathStub("cmd")); p != nil {
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
	p := resolveProfile("/usr/bin/fish", "linux", lookPathStub("fish", "bash"))
	if p == nil || p.Name != "fish" || p.Path != "/usr/bin/fish" {
		t.Errorf("override 优先: %+v", p)
	}
	if p := resolveProfile("/no/such/shell", "linux", lookPathStub("bash")); p != nil {
		t.Errorf("override 落空应降级 nil: %+v", p)
	}
}

func TestProbePrograms(t *testing.T) {
	got := probePrograms(lookPathStub("ls", "cat", "rg", "python"))
	if strings.Join(got, ", ") != "ls, cat, rg, python" {
		t.Errorf("probePrograms = %v", got)
	}
}

func TestShellRuntimeProgramsOnlyWithShell(t *testing.T) {
	rt := resolveShellRuntime("", "linux", lookPathStub())
	if rt.profile != nil || len(rt.programs) != 0 {
		t.Errorf("无 shell 时不应探测程序: %+v", rt)
	}
	rt = resolveShellRuntime("", "linux", lookPathStub("bash", "ls"))
	if rt.profile == nil || strings.Join(rt.programs, ",") != "ls" {
		t.Errorf("有 shell 才探测: %+v", rt)
	}
}

func TestToolDefsWithoutShell(t *testing.T) {
	withShellRuntime(t, &shellRuntime{})
	for _, d := range ToolDefs() {
		if d.Function.Name == "run_shell" {
			t.Error("无 shell 不应注册 run_shell")
		}
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
	params := runShellParams()
	if !strings.Contains(params, "默认 60（interactive 时 300），最大 900") {
		t.Error("timeout 参数描述应为默认 60 interactive 时 300 最大 900")
	}
	if !strings.Contains(params, `"interactive":{"type":"boolean"`) {
		t.Error("params 缺少 interactive 参数声明")
	}
}

func TestRunShellUnavailable(t *testing.T) {
	withShellRuntime(t, &shellRuntime{})
	got := RunShell(context.Background(), "echo hi", 10)
	if !strings.Contains(got, "run_shell 不可用") {
		t.Errorf("got %q", got)
	}
}
