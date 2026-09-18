package repl

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LaoQi/tanya/agent"
)

func (r *REPL) autoArchivePrompt() {
	if !r.archiveInteractive() {
		return
	}
	sug, ok := r.agent.SuggestArchive()
	if !ok {
		return
	}
	r.archiveFlow(agent.ArchiveOptions{Keep: sug.Keep}, "")
}

func (r *REPL) archiveFlow(opt agent.ArchiveOptions, noneArg string) {
	opt.Exclude = r.agent.SessionID()
	opt.DryRun = true
	rep, err := r.agent.ArchiveSessions(opt)
	if err != nil {
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		return
	}
	if len(rep.Sessions) == 0 {
		r.st.out.emit(KindNotice, archiveNoneText(opt, noneArg)+formatArchiveExtras(rep))
		return
	}
	r.st.out.emit(KindNotice, formatArchivePreview(rep, opt))
	line, cerr := r.readConfirm(MsgArchiveConfirm)
	if cerr != nil && strings.TrimSpace(line) == "" {
		r.st.out.emit(KindDecor, "\n")
	}
	if !archiveConfirmed(line) {
		r.st.out.emit(KindNotice, MsgArchiveCancel)
		return
	}
	opt.DryRun = false
	if rep, err = r.agent.ArchiveSessions(opt); err != nil {
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", err))
		return
	}
	r.st.out.emit(KindNotice, formatArchiveReport(rep))
}

func archiveConfirmed(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func archiveNoneText(opt agent.ArchiveOptions, arg string) string {
	switch {
	case opt.OlderThan > 0:
		return fmt.Sprintf(MsgArchiveNoneWindow, arg)
	case opt.Keep > 0:
		return MsgArchiveNoneKeep
	}
	return MsgArchiveNone
}

func formatArchivePreview(rep agent.ArchiveReport, opt agent.ArchiveOptions) string {
	keep := ""
	if opt.Keep > 0 {
		keep = fmt.Sprintf(MsgArchiveKeepTail, opt.Keep)
	}
	var b strings.Builder
	fmt.Fprintf(&b, MsgArchivePreview, rep.Active, len(rep.Sessions), formatBytes(rep.RawBytes), keep)
	b.WriteString(formatArchiveExtras(rep))
	return b.String()
}

func formatArchiveReport(rep agent.ArchiveReport) string {
	var b strings.Builder
	if len(rep.Sessions) > 0 {
		fmt.Fprintf(&b, MsgArchiveDone, len(rep.Sessions), filepath.Base(rep.Volume), formatBytes(rep.RawBytes), formatBytes(rep.VolumeBytes))
	} else {
		b.WriteString(MsgArchiveNone)
	}
	b.WriteString(formatArchiveExtras(rep))
	return b.String()
}

func formatArchiveExtras(rep agent.ArchiveReport) string {
	var b strings.Builder
	for _, s := range rep.Skipped {
		fmt.Fprintf(&b, MsgArchiveSkipFmt, s.ID, s.Reason)
	}
	for _, f := range rep.Failed {
		fmt.Fprintf(&b, MsgArchiveFailFmt, f.ID, f.Err)
	}
	return b.String()
}
