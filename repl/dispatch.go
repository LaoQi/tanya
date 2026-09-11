package repl

import (
	"os"
	"strings"
	"unicode/utf8"
)

func firstToken(line string) string {
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		return line[:i]
	}
	return line
}

func isSlashCommand(line string) bool {
	tok := firstToken(line)
	for _, cmd := range slashCommands {
		if cmd == tok {
			return true
		}
	}
	return false
}

func isExitLine(line string) bool {
	tok := firstToken(line)
	return tok == "exit" || tok == "quit"
}

func dialogueText(line string) (string, bool) {
	r, size := utf8.DecodeRuneInString(line)
	if r != ':' && r != '：' {
		return "", false
	}
	return strings.TrimSpace(line[size:]), true
}

func (r *REPL) cwdLabel() string {
	cwd, _ := os.Getwd()
	return shortPath(cwd)
}

func shortPath(cwd string) string {
	if cwd == "" {
		return ""
	}
	if home, _ := os.UserHomeDir(); home != "" && (cwd == home || strings.HasPrefix(cwd, home+"/")) {
		cwd = "~" + cwd[len(home):]
	}
	parts := strings.Split(cwd, "/")
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}
		if strings.HasPrefix(parts[i], ".") && len(parts[i]) > 1 {
			r, _ := utf8.DecodeRuneInString(parts[i][1:])
			parts[i] = "." + string(r)
		} else {
			r, _ := utf8.DecodeRuneInString(parts[i])
			parts[i] = string(r)
		}
	}
	return strings.Join(parts, "/")
}
