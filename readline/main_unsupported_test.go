//go:build !linux && !darwin

package readline

import (
	"os"
	"testing"
)

// 其余平台没有控制终端概念，无需脱离；TestMain 仍统一在此提供，
// 免得各平台测试文件各自定义导致重复声明。
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
