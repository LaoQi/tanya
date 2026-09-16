//go:build !linux && !darwin && !windows

package agent

var platform = shellPlatform{
	Candidates:     []string{"bash", "sh", "ash"},
	ConfigureGroup: noopConfigureGroup,
	KillGroup:      defaultKillGroup,
	ProtectSignals: noopProtectSignals,
	ExitCode:       defaultExitCode,
	ProcessStopped: neverStopped,
	Capabilities:   func(*shellProfile) string { return "" },
}
