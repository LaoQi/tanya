package repl

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/render/term"

	"github.com/LaoQi/tanya/agent"
)

func TestHistoryLineStripsControlAndSGR(t *testing.T) {
	msgs := []agent.Message{
		{Role: "tool", Name: "run_shell", Content: "stdout:\nmock:200\n\r\x1b[K\x1b[33m⠋ 等待响应 0s\x1b[0m\r\x1b[Kpong\n\x1b[94m  ↳ 0ms\n\x1b[0m"},
		{Role: "tool", Name: "run_shell", Content: "stdout:\n--- A ---\n\r\x1b[K\x1b[33m" + strings.Repeat("长", 300)},
	}
	for i, m := range msgs {
		line := historyLine(i+1, m, 80)
		if strings.ContainsRune(line, 0x1b) || strings.ContainsRune(line, '\r') || strings.ContainsRune(line, 0x07) {
			t.Errorf("摘要行残留转义/控制字符: %q", line)
		}
		if w := term.Width(line); w > 79 {
			t.Errorf("摘要行未按终端列宽截断: %d", w)
		}
	}
}

func TestHistoryLineKeepsPlainText(t *testing.T) {
	m := agent.Message{Role: "assistant", Content: "第一行\n第二行"}
	if got, want := historyLine(2, m, 0), "  2 assistant 第一行 第二行"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestHistoryLineColumnWidthCJK 锁住截断宽度按显示列算：中文 100 字在 40 列终端上
// 必须截到一行内（按 rune 算会放过 200 列）。
func TestHistoryLineColumnWidthCJK(t *testing.T) {
	m := agent.Message{Role: "assistant", Content: strings.Repeat("字", 100)}
	line := historyLine(1, m, 40)
	if w := term.Width(line); w > 39 {
		t.Errorf("摘要行超宽 %d 列: %q", w, line)
	}
	if !strings.Contains(line, "~") {
		t.Errorf("截断应带标记: %q", line)
	}
}

// TestShowHistoryRespectsTerminalWidth 是接线用例：/history 的列宽必须取自 Console.Size()
// （fakeTerm 为 80×24），中文摘要按显示列截断到一行——而不是旧的 120 rune。
func TestShowHistoryRespectsTerminalWidth(t *testing.T) {
	dir := t.TempDir()
	seed := `{"role":"user","content":"` + strings.Repeat("长", 200) + `"}` + "\n"
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", seed)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	r.showHistory(nil)
	if !strings.Contains(out.String(), "~") {
		t.Fatalf("超长摘要应被截断，输出里应有截断标记: %q", out.String())
	}
	for _, l := range strings.Split(out.String(), "\n") {
		if w := term.Width(l); w > 79 {
			t.Errorf("/history 行超宽 %d 列（终端 80）: %q", w, l)
		}
	}
}

// TestTermColsUnavailable 锁住「宽度不可知 → 0」：没有 Console 时不猜宽度，调用方保持原样输出。
func TestTermColsUnavailable(t *testing.T) {
	r, _, _ := newTestREPLAgentProf(t, nil, nil, nonTTYProf)
	if got := r.termCols(); got != 0 {
		t.Errorf("无 Console 时 termCols 应为 0，实际 %d", got)
	}
}

// TestShowHistoryNegativeFullRender：/history -n 走完整渲染（消息头 + 正文），
// 而不是一行摘要；序号仍是绝对序号，头行标注只显示了尾部。
func TestShowHistoryNegativeFullRender(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", historySampleSession(5))
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	r.showHistory([]string{"-2"})
	got := out.String()
	if !strings.Contains(got, "共 5 条消息（仅显示最近 2 条）") {
		t.Errorf("头行应标注只显示最近 2 条: %q", got)
	}
	for _, want := range []string{"#4 assistant", "回复 4", "#5 user", "提问 5"} {
		if !strings.Contains(got, want) {
			t.Errorf("应完整渲染最近 2 条（缺 %q）: %q", want, got)
		}
	}
	if i, j := strings.Index(got, "#4 assistant"), strings.Index(got, "#5 user"); i < 0 || j < 0 || i > j {
		t.Errorf("条目应按时间顺序输出: %q", got)
	}
	if strings.Contains(got, "#1 ") || strings.Contains(got, "提问 1") {
		t.Errorf("不应出现第 1 条: %q", got)
	}
	if strings.Contains(got, "  4 assistant 回复 4") {
		t.Errorf("不应再输出一行摘要形态: %q", got)
	}
}

// TestShowHistoryNegativeRendersToolMessage：-n 下的工具消息同样完整渲染——正文与
// 「→ 工具 参数」行都要出现，而不是被压成一行摘要。
func TestShowHistoryNegativeRendersToolMessage(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	seed := `{"role":"user","content":"提问"}` + "\n" +
		`{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"run_shell","arguments":"{\"command\":\"ls\"}"}}]}` + "\n" +
		`{"role":"tool","name":"run_shell","content":"stdout:\nmock-out"}` + "\n"
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", seed)
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	r.showHistory([]string{"-2"})
	got := out.String()
	for _, want := range []string{"#2 assistant", "[调用 run_shell]", "#3 run_shell", "mock-out", "→ run_shell", "command"} {
		if !strings.Contains(got, want) {
			t.Errorf("工具消息应完整渲染（缺 %q）: %q", want, got)
		}
	}
}

// TestShowHistoryNegativeBeyondTotal：-n 超过总条数按全部处理，不报错。
func TestShowHistoryNegativeBeyondTotal(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", historySampleSession(3))
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	r.showHistory([]string{"-99"})
	got := out.String()
	if !strings.Contains(got, "共 3 条消息\n") {
		t.Errorf("越界不应报错且头行不带尾标注: %q", got)
	}
	for _, want := range []string{"#1 user", "提问 1", "#2 assistant", "回复 2", "#3 user", "提问 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("应完整渲染全部 3 条（缺 %q）: %q", want, got)
		}
	}
	if errb.String() != "" {
		t.Errorf("不应有错误行: %q", errb.String())
	}
}

