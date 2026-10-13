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
	"continue 省略即 true（交接后自动继续执行），设为 false 则停在新会话等待用户输入；" +
	"workspace 把新会话开到另一个项目目录（省略即留在当前工作区，跨项目协作时用），" +
	"目录不可用时整次交接作废、本会话照旧继续。" +
	"提交后本回合正常结束时交接生效，旧会话不再追加内容。"

const msgHandoffParams = `{"type":"object","properties":{` +
	`"summary":{"type":"string","description":"交接摘要：当前进度、关键上下文、下一阶段目标。新会话以此起步。"},` +
	`"continue":{"type":"boolean","description":"交接后是否自动继续执行（省略即 true）；false 表示停在新会话等待用户输入"},` +
	`"workspace":{"type":"string","description":"新会话的工作区目录（绝对路径、~ 或相对当前工作区）；省略即留在当前工作区。目录不可用时整次交接作废"}},` +
	`"required":["summary"]}`

type Handoff struct {
	Summary      string
	OldID        string
	NewID        string
	OldMsgs      int
	OldStats     Stats
	OldFile      string
	OldWorkspace string
	Workspace    string
	Continue     bool
	NoSave       bool
	Failed       bool
	Err          string
}

type pendingHandoff struct {
	summary   string
	cont      bool
	workspace string
}

type handoffTarget interface {
	RequestHandoff(summary string, cont bool, workspace string) (string, error)
}

func (a *Agent) RequestHandoff(summary string, cont bool, workspace string) (string, error) {
	if a.handoffPend != nil {
		return "", fmt.Errorf("%s", MsgHandoffDup)
	}
	summary = normalizeHandoffSummary(summary)
	if summary == "" {
		return "", fmt.Errorf("%s", MsgHandoffEmpty)
	}
	if n := len([]rune(summary)); n > handoffSummaryLimit {
		return "", fmt.Errorf(MsgHandoffTooLongFmt, n, handoffSummaryLimit)
	}
	target := ""
	if ws := strings.TrimSpace(workspace); ws != "" {
		dir, err := a.resolveWorkspace(ws)
		if err != nil {
			return "", fmt.Errorf(MsgHandoffBadWorkspaceFmt, ws)
		}
		if dir != a.workspace {
			target = dir
		}
	}
	a.handoffPend = &pendingHandoff{summary: summary, cont: cont, workspace: target}
	return target, nil
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
	oldStore, oldHistory := a.store, a.history
	h := Handoff{
		Summary:      p.summary,
		OldID:        oldStore.id(),
		OldMsgs:      len(oldHistory),
		OldStats:     a.Stats(),
		OldFile:      a.SessionFile(),
		OldWorkspace: a.workspace,
		Workspace:    a.workspace,
		Continue:     p.cont,
		NoSave:       oldStore.disabled,
	}
	var wsErr error
	if p.workspace != "" {
		target, err := a.resolveWorkspace(p.workspace)
		switch {
		case err != nil:
			wsErr = fmt.Errorf(MsgHandoffBadWorkspaceFmt, p.workspace)
		case target != a.workspace:
			if err := a.loadWorkspace(target, oldStore.disabled); err != nil {
				wsErr = err
			}
		}
	}
	if wsErr != nil {
		h.Failed = true
		h.Err = briefErr(wsErr)
		a.lastHandoff = &h
		_ = a.AppendNotice(fmt.Sprintf(MsgHandoffFailNoticeFmt, h.Err, shortPath(a.workspace)))
		return
	}
	h.Workspace = a.workspace
	moved := h.Workspace != h.OldWorkspace
	newID := a.store.nextID()
	h.NewID = newID
	notice := Message{Role: "user", Content: fmt.Sprintf(MsgHandoffNoticeFmt, newID)}
	intro := fmt.Sprintf(MsgHandoffIntroFmt, h.OldID, p.summary)
	if moved {
		notice.Content = fmt.Sprintf(MsgHandoffNoticeWsFmt, newID, h.Workspace)
		intro = fmt.Sprintf(MsgHandoffIntroWsFmt, h.OldID, h.OldWorkspace, h.Workspace, p.summary)
		_ = oldStore.append(append(oldHistory, notice), "")
	} else {
		a.history = append(a.history, notice)
		_ = a.save()
	}
	a.store.rotateTo(newID)
	a.history = []Message{{Role: "user", Content: intro}}
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
		Summary   string `json:"summary"`
		Continue  *bool  `json:"continue"`
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ToolResult{Text: fmt.Sprintf(MsgParseArgs, err)}
	}
	cont := true
	if args.Continue != nil {
		cont = *args.Continue
	}
	target, err := t.target.RequestHandoff(args.Summary, cont, args.Workspace)
	if err != nil {
		return ToolResult{Text: MsgErrPrefix + err.Error()}
	}
	lines := handoffLines(normalizeHandoffSummary(args.Summary))
	if target != "" {
		return ToolResult{Text: fmt.Sprintf(MsgHandoffToolDoneWsFmt, lines, shortPath(target))}
	}
	return ToolResult{Text: fmt.Sprintf(MsgHandoffToolDoneFmt, lines)}
}
