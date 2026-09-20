//go:build !linux && !darwin && !windows

package ctty

import "errors"

func Bell() error { return errors.New("ctty: 终端提示音在当前平台不可用") }
