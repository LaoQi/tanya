package repl

import "github.com/LaoQi/tanya/readline"

const defaultToolWidth = 80

type TermFacts struct {
	Cols   int
	ColsOK bool
}

// LiveWidth 返回工具块宽度回调：每次渲染现取 Console 尺寸（缩放后自动跟随），
// 取不到时回落启动探测值（无终端/管道），再回落默认宽度。无状态，故不需要尺寸事件。
func LiveWidth(con readline.Console, facts TermFacts) func() int {
	return func() int {
		if con != nil {
			if s, ok := con.Size(); ok && s.Cols > 0 {
				return s.Cols
			}
		}
		return facts.Width()
	}
}

func (f TermFacts) Width() int {
	if f.ColsOK && f.Cols > 0 {
		return f.Cols
	}
	return defaultToolWidth
}
