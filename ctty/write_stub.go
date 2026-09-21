//go:build !linux && !darwin && !windows

package ctty

import "errors"

var errNoTTYWrite = errors.New("ctty: 控制终端写入在当前平台不可用")

func Bell() error { return errNoTTYWrite }

func NotifyOSC(string) error { return errNoTTYWrite }
