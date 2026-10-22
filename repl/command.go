package repl

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Cmd uint8

const (
	CmdREPL Cmd = iota
	CmdAsk
	CmdInit
	CmdConfig
	CmdAttach
)

// AttachArgs 是 attach 子命令自己的选项（写在子命令之后：tanya attach --pid N 或 tanya attach N）。
type AttachArgs struct {
	PID   int
	List  bool
	Force bool
	Prune bool
}

func ParseAttachArgs(arg string) (AttachArgs, error) {
	var out AttachArgs
	fields := strings.Fields(arg)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "--list" || f == "-l":
			out.List = true
		case f == "--force" || f == "-f":
			out.Force = true
		case f == "--prune":
			out.Prune = true
		case f == "--pid" || strings.HasPrefix(f, "--pid="):
			v := strings.TrimPrefix(f, "--pid=")
			if f == "--pid" {
				i++
				if i >= len(fields) {
					return AttachArgs{}, errors.New(MsgAttachUsage)
				}
				v = fields[i]
			}
			if err := setAttachPID(&out, v); err != nil {
				return AttachArgs{}, err
			}
		case isDigits(f):
			if err := setAttachPID(&out, f); err != nil {
				return AttachArgs{}, err
			}
		default:
			return AttachArgs{}, fmt.Errorf(MsgAttachBadArg, f)
		}
	}
	switch {
	case out.Prune && out.List:
		return AttachArgs{}, errors.New(MsgAttachPruneConflict)
	case out.Prune && out.PID > 0:
		return AttachArgs{}, errors.New(MsgAttachPruneConflict)
	case out.Prune && out.Force:
		return AttachArgs{}, errors.New(MsgAttachPruneConflict)
	}
	return out, nil
}

func setAttachPID(out *AttachArgs, v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fmt.Errorf(MsgAttachBadPID, v)
	}
	if out.PID > 0 {
		return fmt.Errorf(MsgAttachDupPID, v)
	}
	out.PID = n
	return nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func ParseCommand(args []string) (Cmd, string) {
	if len(args) == 0 {
		return CmdREPL, ""
	}
	switch args[0] {
	case "ask":
		return CmdAsk, strings.Join(args[1:], " ")
	case "init":
		return CmdInit, strings.Join(args[1:], " ")
	case "config":
		return CmdConfig, strings.Join(args[1:], " ")
	case "attach":
		return CmdAttach, strings.Join(args[1:], " ")
	default:
		return CmdREPL, ""
	}
}
