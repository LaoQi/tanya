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
		name, val, hasVal := splitAttachArg(fields[i])
		switch {
		case name == "list" || name == "l":
			out.List = true
		case name == "force" || name == "f":
			out.Force = true
		case name == "prune":
			out.Prune = true
		case name == "pid":
			if !hasVal {
				i++
				if i >= len(fields) {
					return AttachArgs{}, errors.New(MsgAttachUsage)
				}
				val = fields[i]
			}
			if err := setAttachPID(&out, val); err != nil {
				return AttachArgs{}, err
			}
		case isDigits(fields[i]):
			if err := setAttachPID(&out, fields[i]); err != nil {
				return AttachArgs{}, err
			}
		default:
			return AttachArgs{}, fmt.Errorf(MsgAttachBadArg, fields[i])
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

// splitAttachArg 把 attach 子命令的一个参数拆成名字与可选值：
// 单横线与双横线等价（-list 与 --list 同义，与主 flag 层「去横线后按名字匹配」一致），
// 值可写成 --pid=123 或 --pid 123。
func splitAttachArg(f string) (name, val string, hasVal bool) {
	if !strings.HasPrefix(f, "-") {
		return "", "", false
	}
	body := strings.TrimPrefix(f, "-")
	body = strings.TrimPrefix(body, "-")
	if eq := strings.IndexByte(body, '='); eq >= 0 {
		return body[:eq], body[eq+1:], true
	}
	return body, "", false
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
