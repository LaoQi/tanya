package style

import "strings"

type Renderer struct {
	Prof Profile
}

func (r Renderer) Inline(in ...Inline) string {
	var b strings.Builder
	for _, i := range in {
		switch n := i.(type) {
		case Span:
			if seq := r.Prof.sgr(n.Style); seq != "" {
				b.WriteString(seq)
				b.WriteString(n.Text)
				b.WriteString(resetSequence)
			} else {
				b.WriteString(n.Text)
			}
		case CodeSpan:
			b.WriteString(r.Inline(Dim.Text(n.Text)))
		case SoftBreak:
			b.WriteString("\n")
		}
	}
	return b.String()
}

func Sprint(in ...Inline) string {
	return Renderer{Prof: current}.Inline(in...)
}
