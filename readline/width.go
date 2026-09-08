package readline

import "github.com/LaoQi/tanyan/style"

func stringWidth(s string) int {
	return style.Width(s)
}

func stripANSI(s string) string {
	return style.Strip(s)
}

func truncate(s string, w int) string {
	return style.Truncate(s, w)
}
