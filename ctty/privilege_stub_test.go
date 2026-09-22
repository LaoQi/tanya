//go:build !linux && !darwin

package ctty

import "testing"

func TestIsRootAlwaysFalse(t *testing.T) {
	if IsRoot() {
		t.Error("非 posix 平台 IsRoot() 应为 false")
	}
}
