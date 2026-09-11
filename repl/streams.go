package repl

import (
	"io"
	"sync"
)

// output 是 repl 内唯一的写出口：持有 writer、保证单次写入原子、提供测试钩子。
// Write 是无门禁通道（raw 期自绘：Editor、picker），emit 与 atomic 是带 guard 的常规通道。
type output struct {
	mu    sync.Mutex
	w     io.Writer
	guard func()
}

func newOutput(w io.Writer) *output {
	return &output{w: w}
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.w.Write(p)
}

func (o *output) emit(s string) {
	if s == "" {
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
func (o *output) atomic(f func(w io.Writer)) {
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
