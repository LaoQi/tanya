//go:build darwin

package shell

import (
	"strings"
	"testing"
)

func TestPlatformDarwinSharesPosixBase(t *testing.T) {
	if got := strings.Join(platform.Candidates, "/"); got != "bash/sh/ash" {
		t.Errorf("darwin 候选链 = %q", got)
	}
	if len(platform.Programs) == 0 {
		t.Error("darwin 程序清单不应为空（与 posix 共享）")
	}
	if platform.Capabilities == nil || platform.KillGroup == nil {
		t.Errorf("darwin 平台能力存在缺项: %+v", platform)
	}
}
