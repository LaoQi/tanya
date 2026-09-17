//go:build windows

package ctty

import (
	"os"
	"testing"
)

func TestSnapshotInputNonTerminalWindows(t *testing.T) {
	f, err := os.CreateTemp("", "ctty-modes-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, ok := SnapshotInput(int(f.Fd())); ok {
		t.Error("非终端 fd 应返回 false")
	}
}

func TestSnapshotRestoreInputRoundTripWindows(t *testing.T) {
	tty, err := Open()
	if err != nil {
		t.Skip("无控制终端")
	}
	defer tty.Close()
	before, ok := SnapshotInput(int(tty.Fd()))
	if !ok {
		t.Skip("CONIN$ 模式不可读")
	}
	if !RestoreInput(int(tty.Fd()), before) {
		t.Fatal("RestoreInput 失败")
	}
	after, ok := SnapshotInput(int(tty.Fd()))
	if !ok || after != before {
		t.Errorf("往返不一致: before=%#x after=%#x", before.Mode, after.Mode)
	}
}
