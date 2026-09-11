package repl

import (
	"fmt"
	"strings"

	"github.com/LaoQi/tanyan/style"
)

var dirChangeCommands = []string{"cd", "pushd", "popd"}

func (r *REPL) tryLocalCommand(line string) bool {
	if isDirChangeCmd(line) {
		fmt.Printf("%s\n", style.Warn.Sprint(MsgCdBlocked))
		return true
	}
	return false
}

func isDirChangeCmd(line string) bool {
	tok := firstToken(firstSegment(line))
	for _, name := range dirChangeCommands {
		if tok == name {
			return true
		}
	}
	return false
}

func firstSegment(line string) string {
	if i := strings.IndexAny(line, ";|&"); i >= 0 {
		return line[:i]
	}
	return line
}
