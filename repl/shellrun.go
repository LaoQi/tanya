package repl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LaoQi/tanyan/agent"
	"github.com/LaoQi/tanyan/style"
)

const shellStopPollInterval = 200 * time.Millisecond

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

func shellCdLike(line string) bool {
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

func (r *REPL) runShellLine(line string) {
	if target, ok := cdTarget(line); ok {
		r.changeDir(target)
		return
	}
	if shellCdLike(line) {
		fmt.Printf("%s\n", style.Warn.Sprint(MsgCdSubshell))
		return
	}
	start := time.Now()
	cmd := agent.NewShellCmd(line)
	if r.cwd != "" {
		cmd.Dir = r.cwd
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if r.raw {
		cmd.Stdin = os.Stdin
	}
	defer suppressInterrupt()()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, MsgErrLineFmt+"\n", err)
		fmt.Print(turnSep(time.Since(start)))
		return
	}
	err, stopped, killErr := waitShellCmd(cmd)
	if killErr != nil {
		fmt.Fprintf(os.Stderr, MsgErrLineFmt+"\n", killErr)
	} else {
		reportShellExit(err, stopped)
	}
	fmt.Print(turnSep(time.Since(start)))
}

func suppressInterrupt() func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	return func() { signal.Stop(ch) }
}

func waitShellCmd(cmd *exec.Cmd) (error, bool, error) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(shellStopPollInterval)
	defer tick.Stop()
	hits := 0
	stopped := false
	for {
		select {
		case err := <-done:
			return err, stopped, nil
		case <-tick.C:
			if stopped || cmd.Process == nil || !agent.ProcessStopped(cmd.Process.Pid) {
				hits = 0
				continue
			}
			hits++
			if hits >= 2 {
				stopped = true
				if err := cmd.Process.Kill(); err != nil {
					return nil, true, fmt.Errorf(MsgKillFailFmt, cmd.Process.Pid, err)
				}
			}
		}
	}
}

func reportShellExit(err error, stopped bool) {
	if stopped {
		fmt.Print(MsgShellSuspended)
		return
	}
	if err == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", style.Error.Sprint(fmt.Sprintf(MsgShellExitCode, code)))
		}
		return
	}
	fmt.Fprintf(os.Stderr, MsgErrLineFmt+"\n", err)
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

func (r *REPL) baseCwd() string {
	if r.cwd != "" {
		return r.cwd
	}
	cwd, _ := os.Getwd()
	return cwd
}

func (r *REPL) cwdLabel() string {
	return shortPath(r.baseCwd())
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
