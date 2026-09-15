package repl

import "strings"

type Cmd uint8

const (
	CmdREPL Cmd = iota
	CmdAsk
	CmdInit
)

func ParseCommand(args []string) (Cmd, string) {
	if len(args) == 0 {
		return CmdREPL, ""
	}
	switch args[0] {
	case "ask":
		return CmdAsk, strings.Join(args[1:], " ")
	case "init":
		return CmdInit, strings.Join(args[1:], " ")
	default:
		return CmdREPL, ""
	}
}
