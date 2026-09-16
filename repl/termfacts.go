package repl

const defaultToolWidth = 80

type TermFacts struct {
	Cols   int
	ColsOK bool
}

func (f TermFacts) Width() int {
	if f.ColsOK && f.Cols > 0 {
		return f.Cols
	}
	return defaultToolWidth
}
