//go:build linux

package shell

var platform = fillDefaults(shellPlatform{
	Candidates:     posixCandidates,
	ConfigureGroup: posixConfigureGroup,
	KillGroup:      posixKillGroup,
	ProtectSignals: posixProtectSignals,
	ExitCode:       posixExitCode,
	Programs:       posixPrograms,
	Capabilities:   posixCapabilities,
})
