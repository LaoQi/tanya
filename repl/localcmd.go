package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanyan/style"
)

func (r *REPL) tryLocalCommand(line string) bool {
	if target, ok := cdTarget(line); ok {
		r.changeDir(target)
		return true
	}
	if cdLikeLine(line) {
		fmt.Printf("%s\n", style.Warn.Sprint(MsgCdSubshell))
		return true
	}
	return false
}

func cdTarget(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "cd" || len(fields) > 2 {
		return "", false
	}
	if len(fields) == 1 {
		return "", true
	}
	return fields[1], true
}

func cdLikeLine(line string) bool {
	switch firstToken(firstSegment(line)) {
	case "cd", "pushd", "popd":
		return true
	}
	return false
}

func firstSegment(line string) string {
	if i := strings.IndexAny(line, ";|&"); i >= 0 {
		return line[:i]
	}
	return line
}

func (r *REPL) changeDir(arg string) {
	target := arg
	switch arg {
	case "":
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, MsgErrLineFmt+"\n", err)
			return
		}
		target = home
	case "-":
		if r.prevCwd == "" {
			fmt.Print(MsgCdNoPrev)
			return
		}
		target = r.prevCwd
	default:
		target = expandHome(arg)
		if !filepath.IsAbs(target) {
			target = filepath.Join(r.baseCwd(), target)
		}
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		fmt.Printf(MsgCdBadDir, arg)
		return
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		abs = target
	}
	r.prevCwd = r.baseCwd()
	r.cwd = abs
}

func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}
