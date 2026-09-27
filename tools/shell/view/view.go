// Package view 是 run_shell 自带的表现层视图：参数区（cwd/timeout/命令）与结果区（输出/状态）。
package view

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LaoQi/tanya/render/present"
	"github.com/LaoQi/tanya/render/term"
	"github.com/LaoQi/tanya/tools/shell"
)

const (
	headLines = 3
	tailLines = 2

	commandPrefix = "  $ "
	cwdPrefix     = "  cwd: "
	timeoutPrefix = "  timeout: "

	msgCmdOmittedFmt = "… 省略 %d 行（完整命令见 /history）"
	msgTimeoutSecFmt = "%ds"
	msgTruncNoteFmt  = "…中间省略 %d 字节…"
	msgLinesTotalFmt = "共 %d 行"
	msgLinesFmt      = "%d 行"

	msgInterrupt  = "已中断"
	msgNotStarted = "未执行"
	msgTimeout    = "执行超时"
	msgToolErrFmt = "错误: %s"
)

type tagLine struct {
	text   string
	stderr bool
}

// View 返回 run_shell 的自带视图：按工具名注册进 present.Registry 后由 repl 消费。
func View() present.ToolView {
	return present.ToolView{Args: argsView, Result: resultView}
}

// argsView 渲染 run_shell 参数：cwd 行（显式指定时）、timeout 行（显式指定时）与折行的命令区。
// 内联只在「无 cwd、无 timeout、命令单行且非空」时成立，其余情形转块形态——附加参数是块形态的判据。
func argsView(args string, width int) (present.ArgsView, bool) {
	var a struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return present.PlainArgsView(present.TrimBlankEdges(args), commandPrefix, msgCmdOmittedFmt, width), true
	}
	cwd := strings.TrimSpace(a.Cwd)
	cmd := present.ExpandTabs(present.TrimBlankEdges(a.Command))
	var opts []string
	if cwd != "" {
		opts = append(opts, term.Truncate(cwdPrefix+cwd, width-2))
	}
	if a.Timeout > 0 {
		opts = append(opts, term.Truncate(timeoutPrefix+fmt.Sprintf(msgTimeoutSecFmt, a.Timeout), width-2))
	}
	v := present.ArgsView{Body: append(opts, present.Prefixed(commandLines(cmd, width-len(commandPrefix)), commandPrefix)...)}
	if cwd == "" && a.Timeout <= 0 && cmd != "" && !strings.Contains(cmd, "\n") {
		v.Inline = cmd
	}
	return v, true
}

// commandLines 把命令折成显示行（制表符已摊平、保留原换行结构）；超过上限时保留头尾，
// 中段换成省略提示——命令是有序脚本，省略中段比省略尾部更不易误读收尾的 done/EOF。
func commandLines(cmd string, width int) []string {
	if cmd == "" {
		return nil
	}
	return present.CapLines(term.Wrap(cmd, width), width, msgCmdOmittedFmt)
}

// resultView 渲染 run_shell 结果：合并 stdout/stderr（stderr 行带 2| 前缀）、按总行数截断，
// 状态行 = 退出/中断/超时状态 · 耗时 · 行数。
func resultView(_ string, meta any, width, maxLines int) (present.View, bool) {
	r, ok := meta.(*shell.Result)
	if !ok || r == nil {
		return present.View{}, false
	}
	lines, status, total, trunc := shellView(r, width, maxLines)
	parts := []string{status, present.Duration(r.Duration)}
	switch {
	case trunc:
		parts = append(parts, fmt.Sprintf(msgLinesTotalFmt, total))
	case total > 0:
		parts = append(parts, fmt.Sprintf(msgLinesFmt, total))
	}
	return present.View{Body: present.Indent(lines), Status: strings.Join(parts, " · ")}, true
}

func shellView(r *shell.Result, width, maxLines int) ([]string, string, int, bool) {
	stdoutLines := chunkLines(r.Stdout)
	stderrLines := chunkLines(r.Stderr)
	total := len(stdoutLines) + len(stderrLines)
	tagged := make([]tagLine, 0, total)
	for _, l := range stdoutLines {
		tagged = append(tagged, tagLine{l, false})
	}
	for _, l := range stderrLines {
		tagged = append(tagged, tagLine{l, true})
	}
	view := tagged
	trunc := false
	if total > maxLines {
		if headLines+tailLines >= total {
			view = tagged
		} else {
			view = append(append([]tagLine{}, tagged[:headLines]...), tagged[total-tailLines:]...)
			trunc = true
		}
	}
	lines := make([]string, 0, len(view))
	for _, t := range view {
		s := t.text
		if t.stderr {
			s = "2| " + s
		}
		lines = append(lines, term.Truncate(s, width-2))
	}
	return lines, shellStatus(r), total, trunc
}

func chunkLines(chunks []shell.Chunk) []string {
	var out []string
	for _, c := range chunks {
		if c.Truncated > 0 {
			out = append(out, fmt.Sprintf(msgTruncNoteFmt, c.Truncated))
		}
		if c.Data == "" {
			continue
		}
		out = append(out, strings.Split(strings.TrimRight(c.Data, "\n"), "\n")...)
	}
	return out
}

func shellStatus(r *shell.Result) string {
	switch {
	case r.Interrupted && r.NotStarted:
		return msgNotStarted
	case r.Interrupted:
		return msgInterrupt
	case r.TimedOut:
		return msgTimeout
	case r.Err != "":
		return fmt.Sprintf(msgToolErrFmt, r.Err)
	}
	return fmt.Sprintf("exit %d", r.ExitCode)
}
