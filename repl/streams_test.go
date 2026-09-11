package repl

import (
	"bytes"
	"io"
	"testing"
)

func TestStreamsInjectWriter(t *testing.T) {
	var out, errb bytes.Buffer
	st := NewStreams(&out, &errb)
	st.out.emit("答案\n")
	st.err.emit("错误\n")
	if got := out.String(); got != "答案\n" {
		t.Errorf("stdout 出口: %q", got)
	}
	if got := errb.String(); got != "错误\n" {
		t.Errorf("stderr 出口: %q", got)
	}
}

func TestOutputEmitEmptyIsNoop(t *testing.T) {
	var out bytes.Buffer
	st := NewStreams(&out, &bytes.Buffer{})
	st.out.emit("")
	if out.Len() != 0 {
		t.Errorf("空串不应写入: %q", out.String())
	}
}

func TestOutputAtomicSingleWrite(t *testing.T) {
	var out bytes.Buffer
	st := NewStreams(&out, &bytes.Buffer{})
	st.out.atomic(func(w io.Writer) {
		io.WriteString(w, "标题\n")
		io.WriteString(w, "正文\n")
	})
	if got := out.String(); got != "标题\n正文\n" {
		t.Errorf("atomic 输出: %q", got)
	}
}

func TestOutputWriteIsWriter(t *testing.T) {
	var out bytes.Buffer
	st := NewStreams(&out, &bytes.Buffer{})
	if _, err := st.out.Write([]byte("裸写\n")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "裸写\n" {
		t.Errorf("Write 输出: %q", got)
	}
}

func TestOutputSetWriter(t *testing.T) {
	var first, second bytes.Buffer
	st := NewStreams(&first, &bytes.Buffer{})
	st.out.setWriter(&second)
	st.out.emit("迁移\n")
	if first.Len() != 0 {
		t.Errorf("替换后不应写入旧 writer: %q", first.String())
	}
	if got := second.String(); got != "迁移\n" {
		t.Errorf("替换后写入新 writer: %q", got)
	}
}

func TestOutputGuardFiresOnEmitNotWrite(t *testing.T) {
	var calls int
	st := NewStreams(&bytes.Buffer{}, &bytes.Buffer{})
	st.out.guard = func() { calls++ }
	st.out.emit("a")
	st.out.atomic(func(w io.Writer) { io.WriteString(w, "b") })
	if calls != 2 {
		t.Errorf("emit/atomic 各应触发一次 guard，实际 %d", calls)
	}
	if _, err := st.out.Write([]byte("c")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("裸 Write 不应触发 guard，实际 %d", calls)
	}
}
