//go:build windows

package ctty

import "os"

func Bell() error {
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\a")
	return err
}
