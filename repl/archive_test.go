package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
)

const replSampleSession = `{"role":"system","content":"sys"}
{"role":"user","content":"归档前的问题"}
{"role":"assistant","content":"归档前的回答"}
`

func TestParseArchiveArg(t *testing.T) {
	cases := []struct {
		arg     string
		want    time.Duration
		wantErr bool
	}{
		{arg: "", want: agent.ArchiveDefaultWindow},
		{arg: "  ", want: agent.ArchiveDefaultWindow},
		{arg: "all", want: 0},
		{arg: "ALL", want: 0},
		{arg: "7d", want: 7 * 24 * time.Hour},
		{arg: "30d", want: 30 * 24 * time.Hour},
		{arg: "12h", want: 12 * time.Hour},
		{arg: "90m", want: 90 * time.Minute},
		{arg: "12h30m", want: 12*time.Hour + 30*time.Minute},
		{arg: "0d", wantErr: true},
		{arg: "-1h", wantErr: true},
		{arg: "3x", wantErr: true},
		{arg: "d", wantErr: true},
		{arg: "1.5d", wantErr: true},
		{arg: "1d2h", wantErr: true},
		{arg: "none", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseArchiveArg(c.arg)
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
		if got.OlderThan != c.want || got.DryRun || got.Keep != 0 || got.Exclude != "" {
			t.Errorf("ParseArchiveArg(%q) = %+v want OlderThan=%v", c.arg, got, c.want)
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

func TestHandleCommandArchive(t *testing.T) {
	dir := t.TempDir()
	a := newSessTestAgent(t, dir)
	r, out, errb := newTestREPLAgent(t, a, newFakeTerm())

	r.handleCommand("/archive")
	if !strings.Contains(out.String(), MsgArchiveNone) {
		t.Errorf("空目录应提示无候选: %q", out.String())
	}

	seedOldSession(t, dir, "20260101-010000", 40*24*time.Hour)
	out.Reset()
	r.handleCommand("/archive")
	got := out.String()
	if !strings.Contains(got, "已归档 1 个会话") {
		t.Errorf("归档输出异常: %q", got)
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
	if !strings.Contains(out.String(), MsgArchiveNone) {
		t.Errorf("已归档的会话不应重复归档: %q", out.String())
	}

	out.Reset()
	errb.Reset()
	r.handleCommand("/archive 3x")
	if !strings.Contains(errb.String(), "无效的归档范围") {
		t.Errorf("非法参数应报错: %q", errb.String())
	}
	errb.Reset()
	r.handleCommand("/archive 1d 2d")
	if !strings.Contains(errb.String(), MsgArchiveUsage) {
		t.Errorf("多余参数应提示用法: %q", errb.String())
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
	if n := strings.Count(SessRow, "%s"); n != 4 || !strings.Contains(SessRow, "%3d") {
		t.Errorf("SessRow 动词口径不应变化: %q", SessRow)
	}
	item := sessSummary(list[1])
	if item != SessArchMark+"归档会话" {
		t.Errorf("补全项应复用同一标记: %q", item)
	}
}
