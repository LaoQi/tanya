//go:build linux

package readline

import (
	"os"
	"testing"

	"github.com/LaoQi/tanya/ctty"
	"golang.org/x/sys/unix"
)

func newTestPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})
	return master, slave
}

func TestSaneEnablesISIG(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	term, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	term.Lflag &^= unix.ISIG
	if err := ctty.SetTermios(fd, term); err != nil {
		t.Fatalf("setTermios: %v", err)
	}
	(&posixTTY{in: slave}).Sane()
	got, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if got.Lflag&unix.ISIG == 0 {
		t.Fatalf("ISIG 未恢复: lflag=0x%x", got.Lflag)
	}
	if got.Lflag&unix.ICANON == 0 || got.Lflag&unix.ECHO == 0 {
		t.Errorf("其它 lflag 被误改: 0x%x", got.Lflag)
	}
}

func TestSaneAlreadySane(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	before, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	before.Lflag |= unix.ISIG
	if err := ctty.SetTermios(fd, before); err != nil {
		t.Fatalf("setTermios: %v", err)
	}
	(&posixTTY{in: slave}).Sane()
	got, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if got.Lflag != before.Lflag {
		t.Fatalf("ISIG 已置位时不应改动 lflag: 0x%x -> 0x%x", before.Lflag, got.Lflag)
	}
}

func TestSaneRestoresRawFlags(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	before, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	raw := before
	raw.Iflag &^= unix.ICRNL | unix.IXON
	raw.Lflag &^= unix.ISIG | unix.ICANON | unix.ECHO | unix.IEXTEN
	raw.Oflag &^= unix.OPOST | unix.ONLCR
	if err := ctty.SetTermios(fd, raw); err != nil {
		t.Fatalf("SetTermios: %v", err)
	}
	(&posixTTY{in: slave}).Sane()
	got, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("GetTermios: %v", err)
	}
	for _, f := range []struct {
		name string
		got  uint32
		want uint32
	}{
		{"ICRNL", got.Iflag, unix.ICRNL},
		{"IXON", got.Iflag, unix.IXON},
		{"ISIG", got.Lflag, unix.ISIG},
		{"ICANON", got.Lflag, unix.ICANON},
		{"ECHO", got.Lflag, unix.ECHO},
		{"IEXTEN", got.Lflag, unix.IEXTEN},
		{"OPOST", got.Oflag, unix.OPOST},
		{"ONLCR", got.Oflag, unix.ONLCR},
	} {
		if f.got&f.want == 0 {
			t.Errorf("%s 未恢复: iflag=0x%x lflag=0x%x oflag=0x%x", f.name, got.Iflag, got.Lflag, got.Oflag)
		}
	}
}

func TestSaneIgnoresNonTerminal(t *testing.T) {
	f, err := os.CreateTemp("", "readline-sane-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	(&posixTTY{in: f}).Sane()
}
