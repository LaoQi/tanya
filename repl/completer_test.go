package repl

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaoQi/tanya/agent"
)

func testCompleter(infos []agent.SessionInfo) *completer {
	return &completer{listSessions: func() ([]agent.SessionInfo, error) {
		return infos, nil
	}}
}

func TestSuggestLoadIDOnly(t *testing.T) {
	c := testCompleter([]agent.SessionInfo{{ID: "20260101-100000"}})
	if got := c.suggest("/load 20"); got != "260101-100000" {
		t.Errorf("ghost 应仅补 ID: %q", got)
	}
	if got := c.suggest("/load 99"); got != "" {
		t.Errorf("无匹配应为空: %q", got)
	}
	if got := c.suggest("/load 20260101-100000"); got != "" {
		t.Errorf("ID 已完整应为空: %q", got)
	}
}

func TestSuggestCommandContext(t *testing.T) {
	c := &completer{}
	if got := c.suggest("/he"); got != "lp" {
		t.Errorf("got %q", got)
	}
}

func testModelsCompleter(ids []string, err error, calls *int) *completer {
	return &completer{listModels: func() ([]string, error) {
		*calls++
		return ids, err
	}}
}

func TestSuggestModelContext(t *testing.T) {
	calls := 0
	c := testModelsCompleter([]string{"auto-flash", "auto-flash-pro", "glm-5"}, nil, &calls)
	if got := c.suggest("/model aut"); got != "o-flash" {
		t.Errorf("got %q", got)
	}
}

func TestSuggestThinkContext(t *testing.T) {
	c := &completer{}
	if got := c.suggest("/think hi"); got != "gh" {
		t.Errorf("got %q", got)
	}
	if got := c.suggest("/think of"); got != "f" {
		t.Errorf("off 候选: %q", got)
	}
	if got := c.suggest("/think off"); got != "" {
		t.Errorf("已完整应为空: %q", got)
	}
}

func TestCompleteThinkContext(t *testing.T) {
	c := &completer{}
	cands := c.complete("/think ")
	if len(cands) != len(agent.EffortLevels)+1 {
		t.Fatalf("应为全部等级加 off: %v", cands)
	}
	if cands[0].Insert != "/think minimal" || cands[0].Display != "minimal" {
		t.Errorf("got %+v", cands[0])
	}
	if cands[len(cands)-1].Insert != "/think off" {
		t.Errorf("末位应为 off: %+v", cands[len(cands)-1])
	}
	cands = c.complete("/think m")
	if len(cands) != 3 {
		t.Fatalf("m 应匹配 minimal/medium/max: %v", cands)
	}
}

func TestCompleteModelContext(t *testing.T) {
	calls := 0
	c := testModelsCompleter([]string{"auto-flash", "auto-flash-pro"}, nil, &calls)
	cands := c.complete("/model ")
	if len(cands) != 2 {
		t.Fatalf("got %v", cands)
	}
	if cands[0].Insert != "/model auto-flash" || cands[0].Display != "auto-flash" {
		t.Errorf("got %+v", cands[0])
	}
}

func TestModelsCacheOnce(t *testing.T) {
	calls := 0
	c := testModelsCompleter([]string{"m1"}, nil, &calls)
	c.models()
	c.models()
	c.complete("/model ")
	if calls != 1 {
		t.Errorf("模型列表应只加载一次: %d", calls)
	}
}

func TestModelsFailureNoRetry(t *testing.T) {
	calls := 0
	c := testModelsCompleter(nil, errors.New("down"), &calls)
	if got := c.models(); got != nil {
		t.Errorf("失败应返回空: %v", got)
	}
	c.models()
	if calls != 1 {
		t.Errorf("失败也不应重试: %d", calls)
	}
}

func TestCompleteLoadShowsSummary(t *testing.T) {
	c := testCompleter([]agent.SessionInfo{{
		ID:      "20260101-100000",
		ModTime: time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local),
		Msgs:    3,
		Summary: "测试摘要",
	}})
	cands := c.complete("/load 2")
	if len(cands) != 1 {
		t.Fatalf("应返回 1 个候选: %v", cands)
	}
	if cands[0].Insert != "/load 20260101-100000" {
		t.Errorf("Insert 应仅含 ID: %q", cands[0].Insert)
	}
	want := "/load 20260101-100000  01-01 10:00    3条  测试摘要"
	if cands[0].Display != want {
		t.Errorf("Display got %q want %q", cands[0].Display, want)
	}
}

func TestCompleteCommandContext(t *testing.T) {
	c := &completer{}
	cands := c.complete("/he")
	if len(cands) != 1 || cands[0].Insert != "/help" {
		t.Errorf("got %v", cands)
	}
	if cands := c.complete("/h"); len(cands) != 2 {
		t.Errorf("/h 应匹配 help 与 history: %v", cands)
	}
}

