//go:build linux

package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunShellStdinIsNullDevice(t *testing.T) {
	start := time.Now()
	res := testShellTool(t).run(context.Background(), request{
		Command:    `read -r line; echo "RC=$? LINE=${line:-none}"`,
		TimeoutSec: 10,
	})
	elapsed := time.Since(start)
	out := shellOut(res)
	if !strings.Contains(out, "RC=1") || !strings.Contains(out, "LINE=none") {
		t.Fatalf("stdin 应接空设备（read 立即 EOF）: %+v", res)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stdin 未接空设备，read 阻塞了 %v", elapsed)
	}
}
