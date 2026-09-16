package readline

import (
	"testing"
)

func feedAll(t *testing.T, p *keyParser, chunks ...string) []KeyEvent {
	t.Helper()
	var out []KeyEvent
	for _, c := range chunks {
		out = append(out, p.feed([]byte(c))...)
	}
	return out
}

func TestParsePrintable(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "abc")
	if len(evs) != 3 || evs[0].Code != KeyRune || evs[0].Rune != 'a' {
		t.Fatalf("got %+v", evs)
	}
}

func TestParseMultiByteRune(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "你", "好")
	if len(evs) != 2 || evs[0].Rune != '你' || evs[1].Rune != '好' {
		t.Fatalf("跨 chunk 的多字节字符解析失败: %+v", evs)
	}
}

func TestParseControlKeys(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "\r\x7f\t\x03\x04\x01\x05")
	want := []KeyCode{KeyEnter, KeyBackspace, KeyTab, KeyCtrlC, KeyCtrlD, KeyCtrlA, KeyCtrlE}
	if len(evs) != len(want) {
		t.Fatalf("got %d events", len(evs))
	}
	for i, w := range want {
		if evs[i].Code != w {
			t.Errorf("第 %d 个: got %v want %v", i, evs[i].Code, w)
		}
	}
}

func TestParseNewControlKeys(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "\x02\x06\x15\x0b\x17\x19\x14\x0c")
	want := []KeyCode{KeyCtrlB, KeyCtrlF, KeyCtrlU, KeyCtrlK, KeyCtrlW, KeyCtrlY, KeyCtrlT, KeyCtrlL}
	if len(evs) != len(want) {
		t.Fatalf("got %d: %+v", len(evs), evs)
	}
	for i, w := range want {
		if evs[i].Code != w {
			t.Errorf("第 %d 个: got %v want %v", i, evs[i].Code, w)
		}
	}
}

func TestParseAltWordKeys(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "\x1bb\x1bf")
	if len(evs) != 2 || evs[0].Code != KeyAltB || evs[1].Code != KeyAltF {
		t.Fatalf("Alt+B/F 解析失败: %+v", evs)
	}
	p2 := &keyParser{}
	evs2 := feedAll(t, p2, "\x1b", "b")
	if len(evs2) != 1 || evs2[0].Code != KeyAltB {
		t.Fatalf("Alt 拆包解析失败: %+v", evs2)
	}
	p3 := &keyParser{}
	evs3 := feedAll(t, p3, "\x1bx")
	if len(evs3) != 2 || evs3[0].Code != KeyEsc || evs3[1].Rune != 'x' {
		t.Fatalf("非 b/f 的 Alt 组合应保持原行为: %+v", evs3)
	}
}

func TestParseEscapeSequences(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "\x1b[A\x1b[B\x1b[C\x1b[D\x1b[H\x1b[F\x1b[3~\x1b[1~\x1b[4~")
	want := []KeyCode{KeyUp, KeyDown, KeyRight, KeyLeft, KeyHome, KeyEnd, KeyDelete, KeyHome, KeyEnd}
	if len(evs) != len(want) {
		t.Fatalf("got %d: %+v", len(evs), evs)
	}
	for i, w := range want {
		if evs[i].Code != w {
			t.Errorf("第 %d 个: got %v want %v", i, evs[i].Code, w)
		}
	}
}

func TestParseEscapeSplitChunks(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "\x1b", "[", "A")
	if len(evs) != 1 || evs[0].Code != KeyUp {
		t.Fatalf("拆包序列解析失败: %+v", evs)
	}
}

func TestFlushLoneEscape(t *testing.T) {
	p := &keyParser{}
	feedAll(t, p, "\x1b")
	if !p.needsMore() {
		t.Fatal("孤立 ESC 应处于等待状态")
	}
	evs := p.flush()
	if len(evs) != 1 || evs[0].Code != KeyEsc {
		t.Fatalf("flush 应产出 KeyEsc: %+v", evs)
	}
}

func TestMixedStream(t *testing.T) {
	p := &keyParser{}
	evs := feedAll(t, p, "he\x1b[C你\x1b[B\n")
	want := []KeyCode{KeyRune, KeyRune, KeyRight, KeyRune, KeyDown, KeyEnter}
	if len(evs) != len(want) {
		t.Fatalf("got %d: %+v", len(evs), evs)
	}
	for i, w := range want {
		if evs[i].Code != w {
			t.Errorf("第 %d 个: got %v want %v", i, evs[i].Code, w)
		}
	}
}

func TestStringWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"abc", 3},
		{"你好", 4},
		{"a你b", 4},
		{"", 0},
	}
	for _, c := range cases {
		if got := stringWidth(c.s); got != c.want {
			t.Errorf("stringWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}
