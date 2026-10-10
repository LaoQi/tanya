package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChatWirePlainTextShapeUnchanged(t *testing.T) {
	msgs := []Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}}
	b, err := json.Marshal(chatWireMessages(msgs, "low"))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]`
	if string(b) != want {
		t.Errorf("纯文本 wire 应逐字节不变:\n got %s\nwant %s", b, want)
	}
}

func TestChatWireImageParts(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "看图", Images: []ImageRef{{MIME: "image/png", Data: "AAAA", Name: "a.png"}}}}
	b, err := json.Marshal(chatWireMessages(msgs, "low"))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"user","content":[{"type":"text","text":"看图"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA","detail":"low"}}]}]`
	if string(b) != want {
		t.Errorf("chat 图像 part:\n got %s\nwant %s", b, want)
	}
}

func TestChatWireImageDetailAndURLComeFromRef(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "", Images: []ImageRef{
		{MIME: "image/jpeg", Data: "BBBB", Detail: "high"},
		{URL: "https://example.com/x.png"},
		{},
	}}}
	b, err := json.Marshal(chatWireMessages(msgs, "low"))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,BBBB","detail":"high"}},` +
		`{"type":"image_url","image_url":{"url":"https://example.com/x.png","detail":"low"}}]}]`
	if string(b) != want {
		t.Errorf("图像 detail/来源优先级:\n got %s\nwant %s", b, want)
	}
}

func TestChatWireDropsImagesOnAssistant(t *testing.T) {
	msgs := []Message{{Role: "assistant", Content: "答", Images: []ImageRef{{MIME: "image/png", Data: "AAAA"}}}}
	b, err := json.Marshal(chatWireMessages(msgs, "low"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "images") || strings.Contains(string(b), "image_url") {
		t.Errorf("非 user 消息不应带图像: %s", b)
	}
}

func TestResponsesInputPlainTextUnchanged(t *testing.T) {
	instructions, items := buildResponsesInput([]Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}}, "low")
	if instructions != "sys" {
		t.Fatalf("instructions: %q", instructions)
	}
	b, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"content":[{"type":"input_text","text":"hi"}],"role":"user","type":"message"}]`
	if string(b) != want {
		t.Errorf("responses 纯文本 input:\n got %s\nwant %s", b, want)
	}
}

func TestResponsesInputImageParts(t *testing.T) {
	_, items := buildResponsesInput([]Message{{Role: "user", Content: "看图", Images: []ImageRef{
		{MIME: "image/png", Data: "AAAA"},
		{URL: "https://example.com/x.png", Detail: "original"},
	}}}, "low")
	b, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"content":[{"type":"input_text","text":"看图"},` +
		`{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"low"},` +
		`{"type":"input_image","image_url":"https://example.com/x.png","detail":"original"}],` +
		`"role":"user","type":"message"}]`
	if string(b) != want {
		t.Errorf("responses 图像 part:\n got %s\nwant %s", b, want)
	}
}

