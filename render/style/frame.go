package style

import "github.com/LaoQi/tanya/render/term"

func (b Bound) Frame(text string) string {
	clean := term.Sanitize(text, false)
	seq := b.Style.SGR(b.Profile)
	if seq == "" {
		return clean
	}
	return seq + clean + term.Reset
}
