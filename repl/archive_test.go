package repl

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/render/term"
)

const replSampleSession = `{"role":"system","content":"sys"}
{"role":"user","content":"归档前的问题"}
{"role":"assistant","content":"归档前的回答"}
`

func TestParseArchiveArg(t *testing.T) {
	const def = 16
	cases := []struct {
		arg      string
		wantKeep int
		wantWin  time.Duration
		wantErr  bool
	}{
		{arg: "", wantKeep: def},
		{arg: "  ", wantKeep: def},
		{arg: "20", wantKeep: 20},
		{arg: "1", wantKeep: 1},
		{arg: "0", wantKeep: 0},
		{arg: "7d", wantWin: 7 * 24 * time.Hour},
		{arg: "30d", wantWin: 30 * 24 * time.Hour},
		{arg: "12h", wantWin: 12 * time.Hour},
		{arg: "90m", wantWin: 90 * time.Minute},
		{arg: "45s", wantWin: 45 * time.Second},
		{arg: "12h30m", wantErr: true},
		{arg: "99999999999999999999", wantErr: true},
		{arg: "all", wantErr: true},
		{arg: "--dry-run", wantErr: true},
		{arg: "0d", wantErr: true},
		{arg: "-1h", wantErr: true},
		{arg: "-1", wantErr: true},
		{arg: "3x", wantErr: true},
		{arg: "d", wantErr: true},
		{arg: "1.5d", wantErr: true},
		{arg: "12.5", wantErr: true},
		{arg: "1d2h", wantErr: true},
		{arg: "1d 2d", wantErr: true},
		{arg: "none", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseArchiveArg(c.arg, def)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseArchiveArg(%q) 应报错，得到 %+v", c.arg, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseArchiveArg(%q) 失败: %v", c.arg, err)
			continue
		}
		if got.Keep != c.wantKeep || got.OlderThan != c.wantWin || got.DryRun || got.Exclude != "" {
			t.Errorf("ParseArchiveArg(%q) = %+v want keep=%d win=%v", c.arg, got, c.wantKeep, c.wantWin)
		}
	}
}
func seedOldSession(t *testing.T, dataDir, id string, age time.Duration) string {
	t.Helper()
	path := seedIntoSessionDir(t, dataDir, id+".jsonl", replSampleSession)
	old := time.Now().Add(-age).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return path
}

func archiveOldSession(t *testing.T, a *agent.Agent, dataDir, id string) string {
	t.Helper()
	seedOldSession(t, dataDir, id, 48*time.Hour)
	rep, err := a.ArchiveSessions(agent.ArchiveOptions{Exclude: a.SessionID(), Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sessions) != 1 {
		t.Fatalf("应归档 1 个会话: %+v", rep)
	}
	return rep.Volume
}

func newArchiveREPL(t *testing.T, a *agent.Agent, keys ...readline.KeyEvent) (*REPL, *fakeTerm, *syncBuf, *syncBuf) {
	t.Helper()
	ttyProfile(t, term.Profile{TTY: true, Colors: term.LevelNone})
	dev := newFakeTerm(keys...)
	out, errb := &syncBuf{}, &syncBuf{}
	r, err := NewREPL(a, "› ", WithStreams(NewStreams(out, errb, modeRich)), WithTerminal(dev, true))
	if err != nil {
		t.Fatal(err)
	}
	return r, dev, out, errb
}

func TestHandleCommandArchiveNonInteractive(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	path := seedOldSession(t, dir, "20260101-010000", 40*24*time.Hour)

	check := func(t *testing.T, r *REPL, out *syncBuf) {
		t.Helper()
		r.handleCommand("/archive")
		if !strings.Contains(out.String(), MsgArchiveOnlyTTY) {
			t.Errorf("非交互环境应提示不可用: %q", out.String())
		}
		if strings.Contains(out.String(), MsgArchiveConfirm) {
			t.Errorf("非交互环境不应询问: %q", out.String())
		}
	}

	cases := []struct {
		name string
		prof term.Profile
		mode outMode
		raw  bool
	}{
		{"非 raw 输入", term.Profile{TTY: true, Colors: term.LevelNone}, modeRich, false},
		{"纯文本模式", term.Profile{TTY: true, Colors: term.LevelNone}, modePlain, true},
		{"非终端输出", term.Profile{TTY: false, Colors: term.LevelNone}, modeRich, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ttyProfile(t, c.prof)
			out, errb := &syncBuf{}, &syncBuf{}
			r, err := NewREPL(a, "› ", WithStreams(NewStreams(out, errb, c.mode)), WithTerminal(newFakeTerm(line("y")), c.raw))
			if err != nil {
				t.Fatal(err)
			}
			check(t, r, out)
		})
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("未启用时不应删除源文件: %v", err)
	}
	if dirs, err := filepath.Glob(filepath.Join(dir, "workspaces", "*", "archive")); err != nil || len(dirs) != 0 {
		t.Errorf("未启用时不应创建 archive 目录: %v %v", dirs, err)
	}
}