// TestShowHistoryZeroPrintsLast：/history 0 直接拉取最后一条全文。
func TestShowHistoryZeroPrintsLast(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", historySampleSession(4))
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	r.showHistory([]string{"0"})
	got := out.String()
	if !strings.Contains(got, "#4 assistant") {
		t.Errorf("应输出第 4 条（最后一条）的消息头: %q", got)
	}
	if !strings.Contains(got, "回复 4") {
		t.Errorf("应输出最后一条正文: %q", got)
	}
	if errb.String() != "" {
		t.Errorf("不应有错误行: %q", errb.String())
	}
}

func TestShowHistoryRejectsBadIndex(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", historySampleSession(3))
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"abc", "4", "1.5"} {
		errb.Reset()
		out.Reset()
		r.showHistory([]string{arg})
		if !strings.Contains(errb.String(), "序号无效") {
			t.Errorf("参数 %q 应报序号无效: %q", arg, errb.String())
		}
	}
}

// historySampleSession 造 n 条消息的会话：user 1 / assistant 1 / … 交替，正文含序号便于断言。
func historySampleSession(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		role, text := "assistant", "回复 "+strconv.Itoa(i)
		if i%2 == 1 {
			role, text = "user", "提问 "+strconv.Itoa(i)
		}
		fmt.Fprintf(&b, "{\"role\":%q,\"content\":%q}\n", role, text)
	}
	return b.String()
}

// TestShowHistoryNegativeOne：-1 只渲染最后一条，但形态与 0 同为全文（多的只是头行标注）。
func TestShowHistoryNegativeOne(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	seedIntoSessionDir(t, dir, "20260101-100000.jsonl", historySampleSession(4))
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	if err := a.LoadSession("20260101-100000"); err != nil {
		t.Fatal(err)
	}
	r.showHistory([]string{"-1"})
	got := out.String()
	for _, want := range []string{"共 4 条消息（仅显示最近 1 条）", "#4 assistant", "回复 4"} {
		if !strings.Contains(got, want) {
			t.Errorf("-1 应输出最后一条全文（缺 %q）: %q", want, got)
		}
	}
	if strings.Contains(got, "#1 ") || strings.Contains(got, "提问 1") {
		t.Errorf("不应出现第 1 条: %q", got)
	}
}
