package repl

import (
	"fmt"

	"github.com/LaoQi/battlelab/agent"
)

func contextUsed(st agent.Stats) int {
	if st.ContextTokens > st.Est {
		return st.ContextTokens
	}
	return st.Est
}

// resetHandoffHint 在会话边界处重新武装提醒（换会话/载入/分支/交接后）。
func (r *REPL) resetHandoffHint() { r.hintDone = false }

func (r *REPL) checkHandoffHint() {
	if r.agent == nil || r.hintTokens <= 0 || r.hintDone {
		return
	}
	used := contextUsed(r.agent.Stats())
	if used < r.hintTokens {
		return
	}
	if err := r.agent.AppendNotice(fmt.Sprintf(MsgHandoffHintNoticeFmt, formatTokens(used), formatTokens(r.hintTokens))); err != nil {
		r.st.err.emitText(KindError, fmt.Sprintf(MsgHandoffHintFailedFmt, err))
		return
	}
	r.hintDone = true
	r.st.out.emit(KindNotice, fmt.Sprintf(MsgHandoffHintFmt, formatTokens(used), formatTokens(r.hintTokens)))
}
