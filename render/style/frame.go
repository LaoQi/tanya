package style

import "github.com/LaoQi/tanya/render/term"

func (s Style) Frame(text string) string {
	clean := term.Sanitize(text, false)
	seq := s.SGR(term.GetProfile())
	if seq == "" {
		return clean
	}
	return seq + clean + term.Reset
}
