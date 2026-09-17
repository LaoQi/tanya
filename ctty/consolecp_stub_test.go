//go:build !windows

package ctty

import "testing"

func TestConsoleCPStub(t *testing.T) {
	if cp, ok := ConsoleCP(); ok || cp != 0 {
		t.Errorf("非 windows 平台 ConsoleCP 应为 (0,false): %d,%v", cp, ok)
	}
	if cp, ok := ConsoleOutputCP(); ok || cp != 0 {
		t.Errorf("非 windows 平台 ConsoleOutputCP 应为 (0,false): %d,%v", cp, ok)
	}
	if SetConsoleCP(65001) || SetConsoleOutputCP(65001) {
		t.Errorf("非 windows 平台 SetConsoleCP/SetConsoleOutputCP 应返回 false")
	}
	if OEMCP() != 0 {
		t.Errorf("非 windows 平台 OEMCP 应为 0")
	}
	EnsureUTF8()
	RestoreUTF8()
	if cp := FallbackCP(); cp != 0 {
		t.Errorf("非 windows 平台 FallbackCP 应为 0: %d", cp)
	}
}

func TestDecodeCPIdentity(t *testing.T) {
	in := []byte{0xC4, 0xE3, 0xBA, 0xC3}
	got := DecodeCP(936, in)
	if string(got) != string(in) {
		t.Errorf("非 windows 平台 DecodeCP 应恒等: in=%q got=%q", in, got)
	}
	if got := DecodeCP(0, nil); got != nil {
		t.Errorf("空输入应原样返回: %v", got)
	}
}
