//go:build linux || darwin

package ctty

import (
	"os"
	"testing"
)

func TestIsRootMatchesEUID(t *testing.T) {
	want := os.Geteuid() == 0
	if got := IsRoot(); got != want {
		t.Errorf("IsRoot() = %v, want %v（euid=%d）", got, want, os.Geteuid())
	}
}
