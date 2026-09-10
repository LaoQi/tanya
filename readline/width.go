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

func layoutCursor(visible []rune, curWidth, cols int) (rows, curRow, curCol int) {
	if cols <= 0 {
		return 1, 0, curWidth
	}
	row, col, g := 0, 0, 0
	set := false
	for _, r := range visible {
		w := stringWidth(string(r))
		if col == cols {
			row, col = row+1, 0
		}
		if w == 2 && col == cols-1 {
			row, col = row+1, 0
		}
		if !set && g == curWidth {
			curRow, curCol = row, col
			set = true
		}
		col += w
		g += w
	}
	if !set {
		curRow, curCol = row, col
	}
	return row + 1, curRow, curCol
}
