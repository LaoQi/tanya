package attach

import (
	"os"
	"strings"
)

// TTYName 返回标准输入对应终端的短名（如 pts/3）；取不到时返回空串。
func TTYName() string {
	p, err := os.Readlink("/dev/fd/0")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(p, "/dev/")
}
