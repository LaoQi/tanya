//go:build linux

package agent

import (
	"fmt"
	"os"
)

func processStopped(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	return statState(string(b)) == "T"
}
