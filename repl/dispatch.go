package repl

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LaoQi/tanya/agent"
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

var (
	archiveDurArg   = regexp.MustCompile(`^([0-9]+)([dhms])$`)
	archiveCountArg = regexp.MustCompile(`^[0-9]+$`)
)

var archiveUnit = map[byte]time.Duration{
	'd': 24 * time.Hour,
	'h': time.Hour,
	'm': time.Minute,
	's': time.Second,
}

func ParseArchiveArg(arg string, defaultKeep int) (agent.ArchiveOptions, error) {
	arg = strings.TrimSpace(arg)
	switch {
	case arg == "":
		return agent.ArchiveOptions{Keep: defaultKeep}, nil
	case archiveCountArg.MatchString(arg):
		n, err := strconv.Atoi(arg)
		if err != nil {
			return agent.ArchiveOptions{}, fmt.Errorf(MsgArchiveBadArg, arg)
		}
		return agent.ArchiveOptions{Keep: n}, nil
	}
	d, ok := parseArchiveDuration(arg)
	if !ok || d <= 0 {
		return agent.ArchiveOptions{}, fmt.Errorf(MsgArchiveBadArg, arg)
	}
	return agent.ArchiveOptions{OlderThan: d}, nil
}

func parseArchiveDuration(arg string) (time.Duration, bool) {
	m := archiveDurArg.FindStringSubmatch(arg)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return time.Duration(n) * archiveUnit[m[2][0]], true
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
