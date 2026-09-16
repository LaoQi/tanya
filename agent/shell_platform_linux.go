//go:build linux

package agent

import (
	"fmt"
	"os"
	"strings"
)

var platform = fillDefaults(shellPlatform{
	Candidates:     posixCandidates,
	ConfigureGroup: posixConfigureGroup,
	KillGroup:      posixKillGroup,
	ProtectSignals: posixProtectSignals,
	ExitCode:       posixExitCode,
	ProcessStopped: linuxProcessStopped,
	Programs:       posixPrograms,
	Capabilities:   posixCapabilities,
})

func linuxProcessStopped(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	return statState(string(b)) == "T"
}

func statState(stat string) string {
	i := strings.IndexByte(stat, ')')
	if i < 0 || i+2 > len(stat) {
		return ""
	}
	fields := strings.Fields(stat[i+2:])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
