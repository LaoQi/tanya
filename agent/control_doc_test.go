package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentToolParamsDocInSync(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	doc := filepath.Join(filepath.Dir(file), "..", "docs", "agent-control-tool.md")
	b, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	var want string
	inFence := false
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "```json") {
			inFence = true
			continue
		}
		if inFence && strings.HasPrefix(ln, "```") {
			inFence = false
			continue
		}
		if inFence && strings.HasPrefix(ln, `{"type":"object"`) {
			want = strings.TrimSpace(ln)
			break
		}
	}
	if want == "" {
		t.Fatalf("未在 %s 找到 schema golden 代码块", doc)
	}
	got := agentParams()
	if got != want {
		t.Errorf("文档 §7 golden 与 agentParams() 不一致:\n doc %q\ngot %q", want, got)
	}
}
