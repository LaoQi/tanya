package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShortPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无 HOME")
	}
	if got := shortPath(home); got != "~" {
		t.Errorf("shortPath(home) = %q", got)
	}
	sub := filepath.Join(home, "x")
	if got := shortPath(sub); got != "~/x" {
		t.Errorf("shortPath(sub) = %q", got)
	}
	if got := shortPath("/usr/share"); got != "/usr/share" {
		t.Errorf("外部路径应原样: %q", got)
	}
}