func TestCompleteLoadEmptyPrefix(t *testing.T) {
	c := testCompleter([]agent.SessionInfo{
		{ID: "20260904-215919", ModTime: time.Date(2026, 9, 4, 21, 59, 0, 0, time.Local), Msgs: 2, Summary: "说一句话"},
		{ID: "20260904-214341", ModTime: time.Date(2026, 9, 4, 21, 43, 0, 0, time.Local), Msgs: 1, Summary: "另一句"},
	})
	cands := c.complete("/load ")
	if len(cands) != 2 {
		t.Fatalf("应返回 2 个候选: %v", cands)
	}
	if strings.Contains(cands[0].Display, "214341") {
		t.Errorf("候选应按列表顺序: %q", cands[0].Display)
	}
}

func TestSuggestReasoningContext(t *testing.T) {
	c := &completer{}
	if got := c.suggest("/reasoning o"); got != "n" {
		t.Errorf("got %q", got)
	}
	if got := c.suggest("/reasoning of"); got != "f" {
		t.Errorf("off 候选: %q", got)
	}
	if got := c.suggest("/reasoning on"); got != "" {
		t.Errorf("已完整应为空: %q", got)
	}
}

func TestCompleteReasoningContext(t *testing.T) {
	c := &completer{}
	cands := c.complete("/reasoning ")
	if len(cands) != 2 {
		t.Fatalf("应为 on/off 两项: %v", cands)
	}
	if cands[0].Insert != "/reasoning on" || cands[0].Display != "on" || cands[1].Insert != "/reasoning off" {
		t.Errorf("got %+v", cands)
	}
}

func TestSlashCommandsIncludeReasoning(t *testing.T) {
	found := false
	for _, cmd := range slashCommands {
		if cmd == "/reasoning" {
			found = true
		}
	}
	if !found {
		t.Error("/reasoning 应在斜杠命令白名单内")
	}
}

func TestModelCandidatesSanitized(t *testing.T) {
	calls := 0
	c := testModelsCompleter([]string{"glm\x1b[2J-5", "bad\x1b]0;x\x07name"}, nil, &calls)
	if got := c.suggest("/model glm"); strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("ghost 应清洗: %q", got)
	}
	if got := c.suggest("/model glm"); got != "-5" {
		t.Errorf("ghost 文本应保留: %q", got)
	}
	cands := c.complete("/model ")
	for _, cd := range cands {
		if strings.ContainsAny(cd.Display, "\x1b\x07") || strings.ContainsAny(cd.Insert, "\x1b\x07") {
			t.Errorf("候选 Display/Insert 应清洗: %+v", cd)
		}
	}
	if len(cands) != 2 || cands[0].Insert != "/model glm-5" {
		t.Errorf("候选应保留可读文本: %+v", cands)
	}
}

func switchCompleter(ws string) *completer {
	return &completer{workspaceDir: func() string { return ws }}
}

func TestSuggestSwitchFirstDir(t *testing.T) {
	ws := t.TempDir()
	for _, d := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := switchCompleter(ws)
	if got := c.suggest("/switch "); got != "alpha/" {
		t.Errorf("空前缀 ghost 应为首个目录: got %q want %q", got, "alpha/")
	}
	if got := c.suggest("/switch be"); got != "ta/" {
		t.Errorf("前缀匹配 ghost: got %q want %q", got, "ta/")
	}
	if got := c.suggest("/switch alpha"); got != "/" {
		t.Errorf("精确目录名 ghost 应为尾分隔符: got %q", got)
	}
	if got := c.suggest("/switch nope"); got != "" {
		t.Errorf("无匹配不应有 ghost: got %q", got)
	}
}

