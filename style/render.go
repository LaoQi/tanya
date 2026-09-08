package style

import "strings"

type Renderer struct {
	Prof Profile
}

func (r Renderer) Inline(in ...Inline) string {
	var b strings.Builder
	var open Style
	hasOpen := false
	for _, i := range in {
		switch n := i.(type) {
		case Span:
			if hasOpen && n.Style == open {
				b.WriteString(n.Text)
				continue
			}
			if hasOpen {
				b.WriteString(resetSequence)
				hasOpen = false
			}
			if seq := r.Prof.sgr(n.Style); seq != "" {
				b.WriteString(seq)
				b.WriteString(n.Text)
				open = n.Style
				hasOpen = true
			} else {
				b.WriteString(n.Text)
			}
		case CodeSpan:
			b.WriteString(r.Inline(Dim.Text(n.Text)))
		case SoftBreak:
			b.WriteString("\n")
		}
	}
	if hasOpen {
		b.WriteString(resetSequence)
	}
	return b.String()
}

func Sprint(in ...Inline) string {
	return Renderer{Prof: current}.Inline(in...)
}