func TestHandleCommandArchive(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, dev, out, errb := newArchiveREPL(t, a, typed("y")...)

	r.handleCommand("/archive")
	if !strings.Contains(out.String(), MsgArchiveNoneKeep) {
		t.Errorf("空目录应提示无候选: %q", out.String())
	}

	seedOldSession(t, dir, "20260101-010000", 40*24*time.Hour)
	out.Reset()
	dev.rewind()
	r.handleCommand("/archive")
	got := out.String()
	if !strings.Contains(got, "当前活跃会话 ") || !strings.Contains(got, "将归档 1 个（约 ") {
		t.Errorf("应先出预览（含活跃会话数）: %q", got)
	}
	if !strings.Contains(got, "保留最近 16 个") {
		t.Errorf("无参预览应报保留数: %q", got)
	}
	if !strings.Contains(got, MsgArchiveConfirm) {
		t.Errorf("应询问确认: %q", got)
	}
	if !strings.Contains(got, "已归档 1 个会话") {
		t.Errorf("确认后应归档: %q", got)
	}
	if len(r.ed.History()) != 0 {
		t.Errorf("确认答案不应进入输入历史: %v", r.ed.History())
	}
	volumes, err := filepath.Glob(filepath.Join(dir, "workspaces", "*", "archive", "archive-*.zip"))
	if err != nil || len(volumes) != 1 {
		t.Fatalf("应生成一卷: %v %v", volumes, err)
	}
	if !strings.Contains(got, filepath.Base(volumes[0])) {
		t.Errorf("归档输出应含卷名: %q", got)
	}
	if !strings.Contains(got, "→") || !strings.Contains(got, "B") {
		t.Errorf("归档输出应含大小变化: %q", got)
	}

	out.Reset()
	r.handleCommand("/archive")
	if !strings.Contains(out.String(), MsgArchiveNoneKeep) {
		t.Errorf("已归档的会话不应重复归档: %q", out.String())
	}

	errb.Reset()
	r.handleCommand("/archive 3x")
	if !strings.Contains(errb.String(), "无效的归档参数") {
		t.Errorf("非法参数应报错: %q", errb.String())
	}
	errb.Reset()
	r.handleCommand("/archive 1d 2d")
	if !strings.Contains(errb.String(), "无效的归档参数") {
		t.Errorf("多段参数应报非法参数: %q", errb.String())
	}
}
func TestArchiveReadOnlyBlocksDialogue(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	archiveOldSession(t, a, dir, "20260101-010000")
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())

	out.Reset()
	r.handleCommand("/load 20260101-010000")
	if !strings.Contains(out.String(), "归档只读，继续对话请 /fork") {
		t.Fatalf("/load 归档 id 应给只读提示: %q", out.String())
	}

	out.Reset()
	r.handleCommand("/stat")
	if !strings.Contains(out.String(), "归档只读 20260101-010000（未写入）") {
		t.Errorf("/stat 应显示归档只读: %q", out.String())
	}

	out.Reset()
	errb.Reset()
	term := newFakeTerm(line("你好"), line("：你好"), line("exit"))
	r2, out2, errb2 := newTestREPLAgent(t, a, term)
	if err := r2.Run(); err != nil {
		t.Fatal(err)
	}
	got := out2.String()
	if n := strings.Count(got, "归档只读会话"); n != 2 {
		t.Errorf("两种对话写法都应被拦截（含全角 :）: %d\n%s", n, got)
	}
	if strings.Contains(got, "错误") || errb2.String() != "" {
		t.Errorf("拦截不应产生错误输出: %q %q", got, errb2.String())
	}

	out.Reset()
	errb.Reset()
	r3, out3, _ := newTestREPLAgent(t, a, newFakeTerm())
	r3.handleCommand("/fork")
	if !strings.Contains(out3.String(), "已 fork 为新会话") {
		t.Errorf("归档态 /fork 应成功: %q", out3.String())
	}
}

