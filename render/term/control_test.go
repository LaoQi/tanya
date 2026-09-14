package term

import "testing"

func TestControlSequences(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ClearLine", ClearLine(), "\x1b[K"},
		{"ClearLineHome", ClearLineHome(), "\r\x1b[K"},
		{"LineStart", LineStart(), "\r"},
		{"ScreenHome", ScreenHome(), "\x1b[2J\x1b[H"},
		{"CursorUp 1", CursorUp(1), "\x1b[1A"},
		{"CursorUp 5", CursorUp(5), "\x1b[5A"},
		{"CursorUp 0", CursorUp(0), ""},
		{"CursorUp -1", CursorUp(-1), ""},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
