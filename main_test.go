package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/attach"
	"github.com/LaoQi/tanya/config"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/readline"
	"github.com/LaoQi/tanya/repl"
	"github.com/LaoQi/tanya/tools/shell"
)

func TestConfigExampleEmbedded(t *testing.T) {
	if strings.TrimSpace(configExampleFile) == "" {
		t.Fatal("嵌入的 config.example.yaml 为空")
	}
	if !strings.HasSuffix(configExampleFile, "\n") {
		t.Error("嵌入文本应以换行结尾")
	}
	if strings.Contains(configExampleFile, "\r") {
		t.Error("嵌入文本不应含 CR")
	}
}

func TestConfigExampleMatchesDefaults(t *testing.T) {
	var got config.Config
	if err := yaml.Unmarshal([]byte(configExampleFile), &got); err != nil {
		t.Fatalf("示例应可被 yaml 解析: %v", err)
	}
	want := config.Default()
	checks := []struct {
		key  string
		got  string
		want string
	}{
		{"base_url", got.BaseURL, want.BaseURL},
		{"api_key", got.APIKey, want.APIKey},
		{"model", got.Model, want.Model},
		{"temperature", strconv.FormatFloat(got.Temperature, 'g', -1, 64), strconv.FormatFloat(want.Temperature, 'g', -1, 64)},
		{"data_dir", expandTilde(got.DataDir), want.DataDir},
		{"session_mode", got.SessionMode, want.SessionMode},
		{"image_detail", got.ImageDetail, want.ImageDetail},
		{"image_max_bytes", strconv.Itoa(got.ImageMaxBytes), strconv.Itoa(want.ImageMaxBytes)},
		{"image_resize", strconv.FormatBool(got.ImageResize), strconv.FormatBool(want.ImageResize)},
		{"tool_output_lines", strconv.Itoa(got.ToolOutputLines), strconv.Itoa(want.ToolOutputLines)},
	}
	covered := make(map[string]bool, len(checks))
	for _, c := range checks {
		covered[c.key] = true
		if c.got != c.want {
			t.Errorf("示例的 %s = %q，代码默认值 = %q", c.key, c.got, c.want)
		}
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(configExampleFile), &node); err != nil {
		t.Fatalf("示例应可被 yaml 解析: %v", err)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		t.Fatal("示例应为顶层 yaml 映射")
	}
	root := node.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if key := root.Content[i].Value; !covered[key] {
			t.Errorf("示例显式键 %q 未纳入默认值比对", key)
		}
	}
}

func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, p[2:])
}

func TestUsageStartsWithHead(t *testing.T) {
	fs := flag.NewFlagSet("tanya", flag.ContinueOnError)
	registerFlags(fs)
	var buf bytes.Buffer
	writeUsage(&buf, fs)
	if got := buf.String(); !strings.HasPrefix(got, repl.UsageHead) {
		t.Errorf("用法应以 UsageHead 开头:\n%s", got)
	}
}

func TestUsageListsAllFlags(t *testing.T) {
	fs := flag.NewFlagSet("tanya", flag.ContinueOnError)
	registerFlags(fs)
	var buf bytes.Buffer
	writeUsage(&buf, fs)
	got := buf.String()
	fs.VisitAll(func(f *flag.Flag) {
		if !strings.Contains(got, "-"+f.Name) {
			t.Errorf("用法应列出选项 -%s:\n%s", f.Name, got)
		}
	})
}

func TestApplyModel(t *testing.T) {
	base := config.Default().Model
	cases := []struct{ name, in, want string }{
		{"空串不覆盖", "", base},
		{"纯空白不覆盖", "  \t ", base},
		{"有值覆盖", "gpt-5", "gpt-5"},
		{"去首尾空白", "  gpt-5  ", "gpt-5"},
	}
	for _, c := range cases {
		cfg := config.Default()
		cfg.ConfigPath = cfg.Path
		applyModel(cfg, c.in)
		if cfg.Model != c.want {
			t.Errorf("%s: Model = %q, want %q", c.name, cfg.Model, c.want)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("%s: 覆盖后应通过校验: %v", c.name, err)
		}
	}
}

func TestApplyModelBeatsEnv(t *testing.T) {
	t.Setenv("TANYA_MODEL", "env-model")
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("加载配置: %v", err)
	}
	if cfg.Model != "env-model" {
		t.Fatalf("env 应先于默认值生效: %q", cfg.Model)
	}
	applyModel(cfg, "cli-model")
	if cfg.Model != "cli-model" {
		t.Errorf("命令行应覆盖 env: %q", cfg.Model)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("覆盖后应通过校验: %v", err)
	}
}

