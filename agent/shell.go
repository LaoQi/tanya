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

type limitedBuffer struct {
	buf     []byte
	dropped int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if space := shellMaxOutput - len(b.buf); space > 0 {
		if space > n {
			space = n
		}
		b.buf = append(b.buf, p[:space]...)
		p = p[space:]
	}
	b.dropped += int64(len(p))
	return n, nil
}

func (b *limitedBuffer) report(sb *strings.Builder, label string) {
	if len(b.buf) > 0 {
		fmt.Fprintf(sb, "%s:\n%s\n", label, b.buf)
	}
	if b.dropped > 0 {
		fmt.Fprintf(sb, "[%s 截断 %d 字节]\n", label, b.dropped)
	}
}

func RunShell(ctx context.Context, command string, timeoutSec int) string {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", command)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = shellWaitDelay
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	var b strings.Builder
	stdout.report(&b, "stdout")
	stderr.report(&b, "stderr")
	switch {
	case ctx.Err() != nil:
		b.WriteString("error: 已中断\n")
	case runCtx.Err() == context.DeadlineExceeded:
		fmt.Fprintf(&b, "error: 执行超时（%ds）\n", timeoutSec)
	case err != nil:
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(&b, "exit code: %d\n", exitErr.ExitCode())
		} else {
			b.WriteString("error: " + err.Error() + "\n")
		}
	}
	if b.Len() == 0 {
		return "(无输出，退出码 0)"
	}
	return b.String()
}
