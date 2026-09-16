package ctty

import "os"

type Facts struct {
	StdinTTY  bool
	StdoutTTY bool
	Cols      int
	Rows      int
	SizeOK    bool
	VT        bool
	Kind      string
}

func Probe() Facts {
	f := Facts{
		StdinTTY:  IsTerminal(int(os.Stdin.Fd())),
		StdoutTTY: IsTerminal(int(os.Stdout.Fd())),
		Kind:      ConsoleKind(),
	}
	if cols, rows, ok := Size(int(os.Stdout.Fd())); ok {
		f.Cols, f.Rows, f.SizeOK = cols, rows, true
	}
	f.VT = EnableVT(int(os.Stdout.Fd()))
	return f
}
