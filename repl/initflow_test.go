package repl

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
)

func initWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

func initStreams(t *testing.T, mode outMode) (*streams, *syncBuf, *syncBuf) {
	t.Helper()
	ttyProfile(t, plainProf)
	out, errb := &syncBuf{}, &syncBuf{}
	return NewStreams(out, errb, mode), out, errb
}

func runInitStrip(t *testing.T, mode outMode, tty string) (string, error) {
	t.Helper()
	st, out, _ := initStreams(t, mode)
	cfg, err := agent.LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var r io.Reader
	if tty != "" {
		r = strings.NewReader(tty)
	}
	err = runInit(st, testSem(), cfg, r)
	return out.String(), err
}

func TestRunInitReportInteractiveYes(t *testing.T) {
	dir := initWorkdir(t)
	got, err := runInitStrip(t, modeRich, "y\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "初始化工作区 "+dir+"\n") {
		t.Errorf("报告应以工作区开头:\n%s", got)
	}
	for _, want := range []string{
		MsgInitAskIgnore,
		MsgInitTagNew + "  .tanya/sessions  会话与历史",
		MsgInitTagNew + "  .tanya/.gitignore  忽略 .tanya/ 全部内容",
		MsgInitTagNew + "  AGENTS.md  项目说明骨架",
		"  会话目录 " + filepath.Join(dir, ".tanya", "sessions"),
		MsgInitHint,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("报告缺少 %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".tanya", ".gitignore")); err != nil {
		t.Errorf("确认后应写入忽略文件: %v", err)
	}
}

func TestRunInitReportNonInteractive(t *testing.T) {
	dir := initWorkdir(t)
	got, err := runInitStrip(t, modeRich, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, MsgInitAskIgnore) {
		t.Errorf("非交互不应提问:\n%s", got)
	}
	if want := MsgInitTagSkip + "  .tanya/.gitignore  " + agent.MsgInitSkipNoTTY; !strings.Contains(got, want) {
		t.Errorf("报告缺少 %q:\n%s", want, got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tanya", ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("非交互不应写入忽略文件: %v", err)
	}
	if !strings.Contains(got, MsgInitTagNew+"  AGENTS.md") {
		t.Errorf("骨架仍应创建:\n%s", got)
	}
}

func TestRunInitDeclined(t *testing.T) {
	dir := initWorkdir(t)
	got, err := runInitStrip(t, modeRich, "\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := MsgInitTagSkip + "  .tanya/.gitignore  " + agent.MsgInitSkipDeclined; !strings.Contains(got, want) {
		t.Errorf("回车应视为拒绝:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".tanya", ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("拒绝后不应写入忽略文件: %v", err)
	}
}

func TestRunInitIdempotent(t *testing.T) {
	initWorkdir(t)
	if _, err := runInitStrip(t, modeRich, "y\n"); err != nil {
		t.Fatal(err)
	}
	again, err := runInitStrip(t, modeRich, "y\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(again, MsgInitTagNew) {
		t.Errorf("二次运行不应有新建项:\n%s", again)
	}
	if strings.Contains(again, MsgInitAskIgnore) {
		t.Errorf("忽略文件已存在时不应再提问:\n%s", again)
	}
	for _, want := range []string{
		MsgInitTagOld + "  .tanya/sessions",
		MsgInitTagOld + "  .tanya/.gitignore",
		MsgInitTagOld + "  AGENTS.md",
	} {
		if !strings.Contains(again, want) {
			t.Errorf("二次运行缺少 %q:\n%s", want, again)
		}
	}
}

func TestRunInitPlainKeepsReport(t *testing.T) {
	initWorkdir(t)
	got, err := runInitStrip(t, modePlain, "n\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, MsgInitTagNew+"  .tanya/sessions") {
		t.Errorf("plain 下报告仍应可见:\n%s", got)
	}
	if strings.ContainsAny(got, "\x1b") {
		t.Errorf("plain 不应含转义: %q", got)
	}
}

func TestRunInitFailsOnBlockedPath(t *testing.T) {
	dir := initWorkdir(t)
	if err := os.Mkdir(filepath.Join(dir, "AGENTS.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runInitStrip(t, modeRich, "n\n"); err == nil {
		t.Fatal("应报错")
	}
}
