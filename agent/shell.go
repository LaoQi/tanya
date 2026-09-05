package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const shellMaxOutput = 30000

const shellWaitDelay = 2 * time.Second

type ShellResult struct {
	Command     string
	Stdout      []ShellChunk
	Stderr      []ShellChunk
	Err         string
	ExitCode    int
	TimedOut    bool
	Interrupted bool
	Duration    time.Duration
}

type ShellChunk struct {
	Data      string
	Truncated int64
}

func (r *ShellResult) String() string {
	var b strings.Builder
	writeStream(&b, "stdout", r.Stdout)
	writeStream(&b, "stderr", r.Stderr)
	if r.Interrupted {
		b.WriteString("error: 已中断\n")
	}
	if r.TimedOut {
		fmt.Fprintf(&b, "error: 执行超时\n")
	}
	if r.Err != "" {
		b.WriteString("error: " + r.Err + "\n")
	}
	if r.ExitCode != 0 {
		fmt.Fprintf(&b, "exit code: %d\n", r.ExitCode)
	}
	if b.Len() == 0 {
		return "(无输出，退出码 0)"
	}
	return b.String()
}

func writeStream(sb *strings.Builder, label string, chunks []ShellChunk) {
	if len(chunks) == 0 {
		return
	}
	fmt.Fprintf(sb, "%s:\n%s\n", label, chunks[0].Data)
	if len(chunks) > 1 {
		fmt.Fprintf(sb, "[%s 中间截断 %d 字节]\n%s\n", label, chunks[1].Truncated, chunks[1].Data)
	} else if chunks[0].Truncated > 0 {
		fmt.Fprintf(sb, "[%s 截断 %d 字节]\n", label, chunks[0].Truncated)
	}
}

type streamCapture struct {
	chunks   *[]ShellChunk
	head     []byte
	tail     []byte
	written  int64
	middle   int64
	headDone bool
}

func (c *streamCapture) Write(p []byte) (int, error) {
	n := len(p)
	c.written += int64(n)
	for len(p) > 0 {
		if !c.headDone {
			space := shellMaxOutput - len(c.head)
			if space > len(p) {
				space = len(p)
			}
			c.head = append(c.head, p[:space]...)
			p = p[space:]
			if len(c.head) == shellMaxOutput {
				c.headDone = true
			}
			continue
		}
		if len(c.tail) == shellMaxOutput {
			c.middle += shellMaxOutput / 2
			c.tail = c.tail[shellMaxOutput/2:]
		}
		space := shellMaxOutput - len(c.tail)
		if space > len(p) {
			space = len(p)
		}
		c.tail = append(c.tail, p[:space]...)
		p = p[space:]
	}
	return n, nil
}

func (c *streamCapture) finish() {
	if c.written == 0 {
		return
	}
	*c.chunks = append(*c.chunks, ShellChunk{Data: string(c.head)})
	if len(c.tail) > 0 {
		if c.middle == 0 {
			(*c.chunks)[0].Data += string(c.tail)
		} else {
			*c.chunks = append(*c.chunks, ShellChunk{Data: string(c.tail), Truncated: c.middle})
		}
	}
}

func RunShell(ctx context.Context, command string, timeoutSec int) string {
	return RunShellResult(ctx, command, timeoutSec).String()
}

func RunShellResult(ctx context.Context, command string, timeoutSec int) *ShellResult {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}
	res := &ShellResult{Command: command}
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", command)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = shellWaitDelay
	var stdout, stderr streamCapture
	stdout.chunks = &res.Stdout
	stderr.chunks = &res.Stderr
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	stdout.finish()
	stderr.finish()
	res.Duration = time.Since(start)

	switch {
	case ctx.Err() != nil:
		res.Interrupted = true
	case runCtx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
	case err != nil:
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.Err = err.Error()
		}
	}
	return res
}
