package repl

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/ctty"
)

func (r *REPL) autoArchivePrompt() {
	if r.agent == nil || r.st.mode.plain() || !ctty.Probe().StdoutTTY {
		return
	}
	sug, ok := r.agent.SuggestArchive()
	if !ok {
		return
	}
	tty, err := ctty.Open()
	if err != nil {
		return
	}
	defer tty.Close()
	runAutoArchive(r.st, r.agent, tty, sug)
}

func runAutoArchive(st *streams, a *agent.Agent, tty io.Reader, sug agent.ArchiveSuggestion) {
	st.Print(fmt.Sprintf(MsgAutoArchiveAsk, sug.Active, sug.Threshold, sug.Keep, sug.Candidates, formatBytes(sug.Bytes)))
	line, err := bufio.NewReader(tty).ReadString('\n')
	if line == "" && err != nil {
		st.Print("\n")
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
	default:
		st.Print(MsgAutoArchiveSkip)
		return
	}
	rep, err := a.ArchiveSessions(agent.ArchiveOptions{Keep: sug.Keep, Exclude: a.SessionID()})
	if err != nil {
		st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		return
	}
	st.Print(formatArchiveReport(rep))
}
