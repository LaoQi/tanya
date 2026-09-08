package style

import "strconv"

func LineStart() string { return "\r" }

func ClearLine() string { return "\x1b[K" }

func ClearLineHome() string { return "\r\x1b[K" }

func CursorUp(n int) string {
	if n < 1 {
		return ""
	}
	return "\x1b[" + strconv.Itoa(n) + "A"
}

func ScreenHome() string { return "\x1b[2J\x1b[H" }
