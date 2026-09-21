package readline

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type vtScreen struct {
	grid       [][]rune
	scrollback []string
	row, col   int
	cols       int
	rows       int
}

func newVTScreen(cols, rows int) *vtScreen {
	grid := make([][]rune, rows)
	for i := range grid {
		grid[i] = make([]rune, cols)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}
	return &vtScreen{grid: grid, cols: cols, rows: rows}
}

func (v *vtScreen) feed(s string) {
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) {
				v.csi(s[i+2:j], s[j])
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\r':
			v.col = 0
		case r == '\n':
			v.down()
		case r >= 0x20:
			v.put(r)
		}
	}
}

func (v *vtScreen) csi(params string, final byte) {
	n := 1
	if params != "" {
		if k, err := strconv.Atoi(params); err == nil && k > 0 {
			n = k
		}
	}
	switch final {
	case 'A':
		if v.row -= n; v.row < 0 {
			v.row = 0
		}
	case 'C':
		if v.col += n; v.col > v.cols-1 {
			v.col = v.cols - 1
		}
	case 'J':
		for c := v.col; c < v.cols; c++ {
			v.grid[v.row][c] = ' '
		}
		for r := v.row + 1; r < v.rows; r++ {
			for c := 0; c < v.cols; c++ {
				v.grid[r][c] = ' '
			}
		}
	case 'K':
		for c := v.col; c < v.cols; c++ {
			v.grid[v.row][c] = ' '
		}
	}
}

func (v *vtScreen) down() {
	if v.row+1 < v.rows {
		v.row++
		return
	}
	v.scrollback = append(v.scrollback, v.line(0))
	copy(v.grid, v.grid[1:])
	blank := make([]rune, v.cols)
	for i := range blank {
		blank[i] = ' '
	}
	v.grid[v.rows-1] = blank
}

func (v *vtScreen) put(r rune) {
	w := stringWidth(string(r))
	if w < 1 {
		w = 1
	}
	if v.col+w > v.cols {
		v.col = 0
		v.down()
	}
	v.grid[v.row][v.col] = r
	for k := 1; k < w && v.col+k < v.cols; k++ {
		v.grid[v.row][v.col+k] = 0
	}
	if v.col += w; v.col > v.cols-1 {
		v.col = v.cols - 1
	}
}

func (v *vtScreen) line(r int) string {
	var b strings.Builder
	for _, c := range v.grid[r] {
		if c != 0 {
			b.WriteRune(c)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func (v *vtScreen) dump() string {
	lines := make([]string, v.rows)
	for r := range v.grid {
		lines[r] = v.line(r)
	}
	return strings.Join(lines, "\n")
}
