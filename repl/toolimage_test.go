package repl

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
	"github.com/LaoQi/tanya/render/present"
)

func imageResult() agent.ToolResult {
	return agent.ToolResult{
		Text:   "已读取 a.png（2×2，1.0k）",
		Images: []agent.ImageRef{{MIME: "image/png", Data: "AAAA", Name: "a.png", Bytes: 1024, Width: 2, Height: 2}},
	}
}

func TestRenderToolEndAppendImagePlaceholder(t *testing.T) {
	got := RenderToolEndAppend(testSem(), ttyRich(), "read_image", imageResult(), testViews(), 80, 20)
	if !strings.Contains(got, "已读取 a.png") {
		t.Fatalf("结果正文缺失: %q", got)
	}
	if !strings.Contains(got, "[图 a.png 1.0k]") {
		t.Errorf("结果后应追加图像占位行: %q", got)
	}
	if strings.Index(got, "已读取 a.png") > strings.Index(got, "[图 a.png") {
		t.Errorf("占位行应在结果之后: %q", got)
	}
}

func TestRenderToolEndAppendImagePlaceholderFallsBackToGeneric(t *testing.T) {
	views := present.NewRegistry()
	got := RenderToolEndAppend(testSem(), ttyRich(), "read_image", imageResult(), views, 80, 20)
	if !strings.Contains(got, "[图 a.png 1.0k]") {
		t.Errorf("未注册视图的工具也应带占位行: %q", got)
	}
	plain := agent.ToolResult{
		Text:   "已读取 b.png（2×2，2.0k）",
		Images: []agent.ImageRef{{Name: "b.png", Bytes: 2048}},
	}
	got = RenderToolEndAppend(testSem(), ttyRich(), "read_image", plain, views, 80, 20)
	if !strings.Contains(got, "[图 b.png 2.0k]") {
		t.Errorf("多来源图像应共用占位文案: %q", got)
	}
}

func TestRenderToolEndAppendNoImageStaysClean(t *testing.T) {
	got := RenderToolEndAppend(testSem(), ttyRich(), "read_image", agent.ToolResult{Text: "纯文本"}, testViews(), 80, 20)
	if strings.Contains(got, "[图") {
		t.Errorf("无图不应出现占位行: %q", got)
	}
}
