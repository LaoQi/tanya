package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/render/present"
	"github.com/LaoQi/tanya/tools/shell"
)

func TestShellArgsViewKeepsMultiline(t *testing.T) {
	args := `{"command":"cat > a <<'EOF'\n  line one \n\nline two\nEOF"}`
	v, _ := argsView(args, 80)
	if v.Inline != "" {
		t.Errorf("多行命令不应内联: %q", v.Inline)
	}
	var body []string
	for _, l := range v.Body {
		body = append(body, strings.TrimPrefix(l, commandPrefix))
	}
	if got, want := strings.Join(body, "\n"), "cat > a <<'EOF'\n  line one \n\nline two\nEOF"; got != want {
		t.Errorf("多行命令应保留换行与缩进（只去首尾空行）: got %q, want %q", got, want)
	}
}

func TestShellArgsViewCwd(t *testing.T) {
	cases := []struct {
		label, args string
		wantCwd     string
		wantCmd     string
	}{
		{"未指定 cwd", `{"command":"ls -la"}`, "", "ls -la"},
		{"显式 cwd 原样", `{"command":"ls","cwd":"/tmp/abc"}`, "/tmp/abc", "ls"},
		{"cwd 相对路径", `{"command":"ls","cwd":"sub"}`, "sub", "ls"},
		{"cwd 带空白", `{"command":"ls","cwd":" /tmp/a "}`, "/tmp/a", "ls"},
		{"cwd 空串", `{"command":"ls","cwd":""}`, "", "ls"},
	}
	for _, c := range cases {
		v, _ := argsView(c.args, 80)
		body := strings.Join(v.Body, "\n")
		if c.wantCwd == "" {
			if strings.Contains(body, "cwd") || strings.Contains(body, "timeout") {
				t.Errorf("%s: 不应出现 cwd/timeout 行: %q", c.label, body)
			}
			if want := "\n▸ run_shell " + c.wantCmd + "\n"; v.Inline != c.wantCmd {
				t.Errorf("%s: 应内联命令: got %q, want %q", c.label, v.Inline, want)
			}
			continue
		}
		want := cwdPrefix + c.wantCwd + "\n" + commandPrefix + c.wantCmd
		if body != want || v.Inline != "" {
			t.Errorf("%s: got (%q, %q), want (%q, 内联为空)", c.label, body, v.Inline, want)
		}
	}
	bad, _ := argsView(`{bad`, 80)
	if bad.Inline != `{bad` || len(bad.Body) != 1 || bad.Body[0] != commandPrefix+`{bad` {
		t.Errorf("坏 JSON 应原样展示: %+v", bad)
	}
	if empty, _ := argsView(`{}`, 80); empty.Inline != "" || len(empty.Body) != 0 {
		t.Errorf("空参数不应产生任何行: %+v", empty)
	}
}

func TestRenderToolStartCommandOmitted(t *testing.T) {
	var lines []string
	for i := 1; i <= 12; i++ {
		lines = append(lines, fmt.Sprintf("step-%02d", i))
	}
	args, _ := json.Marshal(map[string]string{"command": strings.Join(lines, "\n")})
	v, ok := argsView(string(args), 80)
	if !ok {
		t.Fatal("自带视图应处理 run_shell 参数")
	}
	body := v.Body
	if len(body) != present.MaxLines {
		t.Fatalf("命令行数应为上限 %d，实际 %d: %q", present.MaxLines, len(body), body)
	}
	wantOmitted := fmt.Sprintf(msgCmdOmittedFmt, 12-present.HeadLines-present.TailLines)
	if body[present.HeadLines] != commandPrefix+wantOmitted {
		t.Errorf("省略行 = %q, want %q", body[present.HeadLines], commandPrefix+wantOmitted)
	}
	if body[0] != commandPrefix+"step-01" || body[len(body)-1] != commandPrefix+"step-12" {
		t.Errorf("应保留头尾: %q", body)
	}
}

func TestResultViewStatusAndBody(t *testing.T) {
	r := &shell.Result{
		Stdout:   []shell.Chunk{{Data: "ok\n"}},
		Stderr:   []shell.Chunk{{Data: "boom\n"}},
		Duration: 1500 * time.Millisecond,
		ExitCode: 0,
	}
	v, ok := resultView("", r, 80, 20)
	if !ok {
		t.Fatal("shell.Result 应由自带视图处理")
	}
	if want := "exit 0 · 1.5s · 2 行"; v.Status != want {
		t.Errorf("状态行 = %q, want %q", v.Status, want)
	}
	if want := "  ok\n  2| boom\n"; v.Body != want {
		t.Errorf("正文 = %q, want %q", v.Body, want)
	}
}

func TestResultViewTruncatesMiddle(t *testing.T) {
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("L%02d", i))
	}
	r := &shell.Result{Stdout: []shell.Chunk{{Data: strings.Join(lines, "\n") + "\n"}}}
	v, _ := resultView("", r, 80, 4)
	body := strings.Split(strings.Trim(v.Body, "\n"), "\n")
	if len(body) != headLines+tailLines {
		t.Fatalf("应保留头 %d + 尾 %d 行: %q", headLines, tailLines, body)
	}
	if body[0] != "  L01" || body[len(body)-1] != "  L10" {
		t.Errorf("头尾不符: %q", body)
	}
	if want := "共 10 行"; !strings.Contains(v.Status, want) {
		t.Errorf("截断时状态行应报总行数 %q: %q", want, v.Status)
	}
}

func TestResultViewTruncationMarkerAndStatus(t *testing.T) {
	r := &shell.Result{
		Stdout: []shell.Chunk{{Truncated: 2048}, {Data: "x\n"}},
		Err:    "cwd 不存在",
	}
	v, _ := resultView("", r, 80, 20)
	if !strings.Contains(v.Body, "…中间省略 2048 字节…") {
		t.Errorf("捕获截断应带标记: %q", v.Body)
	}
	if !strings.Contains(v.Status, "错误: cwd 不存在") {
		t.Errorf("错误状态不符: %q", v.Status)
	}
}

func TestResultViewRejectsForeignMeta(t *testing.T) {
	for _, meta := range []any{nil, "text", &struct{}{}} {
		if _, ok := resultView("", meta, 80, 20); ok {
			t.Errorf("非 *shell.Result 不应被认领: %#v", meta)
		}
	}
}
