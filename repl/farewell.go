package repl

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LaoQi/tanya/agent"
)

type farewellInfo struct {
	session  string
	duration time.Duration
	stats    agent.Stats
	file     string
	noSave   bool
}

func (r *REPL) farewell() {
	if r.agent == nil {
		return
	}
	r.st.out.emit(KindDecor, farewellText(r.farewellData()))
}

func (r *REPL) farewellData() farewellInfo {
	started := r.started
	if started.IsZero() {
		started = time.Now()
	}
	return farewellInfo{
		session:  r.agent.SessionID(),
		duration: time.Since(started),
		stats:    r.agent.Stats(),
		file:     r.agent.SessionFile(),
		noSave:   r.agent.NoSave(),
	}
}

func homePath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + p[len(home):]
	}
	return p
}

func farewellText(f farewellInfo) string {
	var b strings.Builder
	dur := turnDuration(f.duration)
	if f.session != "" {
		fmt.Fprintf(&b, MsgFarewellSessionFmt+"\n", f.session, dur, f.stats.Messages)
	} else {
		fmt.Fprintf(&b, MsgFarewellTimeFmt+"\n", dur, f.stats.Messages)
	}
	usage := fmt.Sprintf(MsgFarewellUsageFmt, totalsText(f.stats))
	if c := cacheRateTotalText(f.stats); c != "" {
		usage += fmt.Sprintf(MsgFarewellCacheTail, c)
	}
	b.WriteString(usage + "\n")
	switch {
	case f.file != "":
		fmt.Fprintf(&b, MsgFarewellFileFmt+"\n", homePath(f.file))
	case f.noSave:
		b.WriteString(MsgFarewellNoFile + "\n")
	}
	return b.String()
}