func TestHandleCommandForkNotArchived(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir())
	r, out, _ := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/fork")
	if !strings.Contains(out.String(), MsgForkNotArchive) {
		t.Errorf("非归档态 /fork 应提示直接对话: %q", out.String())
	}
	if strings.Contains(out.String(), "已 fork") {
		t.Error("非归档态不应 fork")
	}
}

func TestSessRowArchivedMark(t *testing.T) {
	m := time.Date(2026, 9, 7, 13, 56, 0, 0, time.Local)
	list := []agent.SessionInfo{
		{ID: "20260907-135638", ModTime: m, Msgs: 3, Summary: "活动会话", MetaOK: true},
		{ID: "20260101-010000", ModTime: m, Msgs: 2, Summary: "归档会话", Archived: true, MetaOK: true},
	}
	var out syncBuf
	pickByNumber(list, NewStreams(&out, &syncBuf{}, modeRich).out)
	got := out.String()
	if !strings.Contains(got, SessArchMark+"归档会话") {
		t.Errorf("归档项摘要应带标记: %q", got)
	}
	if strings.Contains(got, SessArchMark+"活动会话") {
		t.Errorf("活动项不应带标记: %q", got)
	}
}

func volumeSessionIDs(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	ids := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		ids = append(ids, strings.TrimSuffix(filepath.Base(f.Name), ".jsonl"))
	}
	return ids
}