func TestAskUserInputCarriesNoImages(t *testing.T) {
	m := newMockLLM(t, mockStep{content: "看到了"})
	a := newAgent(t, m)
	if err := a.Ask(context.Background(), "看一下 @shot.png 这张图里有什么", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(a.history) != 2 || len(a.history[0].Images) != 0 {
		t.Fatalf("用户输入不再附图: %+v", a.history)
	}
	if len(m.reqs) != 1 {
		t.Fatalf("请求数: %d", len(m.reqs))
	}
	content, ok := wireMap(t, m.reqs[0].Messages[1])["content"].(string)
	if !ok || !strings.Contains(content, "@shot.png") {
		t.Errorf("chat 请求应为纯文本并保留 @ 原文: %#v", wireMap(t, m.reqs[0].Messages[1])["content"])
	}
	file := a.SessionFile()
	if file == "" {
		t.Fatal("应已落盘")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"images"`) {
		t.Errorf("会话文件不应出现图像字段: %s", raw)
	}
}

func TestLoadSessionKeepsToolImages(t *testing.T) {
	m := newMockLLM(t,
		mockStep{toolCalls: []mockToolCall{{id: "c1", name: "read_image", args: `{"path":"a.png"}`}}},
		mockStep{content: "ok"},
	)
	a := newAgent(t, m, WithTools(&stubImageTool{}))
	if err := a.Ask(context.Background(), "看图", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	id := a.SessionID()
	a.NewSession()
	if err := a.LoadSession(id); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range a.History() {
		if len(msg.Images) == 1 && msg.Images[0].Data == "AAAA" {
			found = true
		}
	}
	if !found {
		t.Fatalf("载入应保留工具图像: %+v", a.History())
	}
}

func TestScanSessionFileHandlesLargeImageLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-100000.jsonl")
	big := strings.Repeat("A", 2<<20)
	line := `{"role":"user","content":"看图","images":[{"mime":"image/png","data":"` + big + `"}]}` + "\n"
	if err := os.WriteFile(path, []byte(`{"role":"system","content":"sys"}`+"\n"+line), 0o644); err != nil {
		t.Fatal(err)
	}
	si, err := scanSessionFile(path, "20260101-100000", time.Now(), sessionSummaryRunes)
	if err != nil {
		t.Fatalf("大行不应中断扫描: %v", err)
	}
	if si.Msgs != 1 || si.Summary != "看图" {
		t.Errorf("条数/摘要应完整: %+v", si)
	}
}

func TestEstimateImageTokens(t *testing.T) {
	cases := []struct {
		w, h int
		want int
	}{
		{0, 0, 1024},
		{1, 1, 179},
		{100, 100, 179},
		{544, 544, 179},
		{1300, 1300, 1024},
		{2000, 2000, 1024},
		{5000, 5000, 1024},
	}
	for _, c := range cases {
		if got := estimateImageTokens(c.w, c.h); got != c.want {
			t.Errorf("estimateImageTokens(%d,%d) = %d want %d", c.w, c.h, got, c.want)
		}
	}
}

func TestImageRefValueHelpers(t *testing.T) {
	if got := (ImageRef{MIME: "image/png", Data: "AAAA"}).URLValue(); got != "data:image/png;base64,AAAA" {
		t.Errorf("data URL: %q", got)
	}
	if got := (ImageRef{Data: "AAAA"}).URLValue(); got != "data:image/png;base64,AAAA" {
		t.Errorf("缺 MIME 应回落到 png: %q", got)
	}
	if got := (ImageRef{URL: "https://x/y.png", Data: "AAAA"}).URLValue(); got != "data:image/png;base64,AAAA" {
		t.Errorf("Data 优先于 URL: %q", got)
	}
	if (ImageRef{}).Present() {
		t.Error("空引用不应可发送")
	}
	if got := (ImageRef{Detail: "HIGH"}).DetailValue("low"); got != "high" {
		t.Errorf("detail 应归一化: %q", got)
	}
	if got := (ImageRef{Detail: "bogus"}).DetailValue("low"); got != "low" {
		t.Errorf("非法 detail 应回落: %q", got)
	}
	if got := (ImageRef{}).DetailValue(""); got != "" {
		t.Errorf("无 detail 且无回落应留空: %q", got)
	}
}

func TestValidateImageDetail(t *testing.T) {
	cfg := defaultConfig()
	cfg.ImageDetail = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Error("非法 image_detail 应报错")
	}
	cfg.ImageDetail = "low"
	if err := cfg.Validate(); err != nil {
		t.Errorf("合法 image_detail 不应报错: %v", err)
	}
	cfg.ImageDetail = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("留空应允许: %v", err)
	}
}

func TestScanSessionFileImageOnlySummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101-100000.jsonl")
	lines := `{"role":"system","content":"sys"}` + "\n" +
		`{"role":"user","images":[{"mime":"image/png","data":"AAAA"}],"content":""}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	si, err := scanSessionFile(path, "20260101-100000", time.Now(), sessionSummaryRunes)
	if err != nil {
		t.Fatal(err)
	}
	if si.Summary != "[图 1]" {
		t.Errorf("纯图消息摘要应回落: %q", si.Summary)
	}
	if si.Msgs != 1 {
		t.Errorf("条数: %d", si.Msgs)
	}
}
