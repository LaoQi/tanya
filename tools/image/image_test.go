package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LaoQi/tanya/agent"
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

func gifBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, w, h), palette.WebSafe)
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodePNG(t *testing.T, ref agent.ImageRef) image.Image {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(ref.Data)
	if err != nil {
		t.Fatalf("data 应为合法 base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("data 应为合法 PNG: %v", err)
	}
	return img
}

func TestReadImageRegion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shot.png", pngBytes(t, 1400, 700))
	tool := New(Config{Workspace: func() string { return dir }, Resize: true, Detail: "low"})
	res := tool.Invoke(context.Background(), `{"path":"shot.png","region":{"x":10,"y":20,"width":300,"height":200}}`)
	if len(res.Images) != 1 {
		t.Fatalf("应返回一张图: %+v", res)
	}
	ref := res.Images[0]
	if ref.Width != 300 || ref.Height != 200 {
		t.Errorf("区域读取应保留区域尺寸: %d×%d", ref.Width, ref.Height)
	}
	if ref.Detail != "low" {
		t.Errorf("detail 应写入引用: %q", ref.Detail)
	}
	img := decodePNG(t, ref)
	want00 := color.RGBA{R: 10, G: 20, B: 7, A: 255}
	if got := img.At(0, 0); got != want00 {
		t.Errorf("输出(0,0) 应等于原图(10,20): %v", got)
	}
	wantEdge := color.RGBA{R: 53, G: 219, B: 7, A: 255}
	if got := img.At(299, 199); got != wantEdge {
		t.Errorf("输出(299,199) 应等于原图(309,219): %v", got)
	}
	if !strings.Contains(res.Text, "已读取 shot.png 的区域（原图 1400×700，区域 (10,20) 300×200，输出 300×200，") {
		t.Errorf("结果文本异常: %q", res.Text)
	}
}

func TestReadImageRegionScalesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shot.png", pngBytes(t, 1400, 700))
	tool := New(Config{Workspace: func() string { return dir }, Resize: true, Detail: "low"})
	res := tool.Invoke(context.Background(), `{"path":"shot.png","region":{"x":0,"y":0,"width":1400,"height":700}}`)
	if len(res.Images) != 1 {
		t.Fatalf("应返回一张图: %+v", res)
	}
	ref := res.Images[0]
	if ref.Width != 512 || ref.Height != 256 {
		t.Errorf("超过目标长边的区域应缩放: %d×%d", ref.Width, ref.Height)
	}
	if !strings.Contains(res.Text, "区域 (0,0) 1400×700") || !strings.Contains(res.Text, "输出 512×256") {
		t.Errorf("结果文本异常: %q", res.Text)
	}
}

func TestReadImageRegionResizeOff(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shot.png", pngBytes(t, 1400, 700))
	tool := New(Config{Workspace: func() string { return dir }})
	res := tool.Invoke(context.Background(), `{"path":"shot.png","region":{"x":0,"y":0,"width":1400,"height":700}}`)
	if len(res.Images) != 1 || res.Images[0].Width != 1400 || res.Images[0].Height != 700 {
		t.Fatalf("关闭缩放应保留区域原尺寸: %+v", res.Images)
	}
}

func TestReadImageScaledHint(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "big.png", pngBytes(t, 1400, 700))
	writeFile(t, dir, "small.png", pngBytes(t, 100, 80))
	tool := New(Config{Workspace: func() string { return dir }, Resize: true, Detail: "low"})
	res := tool.Invoke(context.Background(), `{"path":"big.png"}`)
	if len(res.Images) != 1 {
		t.Fatalf("应返回一张图: %+v", res)
	}
	if !strings.Contains(res.Text, "可用 region 分块读取") {
		t.Errorf("整图被缩放时应提示 region: %q", res.Text)
	}
	res = tool.Invoke(context.Background(), `{"path":"small.png"}`)
	if strings.Contains(res.Text, "region") {
		t.Errorf("未缩放不应提示 region: %q", res.Text)
	}
	off := New(Config{Workspace: func() string { return dir }})
	res = off.Invoke(context.Background(), `{"path":"big.png"}`)
	if strings.Contains(res.Text, "region") {
		t.Errorf("关闭缩放不应提示 region: %q", res.Text)
	}
}

func TestReadImageRegionErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "shot.png", pngBytes(t, 1400, 700))
	writeFile(t, dir, "anim.gif", gifBytes(t, 8, 8))
	writeFile(t, dir, "bad.png", headerOnlyPNG(1400, 700))
	tool := New(Config{Workspace: func() string { return dir }, Resize: true})
	cases := []struct {
		name string
		args string
		want string
	}{
		{"GIF 不支持", `{"path":"anim.gif","region":{"x":0,"y":0,"width":4,"height":4}}`, MsgRegionUnsupported},
		{"GIF 且宽为零", `{"path":"anim.gif","region":{"x":0,"y":0,"width":0,"height":4}}`, MsgRegionUnsupported},
		{"宽为零", `{"path":"shot.png","region":{"x":0,"y":0,"width":0,"height":4}}`, MsgRegionInvalid},
		{"高为零", `{"path":"shot.png","region":{"x":0,"y":0,"width":4,"height":-1}}`, MsgRegionInvalid},
		{"缺字段", `{"path":"shot.png","region":{"x":1}}`, MsgRegionInvalid},
		{"完全在外", `{"path":"shot.png","region":{"x":2000,"y":600,"width":10,"height":10}}`, "1400×700）不相交"},
		{"贴右边界", `{"path":"shot.png","region":{"x":1400,"y":0,"width":10,"height":10}}`, "不相交"},
		{"坏图裁剪", `{"path":"bad.png","region":{"x":0,"y":0,"width":4,"height":4}}`, "读取区域失败"},
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

func TestReadImageRegionSchema(t *testing.T) {
	tool := New(Config{})
	var schema struct {
		Properties map[string]struct {
			Required []string `json:"required"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(tool.Definition().Function.Parameters, &schema); err != nil {
		t.Fatalf("参数 schema 非法: %v", err)
	}
	region, ok := schema.Properties["region"]
	if !ok {
		t.Fatalf("schema 应含 region: %v", schema.Properties)
	}
	if strings.Join(region.Required, ",") != "x,y,width,height" {
		t.Errorf("region required: %v", region.Required)
	}
	if strings.Join(schema.Required, ",") != "path" {
		t.Errorf("顶层 required: %v", schema.Required)
	}
}

func buildExifAPP1(orientation int) []byte {
	payload := []byte("Exif\x00\x00")
	payload = append(payload,
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00,
		byte(orientation), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	)
	size := len(payload) + 2
	head := []byte{0xFF, 0xE1, byte(size >> 8), byte(size)}
	return append(head, payload...)
}

func orientedJPEG(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	var base bytes.Buffer
	if err := jpeg.Encode(&base, img, nil); err != nil {
		t.Fatal(err)
	}
	out := append([]byte{0xFF, 0xD8}, buildExifAPP1(orientation)...)
	return append(out, base.Bytes()[2:]...)
}

func headerOnlyPNG(w, h int) []byte {
	var out []byte
	out = append(out, 0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n')
	be32 := func(v uint32) []byte {
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	}
	chunk := func(typ string, data []byte) {
		body := append([]byte(typ), data...)
		out = append(out, be32(uint32(len(data)))...)
		out = append(out, body...)
		out = append(out, be32(crc32.ChecksumIEEE(body))...)
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(h))
	ihdr[8] = 8
	ihdr[9] = 2
	chunk("IHDR", ihdr)
	chunk("IEND", nil)
	return out
}

func TestReadImageRegionOrientedJPEG(t *testing.T) {
	dir := t.TempDir()
	raw := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			if y >= 10 {
				raw.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
			} else {
				raw.SetRGBA(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	writeFile(t, dir, "cam.jpg", orientedJPEG(t, raw, 6))
	tool := New(Config{Workspace: func() string { return dir }, Detail: "high"})

	res := tool.Invoke(context.Background(), `{"path":"cam.jpg"}`)
	if !strings.Contains(res.Text, "已读取 cam.jpg（20×40，") {
		t.Errorf("整图读取应报告显示尺寸（宽高交换）: %q", res.Text)
	}

	res = tool.Invoke(context.Background(), `{"path":"cam.jpg","region":{"x":0,"y":0,"width":10,"height":40}}`)
	if len(res.Images) != 1 {
		t.Fatalf("应返回一张图: %+v", res)
	}
	ref := res.Images[0]
	if ref.Width != 10 || ref.Height != 40 {
		t.Fatalf("区域输出尺寸: %d×%d", ref.Width, ref.Height)
	}
	if !strings.Contains(res.Text, "原图 20×40") || !strings.Contains(res.Text, "区域 (0,0) 10×40") || !strings.Contains(res.Text, "输出 10×40") {
		t.Errorf("区域文本异常: %q", res.Text)
	}
	data, err := base64.StdEncoding.DecodeString(ref.Data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := img.At(5, 20).RGBA(); r < 200 {
		t.Errorf("方向 6 显示帧左半应为红: %d", r)
	}

	res = tool.Invoke(context.Background(), `{"path":"cam.jpg","region":{"x":10,"y":0,"width":10,"height":40}}`)
	if len(res.Images) != 1 {
		t.Fatalf("应返回一张图: %+v", res)
	}
	if data, err = base64.StdEncoding.DecodeString(res.Images[0].Data); err != nil {
		t.Fatal(err)
	}
	if img, err = jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, _, b, _ := img.At(5, 20).RGBA(); b < 200 {
		t.Errorf("方向 6 显示帧右半应为蓝: %d", b)
	}
}
