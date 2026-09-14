package repl

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// outMode 是输出模式：rich 全开；plain 只留正文与命令反馈（供父代理作为子代理调用）；
// plain+verbose 在 plain 基础上恢复工具块与状态行的纯文本形态。
type outMode uint8

const (
	modeRich outMode = iota
	modePlain
	modePlainVerbose
)

func ParseMode(plain, verbose bool) (outMode, error) {
	if verbose && !plain {
		return modeRich, errors.New(MsgVerboseRequiresPlain)
	}
	switch {
	case !plain:
		return modeRich, nil
	case verbose:
		return modePlainVerbose, nil
	default:
		return modePlain, nil
	}
}

// SingleShot 把 CLI 输出模式调整为单发（ask）默认档：rich 降到 plain+verbose，
// 显式 plain/plain+verbose（更窄或等价）保持不动。
func SingleShot(mode outMode) outMode {
	if mode == modeRich {
		return modePlainVerbose
	}
	return mode
}

func (m outMode) plain() bool  { return m != modeRich }
func (m outMode) decor() bool  { return m == modeRich }
func (m outMode) cursor() bool { return m == modeRich }

// visSet 是可输出的 Kind 集合。
type visSet uint16

var allKinds = []Kind{KindContent, KindReasoning, KindToolBlock, KindToolStatus, KindNotice, KindDecor, KindError, KindSpinner}

func allVisible() visSet { return visOf(allKinds...) }

func visOf(kinds ...Kind) visSet {
	var v visSet
	for _, k := range kinds {
		v |= 1 << k
	}
	return v
}

// 可见集三档：stderr 不参与屏蔽（诊断不被静默）。
func (m outMode) outVis() visSet {
	switch m {
	case modePlain:
		return visOf(KindContent, KindNotice)
	case modePlainVerbose:
		return visOf(KindContent, KindNotice, KindToolBlock, KindToolStatus)
	default:
		return allVisible()
	}
}

func (v visSet) has(k Kind) bool { return v&(1<<k) != 0 }

// output 是 repl 内唯一的写出口：持有 writer、按 Kind 门禁、保证单次写入原子、提供测试钩子。
// Write 是无门禁通道（raw 期自绘：Editor、picker），emit 与 atomic 是带 guard 的常规通道。
type output struct {
	mu    sync.Mutex
	w     io.Writer
	vis   visSet
	nl    bool
	guard func()
}

func newOutput(w io.Writer, vis visSet) *output {
	return &output{w: w, vis: vis}
}

// atLineStart 报告上一次写入是否以换行结尾。
func (o *output) atLineStart() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.nl
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, err := o.w.Write(p)
	if n > 0 {
		o.nl = p[n-1] == '\n'
	}
	return n, err
}

func (o *output) allows(kind Kind) bool { return o.vis.has(kind) }

func (o *output) emit(kind Kind, s string) {
	if s == "" || !o.allows(kind) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.guard != nil {
		o.guard()
	}
	io.WriteString(o.w, s)
	o.nl = strings.HasSuffix(s, "\n")
}

// atomic 在持锁状态下完成整块输出；回调内只允许写参数 w，不得再调用 output 方法（自锁）。
func (o *output) atomic(kind Kind, f func(w io.Writer)) {
	if !o.allows(kind) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.guard != nil {
		o.guard()
	}
	f(o.w)
}

func (o *output) setWriter(w io.Writer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.w = w
}

// streams 是两条独立互斥的输出流：out 承载正文与反馈，err 承载错误与诊断。
type streams struct {
	out  *output
	err  *output
	mode outMode
}

func NewStreams(stdout, stderr io.Writer, mode outMode) *streams {
	return &streams{out: newOutput(stdout, mode.outVis()), err: newOutput(stderr, allVisible()), mode: mode}
}

func (s *streams) decor() bool { return s.mode.decor() }

// cursor 报告是否允许光标控制序列（上移重绘）；plain 下一律追加式输出。
func (s *streams) cursor() bool { return s.mode.cursor() }

// Print/Content/End/Fail 是 main 包可用的语义化出口（Kind 与模式不导出包外）。
func (s *streams) Print(text string)   { s.out.emit(KindNotice, text) }
func (s *streams) Content(text string) { s.out.emit(KindContent, text) }

// End 收尾：rich 沿用无条件补换行；plain 下仅在缺少行尾换行时补，保证 stdout 严格等于答案加一个换行。
func (s *streams) End() {
	if s.mode.plain() && s.out.atLineStart() {
		return
	}
	s.out.emit(KindContent, "\n")
}

func (s *streams) Fail(format string, args ...any) {
	s.err.emit(KindError, fmt.Sprintf(format, args...))
}
