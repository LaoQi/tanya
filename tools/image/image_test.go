package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 7, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadImageResolvesWorkspaceAndScales(t *testing.T) {
	dir := t.TempDir()
	raw := pngBytes(t, 1400, 700)
	writeFile(t, dir, "shot.png", raw)
	tool := New(Config{Workspace: func() string { return dir }, Resize: true, Detail: "low"})
	res := tool.Invoke(context.Background(), `{"path":"shot.png"}`)
	if len(res.Images) != 1 {
		t.Fatalf("应返回一张图: %+v", res)
	}
	ref := res.Images[0]
	if ref.Name != "shot.png" || ref.MIME != "image/png" {
		t.Errorf("元信息异常: %+v", ref)
	}
	if ref.Width != 512 || ref.Height != 256 {
		t.Errorf("low 档应缩到 512 长边: %d×%d", ref.Width, ref.Height)
	}
	if ref.Detail != "low" {
		t.Errorf("detail 应写入引用: %q", ref.Detail)
	}
	data, err := base64.StdEncoding.DecodeString(ref.Data)
	if err != nil {
		t.Fatalf("data 应为合法 base64: %v", err)
	}
	if len(data) != ref.Bytes {
		t.Errorf("字节数应为实际发送量: %d vs %d", len(data), ref.Bytes)
	}
	if ref.Bytes >= len(raw) {
		t.Errorf("应缩小: %d → %d", len(raw), ref.Bytes)
	}
	if !strings.Contains(res.Text, "已读取 shot.png（512×256，") {
		t.Errorf("结果文本异常: %q", res.Text)
	}
}

func TestReadImageDetailOverrideChangesTarget(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shot.png", pngBytes(t, 1400, 700))
	tool := New(Config{Workspace: func() string { return dir }, Resize: true, Detail: "low"})
	res := tool.Invoke(context.Background(), `{"path":"shot.png","detail":"high"}`)
	if len(res.Images) != 1 {
		t.Fatalf("应返回一张图: %+v", res)
	}
	ref := res.Images[0]
	if ref.Width != 1300 || ref.Height != 650 {
		t.Errorf("high 档应缩到 1300 长边: %d×%d", ref.Width, ref.Height)
	}
	if ref.Detail != "high" {
		t.Errorf("detail 覆盖应生效: %q", ref.Detail)
	}
}

func TestReadImageResizeOffKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shot.png", pngBytes(t, 1400, 700))
	tool := New(Config{Workspace: func() string { return dir }})
	res := tool.Invoke(context.Background(), `{"path":"shot.png"}`)
	if len(res.Images) != 1 || res.Images[0].Width != 1400 {
		t.Fatalf("关闭缩放应保留原尺寸: %+v", res.Images)
	}
}

func TestReadImageHomePath(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home, "shot.png", pngBytes(t, 8, 6))
	tool := New(Config{Home: home, Workspace: func() string { return t.TempDir() }})
	res := tool.Invoke(context.Background(), `{"path":"~/shot.png"}`)
	if len(res.Images) != 1 || res.Images[0].Name != "shot.png" {
		t.Fatalf("~ 应展开到家目录: %+v", res)
	}
	if !strings.Contains(res.Text, "（8×6，") {
		t.Errorf("结果文本异常: %q", res.Text)
	}
}

func TestReadImageErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "plain.txt", []byte("hello"))
	writeFile(t, dir, "big.png", pngBytes(t, 64, 64))
	tool := New(Config{Workspace: func() string { return dir }, MaxBytes: 100})
	cases := []struct {
		name string
		args string
		want string
	}{
		{"空 path", `{"path":"  "}`, MsgImagePathEmpty},
		{"缺失文件", `{"path":"nope.png"}`, "文件不存在"},
		{"目录", `{"path":"."}`, "不是常规文件"},
		{"非图像", `{"path":"plain.txt"}`, "不是受支持的图像"},
		{"超限", `{"path":"big.png"}`, "超过大小上限"},
		{"坏 detail", `{"path":"big.png","detail":"huge"}`, "无效 detail"},
		{"坏 JSON", `{bad`, "参数解析失败"},
	}
	for _, c := range cases {
		res := tool.Invoke(context.Background(), c.args)
		if !strings.Contains(res.Text, c.want) {
			t.Errorf("%s: got %q want 含 %q", c.name, res.Text, c.want)
		}
		if len(res.Images) != 0 {
			t.Errorf("%s: 失败不应附图: %+v", c.name, res.Images)
		}
	}
}

func TestReadImageDefinitionAndName(t *testing.T) {
	tool := New(Config{})
	if tool.Name() != "read_image" {
		t.Errorf("工具名: %q", tool.Name())
	}
	def := tool.Definition()
	if def.Type != "function" || def.Function.Name != "read_image" {
		t.Fatalf("definition 头异常: %+v", def)
	}
	if def.Function.Description == "" || len(def.Function.Parameters) == 0 {
		t.Fatalf("definition 不完整: %+v", def)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(def.Function.Parameters, &schema); err != nil {
		t.Fatalf("参数 schema 非法: %v", err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "path" {
		t.Errorf("required 应为 path: %v", schema.Required)
	}
}
