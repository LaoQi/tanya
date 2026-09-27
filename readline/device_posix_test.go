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

const (
	keysMaskI = unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	keysMaskL = unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
)

func TestKeysTermiosClearsSignalAndCanonical(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	base, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if err := (&posixTTY{in: slave, out: slave}).Raw(); err != nil {
		t.Fatalf("Raw: %v", err)
	}
	got, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	fields := []struct {
		name string
		base uint32
		got  uint32
		mask uint32
	}{
		{"Iflag", base.Iflag, got.Iflag, keysMaskI},
		{"Lflag", base.Lflag, got.Lflag, keysMaskL},
		{"Oflag", base.Oflag, got.Oflag, unix.OPOST},
	}
	for _, f := range fields {
		if f.got&f.mask != 0 {
			t.Errorf("%s 未清位: 残留 0x%x（got 0x%x base 0x%x）", f.name, f.got&f.mask, f.got, f.base)
		}
		if f.got&^f.mask != f.base&^f.mask {
			t.Errorf("%s 误改掩码外其它位: got 0x%x base 0x%x", f.name, f.got, f.base)
		}
	}
	if got.Cc[unix.VMIN] != 0 || got.Cc[unix.VTIME] != 1 {
		t.Errorf("VMIN/VTIME 应为 0/1，得 %d/%d", got.Cc[unix.VMIN], got.Cc[unix.VTIME])
	}
}

func TestReaderTermiosKeepsOutputProcessing(t *testing.T) {
	_, slave := newTestPTY(t)
	fd := int(slave.Fd())
	base, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	tty := &posixTTY{in: slave, out: slave}
	if err := tty.ReaderRaw(); err != nil {
		t.Fatalf("ReaderRaw: %v", err)
	}
	got, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if got.Iflag&keysMaskI != 0 || got.Lflag&keysMaskL != 0 {
		t.Errorf("常驻读者仍应清输入侧与信号位: Iflag 0x%x Lflag 0x%x", got.Iflag, got.Lflag)
	}
	if got.Oflag&unix.OPOST == 0 || got.Oflag != base.Oflag {
		t.Errorf("常驻读者必须保留输出处理（输出链路依赖 ONLCR 换行）: Oflag 0x%x base 0x%x", got.Oflag, base.Oflag)
	}
	if got.Cc[unix.VMIN] != 0 || got.Cc[unix.VTIME] != 1 {
		t.Errorf("VMIN/VTIME 应为 0/1，得 %d/%d", got.Cc[unix.VMIN], got.Cc[unix.VTIME])
	}
	if err := tty.Raw(); err != nil {
		t.Fatalf("Raw: %v", err)
	}
	rawKeys, err := ctty.GetTermios(fd)
	if err != nil {
		t.Fatalf("getTermios: %v", err)
	}
	if rawKeys.Oflag&unix.OPOST != 0 {
		t.Error("独占 Raw 仍应清 OPOST（与常驻读者不同）")
	}
}

func TestSizeFromPTY(t *testing.T) {
	_, slave := newTestPTY(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 33, Col: 101}); err != nil {
		t.Fatalf("设置 winsize: %v", err)
	}
	got, ok := (&posixTTY{in: slave, out: slave}).Size()
	if !ok || got.Cols != 101 || got.Rows != 33 {
		t.Fatalf("Size = %+v ok=%v，期望 101x33", got, ok)
	}
	if _, ok := (&posixTTY{in: slave}).Size(); ok {
		t.Error("无输出设备时 Size 应报不可用")
	}
	f, err := os.CreateTemp("", "readline-size-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, ok := (&posixTTY{in: f, out: f}).Size(); ok {
		t.Error("非终端不应给出尺寸")
	}
}
