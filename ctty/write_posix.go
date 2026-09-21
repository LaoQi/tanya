//go:build linux || darwin

package ctty

func writeTTY(s string) error {
	tty, err := Open()
	if err != nil {
		return err
	}
	defer tty.Close()
	_, err = tty.WriteString(s)
	return err
}

func Bell() error { return writeTTY("\a") }

// NotifyOSC 把已清洗的单行文本作为 OSC 9 通知写入控制终端：尽力而为，终端不认就只是没有效果。
func NotifyOSC(text string) error { return writeTTY(oscFrame(text)) }
