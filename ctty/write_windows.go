//go:build windows

package ctty

import "os"

func writeTTY(s string) error {
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}

func Bell() error { return writeTTY("\a") }

// NotifyOSC 把已清洗的单行文本作为 OSC 9 通知写入控制台：尽力而为，宿主不认就只是没有效果。
func NotifyOSC(text string) error { return writeTTY(oscFrame(text)) }