func TestRootRefusalMatrix(t *testing.T) {
	refusedByDefault := ctty.IsRoot()
	cases := []struct {
		env     string
		refused bool
	}{
		{"", refusedByDefault},
		{"1", false},
		{"true", refusedByDefault},
		{"0", refusedByDefault},
	}
	for _, c := range cases {
		t.Setenv(envAllowRoot, c.env)
		err := rootRefusal()
		if (err != nil) != c.refused {
			t.Errorf("%s=%q: err = %v, want refused=%v", envAllowRoot, c.env, err, c.refused)
			continue
		}
		if err != nil && err.Error() != repl.MsgRootRefused {
			t.Errorf("%s=%q: 文案 = %q, want %q", envAllowRoot, c.env, err.Error(), repl.MsgRootRefused)
		}
	}
}

func TestEnvSectionNoCwdAndGolden(t *testing.T) {
	inv := shell.Invocation{Argv: []string{"/bin/bash", "-c"}, Name: "bash", Kind: shell.KindPosix}
	got := envSection(inv)
	want := "# 环境\n" +
		"OS: " + runtime.GOOS + "/" + runtime.GOARCH + "\n" +
		"SHELL: bash\n"
	if ctty.Supported {
		want += "TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）\n"
	}
	want += "TIMEOUT: 默认 60s（interactive 时 300s），上限 900s\n" +
		"OUTPUT: stdout/stderr 头尾各 30KB，中间截断\n"
	if got != want {
		t.Errorf("envSection 全串不匹配:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "CWD:") {
		t.Error("env 头部不应含 CWD 行（agent 不再感知工作区）")
	}
}

func TestSystemBaseAppendsEnvAfterPrompt(t *testing.T) {
	inv, err := shell.Resolve("")
	if err != nil {
		t.Skipf("当前环境无可解析 shell: %v", err)
	}
	base := systemBase(inv)
	if !strings.HasPrefix(base, strings.TrimRight(systemPromptFile, "\n")) {
		t.Error("内置提示词应在基座最前")
	}
	if !strings.Contains(base, "\n\n# 环境\n") {
		t.Errorf("环境段应紧随内置提示词之后: %q", base)
	}
}

type fakeConsole struct {
	stdin   readline.Lease
	stdinN  int
	stderr  error
	full    readline.Lease
	fullErr error
}

func (c *fakeConsole) BeginRead() error                      { return nil }
func (c *fakeConsole) EndRead()                              {}
func (c *fakeConsole) Sane()                                 {}
func (c *fakeConsole) ReadEvent() (readline.Event, error)    { return readline.Event{}, io.EOF }
func (c *fakeConsole) Subscribe(func(readline.Event)) func() { return func() {} }

func (c *fakeConsole) SubscribeKeys(func(readline.Event)) func() { return func() {} }
func (c *fakeConsole) Size() (readline.Size, bool)               { return readline.Size{}, false }
func (c *fakeConsole) LendStdin() (readline.Lease, error)        { c.stdinN++; return c.stdin, c.stderr }
func (c *fakeConsole) LendFull(*exec.Cmd, io.Writer) (readline.Lease, error) {
	return c.full, c.fullErr
}

type fakeLease struct{ f *os.File }

func (l fakeLease) Stdin() *os.File { return l.f }
func (l fakeLease) Release()        {}

func TestConsoleForShellAdapter(t *testing.T) {
	f, err := os.CreateTemp("", "tanya-adapter-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	c := consoleForShell{&fakeConsole{stdin: fakeLease{f}, full: fakeLease{f}}}
	lease, err := c.LendStdin()
	if err != nil || lease == nil {
		t.Fatalf("LendStdin 应透传租约: %v %v", lease, err)
	}
	if lease.Stdin() != f {
		t.Error("租约应交出同一 stdin 文件")
	}
	lease.Release()
	full, err := c.LendFull(nil, nil)
	if err != nil || full == nil || full.Stdin() != f {
		t.Fatalf("LendFull 应透传租约: %v %v", full, err)
	}

	want := errors.New("无控制终端")
	bad := consoleForShell{&fakeConsole{stderr: want, fullErr: want}}
	if l, err := bad.LendStdin(); l != nil || !errors.Is(err, want) {
		t.Errorf("借出失败应原样回传错误: %v %v", l, err)
	}
	if l, err := bad.LendFull(nil, nil); l != nil || !errors.Is(err, want) {
		t.Errorf("借出失败应原样回传错误: %v %v", l, err)
	}
}

func TestAttachRefused(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{attach.CodeNoSave, "不落盘模式"},
		{attach.CodeReadOnly, "归档只读"},
		{attach.CodeInProgress, "已有接管请求"},
		{attach.CodeNoSession, "没有可接管"},
		{attach.CodeIOError, "写入会话失败"},
		{attach.CodeVersion, "协议版本不匹配"},
		{attach.CodeBadRequest, "不支持接管注入"},
	}
	for _, c := range cases {
		if got := attachRefused(c.code); !strings.Contains(got, c.want) {
			t.Errorf("attachRefused(%q) = %q, 应含 %q", c.code, got, c.want)
		}
	}
	if got := attachRefused("unknown-code"); got != "unknown-code" {
		t.Errorf("未知码应原样返回: %q", got)
	}
}

func TestShortDur(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{90 * time.Second, "1m30s"},
		{2 * time.Hour, "2h00m"},
		{-time.Second, "0ms"},
		{0, "0ms"},
	}
	for _, c := range cases {
		if got := shortDur(c.d); got != c.want {
			t.Errorf("shortDur(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// staleEntry 在 ws 的 run 目录里写一条指向不存在 socket 的陈旧注册项。
func staleEntry(t *testing.T, ws string, pid int) {
	t.Helper()
	cfg := config.Default()
	cfg.SessionMode = "local"
	dir := agent.RunDirFor(&cfg.Config, ws)
	if err := attach.WriteEntry(dir, attach.Entry{
		PID: pid, Sock: attach.SockPath(dir, pid), State: attach.StateIdle, Started: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

type capturePrinter struct {
	mu    sync.Mutex
	lines []string
}

func (p *capturePrinter) Print(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = append(p.lines, s)
}

func (p *capturePrinter) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.lines, "")
}

// 目标发现时空闲、受理后才变忙：等待期也应刷出进度行（而非静默等待）。
func TestTakeOverLateBusyRefreshesProgress(t *testing.T) {
	old := attachWaitRefresh
	attachWaitRefresh = 20 * time.Millisecond
	t.Cleanup(func() { attachWaitRefresh = old })

	dir := t.TempDir()
	release := make(chan struct{})
	h := &busyLaterHandler{dir: dir, release: release}
	srv, err := attach.Serve(dir, os.Getpid(), h)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	h.sock = srv.SockPath()
	if err := attach.WriteEntry(dir, attach.Entry{
		PID: os.Getpid(), Sock: h.sock, State: attach.StateIdle, Started: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	target, err := attach.ReadEntry(attach.EntryPath(dir, os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}

	p := &capturePrinter{}
	done := make(chan struct{})
	var res attach.Result
	go func() {
		defer close(done)
		res, _ = takeOver(target, false, p)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(p.String(), "仍在执行中") {
		if time.Now().After(deadline) {
			t.Fatalf("目标后来变忙应刷进度行: %q", p.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	<-done
	if !res.OK {
		t.Fatalf("接管应成功: %+v", res)
	}
	if strings.Contains(p.String(), repl.MsgAttachWaiting) {
		t.Errorf("发现时目标空闲不应打首行等待提示: %q", p.String())
	}
}

type busyLaterHandler struct {
	dir     string
	sock    string
	release chan struct{}
}

func (h *busyLaterHandler) Takeover(force bool, peer attach.Peer, reply func(attach.Result) error) {
	_ = attach.WriteEntry(h.dir, attach.Entry{
		PID: os.Getpid(), Sock: h.sock, State: attach.StateBusy,
		TurnStarted: time.Now(), Started: time.Now(),
	})
	<-h.release
	_ = reply(attach.Result{OK: true, File: "x"})
}

type replyHandler struct {
	res attach.Result
	n   int32
}

func (h *replyHandler) Takeover(force bool, peer attach.Peer, reply func(attach.Result) error) {
	atomic.AddInt32(&h.n, 1)
	_ = reply(h.res)
}

// serveAttach 在 ws 的 run 目录里以 pid 起一个立即应答 res 的活实例。
func serveAttach(t *testing.T, ws string, pid int, res attach.Result) *replyHandler {
	t.Helper()
	cfg := config.Default()
	cfg.SessionMode = "local"
	dir := agent.RunDirFor(&cfg.Config, ws)
	h := &replyHandler{res: res}
	srv, err := attach.Serve(dir, pid, h)
	if err != nil {
		t.Fatalf("Serve(%d): %v", pid, err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if err := attach.WriteEntry(dir, attach.Entry{
		PID: pid, Sock: srv.SockPath(), State: attach.StateIdle, Started: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestRunAttachSelection(t *testing.T) {
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	inWs := func(t *testing.T) *config.Config {
		t.Helper()
		if err := os.Chdir(t.TempDir()); err != nil {
			t.Fatal(err)
		}
		cfg := config.Default()
		cfg.DataDir = t.TempDir()
		cfg.SessionMode = "local"
		return cfg
	}

	t.Run("无实例报错", func(t *testing.T) {
		cfg := inWs(t)
		_, _, err := runAttach(cfg, repl.AttachArgs{}, &capturePrinter{})
		if err == nil || !strings.Contains(err.Error(), repl.MsgAttachNone) {
			t.Fatalf("应报无实例: %v", err)
		}
	})

	t.Run("空目录 list", func(t *testing.T) {
		cfg := inWs(t)
		p := &capturePrinter{}
		_, listed, err := runAttach(cfg, repl.AttachArgs{List: true}, p)
		if err != nil || !listed || !strings.Contains(p.String(), repl.MsgAttachListNone) {
			t.Fatalf("空 list 异常: %v %v %q", listed, err, p.String())
		}
	})

	t.Run("list 标记陈旧且不接管", func(t *testing.T) {
		cfg := inWs(t)
		ws, _ := os.Getwd()
		h := serveAttach(t, ws, 41001, attach.Result{OK: true, File: "f", Protocol: cfg.ApiProtocol})
		staleEntry(t, ws, 41002)
		p := &capturePrinter{}
		_, listed, err := runAttach(cfg, repl.AttachArgs{List: true}, p)
		if err != nil || !listed {
			t.Fatalf("list 应成功: %v %v", listed, err)
		}
		if !strings.Contains(p.String(), repl.MsgAttachListStale) || !strings.Contains(p.String(), "41001") {
			t.Fatalf("list 应含活实例与陈旧标记: %q", p.String())
		}
		if n := atomic.LoadInt32(&h.n); n != 0 {
			t.Fatalf("list 不应触发接管: %d", n)
		}
	})

	t.Run("多实例不自动选择", func(t *testing.T) {
		cfg := inWs(t)
		ws, _ := os.Getwd()
		serveAttach(t, ws, 41011, attach.Result{OK: true, File: "f1", Protocol: cfg.ApiProtocol})
		serveAttach(t, ws, 41012, attach.Result{OK: true, File: "f2", Protocol: cfg.ApiProtocol})
		p := &capturePrinter{}
		_, _, err := runAttach(cfg, repl.AttachArgs{}, p)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(repl.MsgAttachManyFmt, 2)) {
			t.Fatalf("应报多实例: %v", err)
		}
		if !strings.Contains(p.String(), "41011") || !strings.Contains(p.String(), "41012") {
			t.Fatalf("报错应列出候选: %q", p.String())
		}
	})

	t.Run("pid 精确命中", func(t *testing.T) {
		cfg := inWs(t)
		ws, _ := os.Getwd()
		serveAttach(t, ws, 41021, attach.Result{OK: true, File: "f1", Protocol: cfg.ApiProtocol})
		h := serveAttach(t, ws, 41022, attach.Result{OK: true, File: "f2", Protocol: cfg.ApiProtocol})
		res, listed, err := runAttach(cfg, repl.AttachArgs{PID: 41022}, &capturePrinter{})
		if err != nil || listed {
			t.Fatalf("pid 接管应成功: %v %v", listed, err)
		}
		if res == nil || res.File != "f2" {
			t.Fatalf("应接管指定 pid: %+v", res)
		}
		if n := atomic.LoadInt32(&h.n); n != 1 {
			t.Fatalf("只有目标被接管: %d", n)
		}
	})

	t.Run("pid 指向陈旧项报错且不删", func(t *testing.T) {
		cfg := inWs(t)
		ws, _ := os.Getwd()
		staleEntry(t, ws, 41031)
		_, _, err := runAttach(cfg, repl.AttachArgs{PID: 41031}, &capturePrinter{})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(repl.MsgAttachStaleFmt, 41031)) {
			t.Fatalf("应报陈旧: %v", err)
		}
		if _, serr := os.Stat(agent.RunDirFor(&cfg.Config, ws) + "/41031.json"); serr != nil {
			t.Fatalf("陈旧项不得删除: %v", serr)
		}
	})

	t.Run("pid 不存在报错", func(t *testing.T) {
		cfg := inWs(t)
		_, _, err := runAttach(cfg, repl.AttachArgs{PID: 41041}, &capturePrinter{})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(repl.MsgAttachNoPIDFmt, 41041)) {
			t.Fatalf("应报无此 pid: %v", err)
		}
	})

	t.Run("协议不一致拒绝", func(t *testing.T) {
		cfg := inWs(t)
		ws, _ := os.Getwd()
		serveAttach(t, ws, 41051, attach.Result{OK: true, File: "f", Protocol: "chat"})
		if cfg.ApiProtocol == "chat" {
			t.Skip("默认协议即 chat，无法构造差异")
		}
		_, _, err := runAttach(cfg, repl.AttachArgs{PID: 41051}, &capturePrinter{})
		if err == nil || !strings.Contains(err.Error(), repl.MsgAttachProtocolFmt[:10]) {
			t.Fatalf("应报协议不一致: %v", err)
		}
	})

	t.Run("无会话文件拒绝", func(t *testing.T) {
		cfg := inWs(t)
		ws, _ := os.Getwd()
		serveAttach(t, ws, 41061, attach.Result{OK: true, Protocol: cfg.ApiProtocol})
		_, _, err := runAttach(cfg, repl.AttachArgs{PID: 41061}, &capturePrinter{})
		if err == nil || err.Error() != repl.MsgAttachNoSessionFmt {
			t.Fatalf("应报无会话: %v", err)
		}
	})
}

func TestRunAttachPrune(t *testing.T) {
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.SessionMode = "local"
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ws, _ := os.Getwd()
	dir := agent.RunDirFor(&cfg.Config, ws)

	h := serveAttach(t, ws, 42001, attach.Result{OK: true, File: "f", Protocol: cfg.ApiProtocol})
	staleEntry(t, ws, 42002)
	tmp := attach.EntryPath(dir, 42003) + ".tmp"
	if err := os.WriteFile(tmp, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &capturePrinter{}
	_, listed, err := runAttach(cfg, repl.AttachArgs{Prune: true}, p)
	if err != nil || !listed {
		t.Fatalf("prune 应成功且不接管: %v %v", listed, err)
	}
	out := p.String()
	if !strings.Contains(out, "42002") || !strings.Contains(out, "42003") || !strings.Contains(out, "42001") {
		t.Fatalf("报告应含清理项与保留项: %q", out)
	}
	if strings.Contains(out, repl.MsgAttachHeader) {
		t.Fatalf("prune 不应打印列表表头: %q", out)
	}
	if !strings.Contains(out, "运行实例") {
		t.Fatalf("存活实例应标为运行实例: %q", out)
	}
	if n := atomic.LoadInt32(&h.n); n != 0 {
		t.Fatalf("prune 不得触发接管: %d", n)
	}
	if _, err := os.Stat(attach.EntryPath(dir, 42002)); !os.IsNotExist(err) {
		t.Fatal("陈旧项应被删除")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("tmp 残渣应被删除")
	}
	if _, err := os.Stat(attach.EntryPath(dir, 42001)); err != nil {
		t.Fatal("存活实例不得被删除")
	}
}

func TestAttachOpsListAndPruneShareText(t *testing.T) {
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.SessionMode = "local"
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ws, _ := os.Getwd()
	serveAttach(t, ws, 42101, attach.Result{OK: true, File: "f", Protocol: cfg.ApiProtocol})
	staleEntry(t, ws, 42102)
	dir := agent.RunDirFor(&cfg.Config, ws)

	ops := attachOps{runDir: func() (string, bool) { return dir, true }}
	rows, err := attach.Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := ops.List()
	if err != nil {
		t.Fatal(err)
	}
	if list != attachListText(dir, rows) {
		t.Fatalf("工具侧列表文本应与 --list 同源:\n%q\n%q", list, attachListText(dir, rows))
	}
	prune, err := ops.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prune, "42102") || !strings.Contains(prune, "42101") {
		t.Fatalf("清理报告内容异常: %q", prune)
	}

	noSave := attachOps{runDir: func() (string, bool) { return "", false }}
	if _, err := noSave.List(); err == nil || !strings.Contains(err.Error(), repl.MsgAttachPruneNoSave) {
		t.Fatalf("-n 应报不落盘: %v", err)
	}
	if _, err := noSave.Prune(); err == nil || !strings.Contains(err.Error(), repl.MsgAttachPruneNoSave) {
		t.Fatalf("-n 应报不落盘: %v", err)
	}
}
