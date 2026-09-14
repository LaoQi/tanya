package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LaoQi/tanyan/ctty"
)

func envSection(cwd string, profile *shellProfile) string {
	var b strings.Builder
	b.WriteString("# 环境\n")
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "CWD: %s\n", shortPath(cwd))
	fmt.Fprintf(&b, "SHELL: %s\n", profile.Name)
	if ctty.Supported {
		b.WriteString("TTY: 交互提示须写入 /dev/tty 才可见（stdout/stderr 被工具捕获）\n")
	}
	fmt.Fprintf(&b, "TIMEOUT: 默认 %ds（interactive 时 %ds），上限 %ds\n", shellTimeoutSec, shellInteractiveTimeoutSec, shellTimeoutLimit)
	fmt.Fprintf(&b, "OUTPUT: stdout/stderr 头尾各 %dKB，中间截断\n", shellMaxOutput/1000)
	return b.String()
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + p[len(home):]
		}
	}
	return p
}
