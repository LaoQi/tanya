package readline

import (
	"strings"
	"testing"
)

func TestLayoutCursor(t *testing.T) {
	cases := []struct {
		name     string
		visible  string
		curWidth int
		cols     int
		wantRows int
		wantRow  int
		wantCol  int
	}{
		{"empty", "", 0, 10, 1, 0, 0},
		{"ascii mid", "abcdefghij", 5, 10, 1, 0, 5},
		{"ascii pending", "abcdefghij", 10, 10, 1, 0, 10},
		{"ascii wrap", "abcdefghijkl", 12, 10, 2, 1, 2},
		{"wide no cross", "中中中中中", 10, 10, 1, 0, 10},
		{"wide cross end", "abcdefghi中中", 13, 10, 2, 1, 4},
		{"wide cross mid", "abcdefghi中中", 11, 10, 2, 1, 2},
		{"wide cross after", "abcdefghi中x", 13, 10, 2, 1, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, row, col := layoutCursor([]rune(c.visible), c.curWidth, c.cols)
			if rows != c.wantRows || row != c.wantRow || col != c.wantCol {
				t.Errorf("layoutCursor(%q,%d,%d)=(%d,%d,%d) 期望 (%d,%d,%d)",
					c.visible, c.curWidth, c.cols, rows, row, col, c.wantRows, c.wantRow, c.wantCol)
			}
		})
	}
}

func TestLayoutCursorNoCols(t *testing.T) {
	rows, row, col := layoutCursor([]rune("abc"), 3, 0)
	if rows != 1 || row != 0 || col != 3 {
		t.Errorf("cols<=0 应退化为单行: (%d,%d,%d)", rows, row, col)
	}
}

func TestLayoutCursorWideColsCross(t *testing.T) {
	visible := []rune(strings.Repeat("x", 77) + "中中")
	rows, row, col := layoutCursor(visible, 81, 80)
	if rows != 2 || row != 1 || col != 2 {
		t.Errorf("cols=80 宽字符跨界: (%d,%d,%d) 期望 (2,1,2)", rows, row, col)
	}
}
