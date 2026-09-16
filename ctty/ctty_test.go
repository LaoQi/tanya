package ctty

import "testing"

func TestProbeConsistent(t *testing.T) {
	f := Probe()
	if !f.SizeOK && (f.Cols != 0 || f.Rows != 0) {
		t.Errorf("SizeOK=false 时尺寸应为零值: %+v", f)
	}
	if f.SizeOK && (f.Cols <= 0 || f.Rows <= 0) {
		t.Errorf("SizeOK=true 时尺寸应为正: %+v", f)
	}
}

func TestProbeInvalidFD(t *testing.T) {
	if IsTerminal(-1) {
		t.Error("非法 fd 不应判定为终端")
	}
	if _, _, ok := Size(-1); ok {
		t.Error("非法 fd 不应给出尺寸")
	}
}