func TestHandleCommandArchiveCancel(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, _, out, errb := newArchiveREPL(t, a, typed("n")...)
	path := seedOldSession(t, dir, "20260101-010000", 40*24*time.Hour)

	r.handleCommand("/archive")
	got := out.String()
	if !strings.Contains(got, "当前活跃会话 ") || !strings.Contains(got, "将归档 1 个（约 ") {
		t.Errorf("应先出预览（含活跃会话数）: %q", got)
	}
	if !strings.Contains(got, MsgArchiveConfirm) {
		t.Errorf("应询问确认: %q", got)
	}
	if !strings.Contains(got, MsgArchiveCancel) {
		t.Errorf("拒绝后应提示取消: %q", got)
	}
	if strings.Contains(got, "已归档") {
		t.Errorf("拒绝后不应归档: %q", got)
	}
	if len(r.ed.History()) != 0 {
		t.Errorf("确认答案不应进入输入历史: %v", r.ed.History())
	}
	if errb.String() != "" {
		t.Errorf("取消不应报错: %q", errb.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("取消不应删除源文件: %v", err)
	}
	if dirs, err := filepath.Glob(filepath.Join(dir, "workspaces", "*", "archive")); err != nil || len(dirs) != 0 {
		t.Errorf("取消不应创建 archive 目录: %v %v", dirs, err)
	}
}
func TestHandleCommandArchiveExcludesCurrentSession(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, dev, out, _ := newArchiveREPL(t, a, typed("y")...)
	id := a.SessionID()
	if id == "" {
		t.Fatal("可写模式应有会话 id")
	}
	self := seedOldSession(t, dir, id, 40*24*time.Hour)
	seedOldSession(t, dir, "20260101-010000", 40*24*time.Hour)

	out.Reset()
	dev.rewind()
	r.handleCommand("/archive 0")
	if got := out.String(); !strings.Contains(got, "已归档 1 个会话") {
		t.Errorf("应只归档非当前会话: %q", got)
	} else if strings.Contains(got, "保留最近") {
		t.Errorf("Keep=0 的预览不应报保留数: %q", got)
	}
	if _, err := os.Stat(self); err != nil {
		t.Errorf("当前会话不应被归档: %v", err)
	}
	volumes, err := filepath.Glob(filepath.Join(dir, "workspaces", "*", "archive", "archive-*.zip"))
	if err != nil || len(volumes) != 1 {
		t.Fatalf("应生成一卷: %v %v", volumes, err)
	}
	for _, got := range volumeSessionIDs(t, volumes[0]) {
		if got == id {
			t.Errorf("当前会话不应进入卷: %v", got)
		}
	}
}
func TestHandleCommandArchiveSkippedOutput(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, _, out, _ := newArchiveREPL(t, a, typed("y")...)
	seedOldSession(t, dir, "20260101-010000", time.Minute)

	out.Reset()
	r.handleCommand("/archive 0")
	got := out.String()
	if !strings.Contains(got, "跳过 20260101-010000（"+agent.MsgArchiveSkipIdle+"）") {
		t.Errorf("跳过行输出异常: %q", got)
	}
	if !strings.Contains(got, MsgArchiveNone) {
		t.Errorf("无候选应提示: %q", got)
	}
}
func TestHandleCommandArchiveNoSave(t *testing.T) {
	a := newSessTestAgent(t, t.TempDir(), agent.NoSave(true))
	r, _, out, errb := newArchiveREPL(t, a)

	r.handleCommand("/archive")
	if !strings.Contains(errb.String(), agent.MsgArchiveNoSave) {
		t.Errorf("-n 下应报错: %q", errb.String())
	}
	if out.String() != "" {
		t.Errorf("-n 下不应有正常输出: %q", out.String())
	}
}
func TestHandleCommandForkNoSave(t *testing.T) {
	dir := t.TempDir()
	archiveOldSession(t, newSessTestAgent(t, dir), dir, "20260101-010000")

	a := newSessTestAgent(t, dir, agent.NoSave(true))
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())
	r.handleCommand("/load 20260101-010000")
	if !strings.Contains(out.String(), "归档只读") {
		t.Fatalf("应载入归档只读: %q", out.String())
	}

	out.Reset()
	r.handleCommand("/fork")
	got := out.String()
	if !strings.Contains(got, "已 fork 为新会话") || !strings.Contains(got, MsgForkNoSave) {
		t.Errorf("-n 下 fork 应提示未写入: %q %q", got, errb.String())
	}
}

func TestHandleCommandArchiveConfirmEOF(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, _, out, errb := newArchiveREPL(t, a)
	path := seedOldSession(t, dir, "20260101-010000", 40*24*time.Hour)

	r.handleCommand("/archive")
	got := out.String()
	if !strings.Contains(got, MsgArchiveConfirm) {
		t.Errorf("应询问确认: %q", got)
	}
	if !strings.Contains(got, MsgArchiveCancel) {
		t.Errorf("EOF 应按取消处理: %q", got)
	}
	if errb.String() != "" {
		t.Errorf("取消不应报错: %q", errb.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("取消不应删除源文件: %v", err)
	}
	if dirs, err := filepath.Glob(filepath.Join(dir, "workspaces", "*", "archive")); err != nil || len(dirs) != 0 {
		t.Errorf("取消不应创建 archive 目录: %v %v", dirs, err)
	}
	if len(r.ed.History()) != 0 {
		t.Errorf("确认读不应写入历史: %v", r.ed.History())
	}
}
