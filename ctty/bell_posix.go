//go:build linux || darwin

package ctty

func Bell() error {
	tty, err := Open()
	if err != nil {
		return err
	}
	defer tty.Close()
	_, err = tty.WriteString("\a")
	return err
}