func TestCompleteSwitchDirsOnly(t *testing.T) {
	ws := t.TempDir()
	for _, d := range []string{"alpha", "beta", "with space", ".hid"} {
		if err := os.Mkdir(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := switchCompleter(ws)
	got := c.complete("/switch ")
	if len(got) != 2 {
		t.Fatalf("应只补目录、跳过文件/空白名/隐藏目录: %+v", got)
	}
	if got[0].Insert != "/switch alpha/" || got[0].Display != "alpha/" {
		t.Errorf("候选 0: %+v", got[0])
	}
	if got[1].Insert != "/switch beta/" || got[1].Display != "beta/" {
		t.Errorf("候选 1: %+v", got[1])
	}
}

func TestCompleteSwitchHiddenGating(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, ".hid"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := switchCompleter(ws)
	if got := c.complete("/switch "); len(got) != 0 {
		t.Errorf("空前缀不应出现隐藏目录: %+v", got)
	}
	got := c.complete("/switch .")
	if len(got) != 1 || got[0].Insert != "/switch .hid/" {
		t.Errorf("敲 . 后应出现隐藏目录: %+v", got)
	}
}

func TestCompleteSwitchDrillDown(t *testing.T) {
	ws := t.TempDir()
	for _, d := range []string{"a/b1", "a/b2"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c := switchCompleter(ws)
	got := c.complete("/switch a/")
	if len(got) != 2 || got[0].Insert != "/switch a/b1/" || got[1].Insert != "/switch a/b2/" {
		t.Errorf("相对子路径下钻: %+v", got)
	}
	if g := c.suggest("/switch a/b"); g != "1/" {
		t.Errorf("下钻 ghost: got %q", g)
	}
}

func TestCompleteSwitchTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无 HOME")
	}
	if err := os.Mkdir(filepath.Join(home, "ws-complete-tilde"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := switchCompleter(t.TempDir())
	got := c.complete("/switch ~/ws-complete-tilde")
	if len(got) != 1 || got[0].Insert != "/switch ~/ws-complete-tilde/" {
		t.Errorf("~ 前缀补全: %+v", got)
	}
	got = c.complete("/switch ~")
	found := false
	for _, x := range got {
		if x.Insert == "/switch ~/ws-complete-tilde/" {
			found = true
		}
	}
	if !found {
		t.Errorf("裸 ~ 应补家目录一级（含新建目录）: %+v", got)
	}
	if g := c.suggest("/switch ~/ws-complete-tilde"); g != "/" {
		t.Errorf("~ 精确目录 ghost 应为尾分隔符: got %q", g)
	}
	if got := c.complete("/switch ~user"); got != nil {
		t.Errorf("~user 不展开应无候选: %+v", got)
	}
}

func TestCompleteSwitchAbsolute(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := switchCompleter(t.TempDir())
	got := c.complete("/switch " + dir + "/s")
	if len(got) != 1 || got[0].Insert != "/switch "+dir+"/sub/" {
		t.Errorf("绝对路径补全: %+v", got)
	}
}

func TestCompleteSwitchNoWorkspace(t *testing.T) {
	c := &completer{}
	if got := c.complete("/switch "); got != nil {
		t.Errorf("无工作区基准时空前缀应无候选: %+v", got)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := c.complete("/switch " + dir + "/")
	if len(got) != 1 || got[0].Insert != "/switch "+dir+"/sub/" {
		t.Errorf("无工作区基准时绝对路径仍可补全: %+v", got)
	}
}

func TestCompleteSwitchMissingBase(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := switchCompleter(ws)
	if got := c.complete("/switch missing/"); got != nil {
		t.Errorf("基准不存在应无候选: %+v", got)
	}
	if got := c.complete("/switch f"); got != nil {
		t.Errorf("文件名前缀应无候选: %+v", got)
	}
}

func TestCompleteSwitchSymlinkDir(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "real"), filepath.Join(ws, "link")); err != nil {
		t.Skip("无符号链接能力")
	}
	c := switchCompleter(ws)
	got := c.complete("/switch ")
	if len(got) != 2 || got[0].Insert != "/switch link/" || got[0].Display != "link/" {
		t.Errorf("符号链接目录应可补全（与切换口径一致）: %+v", got)
	}
	if g := c.suggest("/switch lin"); g != "k/" {
		t.Errorf("符号链接 ghost: got %q", g)
	}
	if err := os.Symlink(filepath.Join(ws, "nowhere"), filepath.Join(ws, "broken")); err != nil {
		t.Skip("无符号链接能力")
	}
	if got := c.complete("/switch broken"); got != nil {
		t.Errorf("断链不应出现候选: %+v", got)
	}
}

func TestCompleteSwitchDotDot(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := switchCompleter(ws)
	got := c.complete("/switch ..")
	if len(got) != 1 || got[0].Insert != "/switch ../" || got[0].Display != "../" {
		t.Errorf("裸 .. 应补 ../: %+v", got)
	}
	if g := c.suggest("/switch .."); g != "/" {
		t.Errorf("裸 .. ghost 应为 /: got %q", g)
	}
	got = c.complete("/switch a/..")
	if len(got) != 1 || got[0].Insert != "/switch a/../" {
		t.Errorf("下钻段中的 .. 同样应补: %+v", got)
	}
	if got := c.complete("/switch ."); got != nil {
		t.Errorf("裸 . 不应产生 ./ 候选（切换必被拒）: %+v", got)
	}
}

func TestCompleteSwitchControlNameSkipped(t *testing.T) {
	ws := t.TempDir()
	for _, name := range []string{"a\x7fb", "ok"} {
		if err := os.Mkdir(filepath.Join(ws, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c := switchCompleter(ws)
	got := c.complete("/switch ")
	if len(got) != 1 || got[0].Insert != "/switch ok/" {
		t.Errorf("含 DEL 的目录名应跳过: %+v", got)
	}
}

func TestSwitchCandidatesFollowWorkspace(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()
	for _, d := range []string{filepath.Join(ws1, "subA"), filepath.Join(ws2, "subB")} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var ws string
	c := &completer{workspaceDir: func() string { return ws }}
	ws = ws1
	if got := c.suggest("/switch "); got != "subA/" {
		t.Errorf("ws1 ghost: got %q", got)
	}
	ws = ws2
	if got := c.suggest("/switch "); got != "subB/" {
		t.Errorf("ws2 ghost（应跟随 /switch 换区）: got %q", got)
	}
}
