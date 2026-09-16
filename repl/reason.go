package repl

import (
	"fmt"
	"strings"
	"time"

	"github.com/LaoQi/tanya/render/theme"
)

// reasonSep 渲染思维链的上下分隔符（Think 语义色，前后各三条横线），收尾一侧附时长。
func reasonSep(sem theme.Semantics, label string, d time.Duration) string {
	text := label
	if d > 0 {
		text += fmt.Sprintf(MsgReasonDurFmt, turnDuration(d))
	}
	return sem.Think.Sprint(reasonRuleLeft+text+reasonRuleRight) + "\n"
}

func reasoningTag(on bool) string {
	if on {
		return MsgReasoningOnTag
	}
	return MsgReasoningOffTag
}

func (r *REPL) handleReasoning(args []string) {
	if len(args) == 0 {
		r.st.out.emit(KindNotice, fmt.Sprintf(MsgCurReasoning, reasoningTag(r.showReasoning)))
		return
	}
	switch strings.ToLower(args[0]) {
	case "on":
		r.showReasoning = true
		r.st.out.emit(KindNotice, MsgReasoningOn)
	case "off":
		r.showReasoning = false
		r.st.out.emit(KindNotice, MsgReasoningOff)
	default:
		r.st.err.emit(KindError, fmt.Sprintf(MsgErrLineFmt+"\n", fmt.Errorf(MsgReasoningBad, args[0])))
	}
}
