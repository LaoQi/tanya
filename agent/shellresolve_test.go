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
	cfg.GlobalSession = t.TempDir()
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "配置的 shell") {
		t.Fatalf("无可用 shell 时 New 应报错: %v", err)
	}
}
