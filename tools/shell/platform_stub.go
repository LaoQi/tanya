//go:build !linux && !darwin && !windows

package shell

var platform = fillDefaults(shellPlatform{
	Candidates: []string{"bash", "sh", "ash"},
})
