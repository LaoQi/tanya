package tools

import (
	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/tools/builtin"
	"github.com/LaoQi/tanya/tools/shell"
)

type Options struct {
	ShellOverride string
	Home          string
	Workspace     func() string
	Bridge        shell.Bridge
	LookPath      func(string) (string, error)
	Programs      []string
}

func Shell(o Options) (*shell.Tool, error) {
	return shell.New(shell.Config{
		Override:  o.ShellOverride,
		LookPath:  o.LookPath,
		Home:      o.Home,
		Workspace: o.Workspace,
		Bridge:    o.Bridge,
		Programs:  o.Programs,
	})
}

func Standard(o Options) ([]agent.Tool, error) {
	sh, err := Shell(o)
	if err != nil {
		return nil, err
	}
	return append([]agent.Tool{sh}, builtin.Tools()...), nil
}
