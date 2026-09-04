package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const shellMaxOutput = 30000

func RunShell(command string, timeoutSec int) string {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	var b strings.Builder
	if out := truncateOutput(stdout.String(), shellMaxOutput); out != "" {
		b.WriteString("stdout:\n" + out + "\n")
	}
	if serr := truncateOutput(stderr.String(), shellMaxOutput); serr != "" {
		b.WriteString("stderr:\n" + serr + "\n")
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
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

func truncateOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	head := max * 8 / 10
	tail := max - head
	return s[:head] + fmt.Sprintf("\n[...截断 %d 字节...]\n", len(s)-max) + s[len(s)-tail:]
}
