//go:build !linux && !darwin && !windows

package agent

var platform = fillDefaults(shellPlatform{
	Candidates: []string{"bash", "sh", "ash"},
})
