package repl

import (
	"sort"

	"github.com/LaoQi/battlelab/agent"
	"github.com/LaoQi/tanya/attach"
)

// PickLast 从会话列表里挑出「最近使用」的活动会话（-r/--resume 用）：按 ModTime 降序，同 mtime 再按 ID 降序。
// 归档项不参与（其 mtime 取自卷内 entry，与活动文件不同源，且载入后为只读态）。
func PickLast(list []agent.SessionInfo) (agent.SessionInfo, bool) {
	cands := make([]agent.SessionInfo, 0, len(list))
	for _, si := range list {
		if si.Archived {
			continue
		}
		cands = append(cands, si)
	}
	if len(cands) == 0 {
		return agent.SessionInfo{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if !cands[i].ModTime.Equal(cands[j].ModTime) {
			return cands[i].ModTime.After(cands[j].ModTime)
		}
		return cands[i].ID > cands[j].ID
	})
	return cands[0], true
}

// SessionHolders 返回持有指定会话的活体实例（陈旧注册项不算）。
func SessionHolders(rows []attach.Row, id string) []attach.Entry {
	if id == "" {
		return nil
	}
	var out []attach.Entry
	for _, r := range rows {
		if !r.Alive || r.Entry.Session != id {
			continue
		}
		out = append(out, r.Entry)
	}
	return out
}
