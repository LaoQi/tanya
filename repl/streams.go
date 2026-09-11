package repl

import (
	"fmt"
	"io"
	"sync"
)

// visSet 是可输出的 Kind 集合。阶段 4 引入 rich/plain/plain+verbose 三档，此处恒为全开。
type visSet uint16

func allVisible() visSet {
	var v visSet
	for _, k := range []Kind{KindContent, KindReasoning, KindToolBlock, KindToolStatus, KindNotice, KindDecor, KindError, KindSpinner} {
		v |= 1 << k
	}
	return v
}

func (v visSet) has(k Kind) bool { return v&(1<<k) != 0 }

// output 是 repl 内唯一的写出口：持有 writer、按 Kind 门禁、保证单次写入原子、提供测试钩子。
// Write 是无门禁通道（raw 期自绘：Editor、picker），emit 与 atomic 是带 guard 的常规通道。
type output struct {
	mu    sync.Mutex
	w     io.Writer
	vis   visSet
	guard func()
}

func newOutput(w io.Writer) *output {
	return &output{w: w, vis: allVisible()}
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.w.Write(p)
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
	out *output
	err *output
}

func NewStreams(stdout, stderr io.Writer) *streams {
	return &streams{out: newOutput(stdout), err: newOutput(stderr)}
}

// Print/Content/Fail 是 main 包可用的语义化出口（Kind 不导出包外）。
func (s *streams) Print(text string)   { s.out.emit(KindNotice, text) }
func (s *streams) Content(text string) { s.out.emit(KindContent, text) }
func (s *streams) Fail(format string, args ...any) {
	s.err.emit(KindError, fmt.Sprintf(format, args...))
}
