package repl

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/style"
)

func TestStreamsInjectWriter(t *testing.T) {
	var out, errb bytes.Buffer
	st := NewStreams(&out, &errb)
	st.out.emit(KindContent, "答案\n")
	st.err.emit(KindError, "错误\n")
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
	st.out.emit(KindContent, "")
	if out.Len() != 0 {
		t.Errorf("空串不应写入: %q", out.String())
	}
}

func TestOutputAtomicSingleWrite(t *testing.T) {
	var out bytes.Buffer
	st := NewStreams(&out, &bytes.Buffer{})
	st.out.atomic(KindToolBlock, func(w io.Writer) {
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
	st.out.emit(KindContent, "迁移\n")
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
	st.out.emit(KindContent, "a")
	st.out.atomic(KindToolBlock, func(w io.Writer) { io.WriteString(w, "b") })
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

func TestOutputAtomicNoInterleave(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: true, Colors: style.Level16, Unicode: true})
	var buf syncBuf
	st := NewStreams(&buf, &syncBuf{})
	sink := WireToolView(st, func() int { return 80 }, 20)
	sink(agent.Event{Kind: agent.EventToolStart, ToolName: "run_shell", ToolArgs: `{"command":"sleep 1"}`})

	pairs := 0
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		st.out.atomic(KindToolBlock, func(w io.Writer) {
			io.WriteString(w, "<<A>>")
			io.WriteString(w, "<<B>>")
		})
		pairs++
	}
	sink(agent.Event{Kind: agent.EventToolEnd, ToolName: "run_shell", ToolArgs: `{"command":"sleep 1"}`,
		Result: agent.ToolResult{Shell: &agent.ShellResult{Command: "sleep 1", ExitCode: 0}}})

	got := buf.String()
	if pairs == 0 {
		t.Fatal("未产生写入")
	}
	if !strings.Contains(got, style.ClearLineHome()) {
		t.Error("并发场景下未出现 spinner 帧，断言无意义")
	}
	if n := strings.Count(got, "<<A>><<B>>"); n != pairs {
		t.Errorf("原子块被 spinner 帧插入: %d/%d", n, pairs)
	}
}

func TestNoticeDecorErrorKinds(t *testing.T) {
	ttyProfile(t, style.Profile{TTY: true, Colors: style.Level16, Unicode: true})
	a := newSessTestAgent(t, t.TempDir())
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())

	r.st.out.vis = 1 << KindNotice
	r.handleCommand("/help")
	if !strings.Contains(out.String(), "斜杠命令") {
		t.Errorf("/help 应归 KindNotice: %q", out.String())
	}
	out.Reset()
	r.handleCommand("/help")
	r.st.out.vis = 1 << KindDecor
	out.Reset()
	r.st.out.emit(KindDecor, turnSep(0))
	if !strings.Contains(out.String(), "─") {
		t.Errorf("回合分隔线应归 KindDecor: %q", out.String())
	}
	out.Reset()
	r.handleCommand("/help")
	if out.String() != "" {
		t.Errorf("仅放行 Decor 时 /help 不应输出: %q", out.String())
	}

	r.st.err.vis = 1 << KindNotice
	r.handleCommand("/think bogus")
	if errb.String() != "" {
		t.Errorf("仅放行 Notice 的错误流不应输出错误: %q", errb.String())
	}
	r.st.err.vis = 1 << KindError
	r.handleCommand("/think bogus")
	if !strings.Contains(errb.String(), "无效思考等级") {
		t.Errorf("命令失败应归 KindError 并走 stderr: %q", errb.String())
	}
}

func TestStderrRoutedThroughErr(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/think bogus")
	if errb.String() == "" {
		t.Fatal("失败命令应写入 stderr 出口")
	}
	if out.String() != "" {
		t.Errorf("失败命令不应写 stdout: %q", out.String())
	}

	var err2 syncBuf
	st := NewStreams(&syncBuf{}, &err2)
	st.Fail("失败: %v", errors.New("boom"))
	if !strings.Contains(err2.String(), "失败: boom") {
		t.Errorf("Fail 应写 stderr: %q", err2.String())
	}

	var ob syncBuf
	st2 := NewStreams(&ob, &syncBuf{})
	st2.Print("提示\n")
	st2.Content("正文\n")
	if ob.String() != "提示\n正文\n" {
		t.Errorf("Print/Content 应写 stdout: %q", ob.String())
	}
}
