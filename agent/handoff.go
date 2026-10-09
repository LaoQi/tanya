package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const handoffSummaryLimit = 128 * 1024

const msgHandoffDesc = "结束当前会话并把关键进度交接给新会话。" +
	"上下文接近上限、或某一阶段目标已完成需要转段时使用：" +
	"summary 写清当前进度、必要上下文与下一阶段目标（新会话从这段摘要起步，旧会话历史不会带入）；" +
	"continue 省略即 true（交接后自动继续执行），设为 false 则停在新会话等待用户输入。" +
	"提交后本回合正常结束时交接生效，旧会话不再追加内容。"

const msgHandoffParams = `{"type":"object","properties":{` +
	`"summary":{"type":"string","description":"交接摘要：当前进度、关键上下文、下一阶段目标。新会话以此起步。"},` +
	`"continue":{"type":"boolean","description":"交接后是否自动继续执行（省略即 true）；false 表示停在新会话等待用户输入"}},` +
	`"required":["summary"]}`

type Handoff struct {
	Summary  string
	OldID    string
	NewID    string
	OldMsgs  int
	OldStats Stats
	OldFile  string
	Continue bool
	NoSave   bool
}

type pendingHandoff struct {
	summary string
	cont    bool
}

type handoffTarget interface {
	RequestHandoff(summary string, cont bool) error
}

func (a *Agent) RequestHandoff(summary string, cont bool) error {
	if a.handoffPend != nil {
		return fmt.Errorf("%s", MsgHandoffDup)
	}
	summary = normalizeHandoffSummary(summary)
	if summary == "" {
		return fmt.Errorf("%s", MsgHandoffEmpty)
	}
	if n := len([]rune(summary)); n > handoffSummaryLimit {
		return fmt.Errorf(MsgHandoffTooLongFmt, n, handoffSummaryLimit)
	}
	a.handoffPend = &pendingHandoff{summary: summary, cont: cont}
	return nil
}

func (a *Agent) TakeHandoff() (Handoff, bool) {
	if a.lastHandoff == nil {
		return Handoff{}, false
	}
	h := *a.lastHandoff
	a.lastHandoff = nil
	return h, true
}

func (a *Agent) applyHandoff() {
	p := a.handoffPend
	a.handoffPend = nil
	if p == nil || a.store == nil {
		return
	}
	newID := a.store.nextID()
	h := Handoff{
		Summary:  p.summary,
		OldID:    a.store.id(),
		NewID:    newID,
		OldMsgs:  len(a.history),
		OldStats: a.Stats(),
		OldFile:  a.SessionFile(),
		Continue: p.cont,
		NoSave:   a.store.disabled,
	}
	a.history = append(a.history, Message{Role: "user", Content: fmt.Sprintf(MsgHandoffNoticeFmt, newID)})
	_ = a.save()
	a.store.rotateTo(newID)
	a.history = []Message{{Role: "user", Content: fmt.Sprintf(MsgHandoffIntroFmt, h.OldID, p.summary)}}
	a.prompt.reset()
	a.stats.reset()
	_ = a.save()
	a.lastHandoff = &h
}

func normalizeHandoffSummary(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(s)
}

func handoffLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

type nextTool struct{ target handoffTarget }

func newNextTool(t handoffTarget) *nextTool { return &nextTool{target: t} }

func (t *nextTool) Name() string { return "next_session" }

func (t *nextTool) Definition() ToolDef {
	return NewToolDef(t.Name(), msgHandoffDesc, msgHandoffParams)
}

func (t *nextTool) Invoke(_ context.Context, argsJSON string) ToolResult {
	var args struct {
		Summary  string `json:"summary"`
		Continue *bool  `json:"continue"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ToolResult{Text: fmt.Sprintf(MsgParseArgs, err)}
	}
	cont := true
	if args.Continue != nil {
		cont = *args.Continue
	}
	if err := t.target.RequestHandoff(args.Summary, cont); err != nil {
		return ToolResult{Text: MsgErrPrefix + err.Error()}
	}
	return ToolResult{Text: fmt.Sprintf(MsgHandoffToolDoneFmt, handoffLines(normalizeHandoffSummary(args.Summary)))}
}
