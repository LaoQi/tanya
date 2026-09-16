package repl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/ctty"
	"github.com/LaoQi/tanya/render/theme"
)

func RunInit(st *streams, sem theme.Semantics, cfg *agent.Config) error {
	tty, err := ctty.Open()
	if err != nil {
		return runInit(st, sem, cfg, nil)
	}
	defer tty.Close()
	return runInit(st, sem, cfg, tty)
}

func runInit(st *streams, sem theme.Semantics, cfg *agent.Config, tty io.Reader) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	st.Print(fmt.Sprintf(MsgInitHead, initPath(cwd)))
	var confirm func() bool
	if tty != nil {
		confirm = func() bool { return askIgnore(st, tty) }
	}
	rep, err := agent.InitWorkspace(cfg, agent.InitOptions{ConfirmIgnore: confirm})
	if err != nil {
		return err
	}
	printInit(st, sem, rep)
	return nil
}

func askIgnore(st *streams, tty io.Reader) bool {
	st.Print(MsgInitAskIgnore)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if line == "" && err != nil {
		st.Print("\n")
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func printInit(st *streams, sem theme.Semantics, rep *agent.InitReport) {
	for _, e := range rep.Entries {
		path := relPath(rep.Workspace, e.Path)
		switch e.Action {
		case agent.InitCreated:
			st.Print(fmt.Sprintf(MsgInitEntryFmt, sem.Ok.Sprint(MsgInitTagNew), path, e.Note))
		case agent.InitExists:
			st.Print(fmt.Sprintf(MsgInitEntryFmt, sem.Dim.Sprint(MsgInitTagOld), path, e.Note))
		default:
			st.Print(fmt.Sprintf(MsgInitEntryFmt, sem.Dim.Sprint(MsgInitTagSkip), path, e.Reason))
		}
	}
	st.Print(fmt.Sprintf(MsgInitSessionFmt, initPath(rep.SessionDir)))
	st.Print(MsgInitHint)
}

func relPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return initPath(path)
	}
	return rel
}

func initPath(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + p[len(home):]
		}
	}
	return p
}
