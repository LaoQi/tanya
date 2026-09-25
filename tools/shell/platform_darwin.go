//go:build darwin

package shell

import "golang.org/x/sys/unix"

var platform = fillDefaults(shellPlatform{
	Candidates:     posixCandidates,
	ConfigureGroup: posixConfigureGroup,
	KillGroup:      posixKillGroup,
	ProtectSignals: posixProtectSignals,
	ExitCode:       posixExitCode,
	ProcessStopped: darwinProcessStopped,
	Programs:       posixPrograms,
	Capabilities:   posixCapabilities,
})

const darwinStatusStopped = 4

func darwinProcessStopped(pid int) bool {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil {
		return false
	}
	return kp.Proc.P_stat == darwinStatusStopped
}
