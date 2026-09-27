package readline

import (
	"bufio"
	"os"
	"strings"
)

type pipeDevice struct {
	r *bufio.Reader
}

func newPipeDevice() *pipeDevice {
	return &pipeDevice{r: bufio.NewReader(os.Stdin)}
}

func (d *pipeDevice) Raw() error { return ErrUnsupported }

func (d *pipeDevice) ReaderRaw() error { return ErrUnsupported }

func (d *pipeDevice) Restore() {}

func (d *pipeDevice) Sane() {}

func (d *pipeDevice) Size() (Size, bool) { return Size{}, false }

func (d *pipeDevice) readEvent() (Event, error) {
	if exitRequested() {
		return Event{}, ErrExited
	}
	line, err := d.r.ReadString('\n')
	if err != nil {
		return Event{}, err
	}
	return Event{Kind: EventKey, Key: KeyEvent{Code: KeyLine, Text: strings.TrimRight(line, "\r\n")}}, nil
}
