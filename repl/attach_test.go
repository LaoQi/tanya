package repl

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
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
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAttachTokens(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"@a.png 看图", []string{"a.png"}},
		{"看图 @a.png", []string{"a.png"}},
		{"看图 @a.png @b.png", []string{"a.png", "b.png"}},
		{"a@b.com 不是附件", nil},
		{`看图 @"my shot.png" 结束`, []string{"my shot.png"}},
		{`@"未闭合`, nil},
		{"@", nil},
		{"没有附件", nil},
	}
	for _, c := range cases {
		got := attachTokens(c.line, AttachOptions{})
		if len(got) != len(c.want) {
			t.Errorf("attachTokens(%q) = %v want %v", c.line, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("attachTokens(%q) = %v want %v", c.line, got, c.want)
				break
			}
		}
	}
}

func TestParseAttachmentsSilentOnMissing(t *testing.T) {
	imgs, err := ParseAttachments("看图 @"+filepath.Join(t.TempDir(), "nope.png"), AttachOptions{})
	if err != nil {
		t.Fatalf("不存在的路径应静默: %v", err)
	}
	if len(imgs) != 0 {
		t.Errorf("不应产生附件: %+v", imgs)
	}
}

func TestParseAttachmentsRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAttachments("看图 @"+path, AttachOptions{}); err == nil {
		t.Error("存在的非图像文件应报错")
	}
}

func TestParseAttachmentsLocalImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, pngBytes(t, 4, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	imgs, err := ParseAttachments("看图 @"+path+" 还有引号 @"+path, AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("应解析两张: %+v", imgs)
	}
	img := imgs[0]
	if img.MIME != "image/png" || img.Name != "shot.png" || img.Width != 4 || img.Height != 3 {
		t.Errorf("图像元信息: %+v", img)
	}
	if img.Data == "" || img.Bytes != len(pngBytes(t, 4, 3)) {
		t.Errorf("应内联 base64 与原始字节数: %+v", img)
	}
}

func TestParseAttachmentsRespectsWorkspaceAndTilde(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), pngBytes(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	imgs, err := ParseAttachments("@a.png", AttachOptions{Workspace: func() string { return dir }})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 {
		t.Fatalf("相对路径应按工作区解析: %+v", imgs)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无家目录")
	}
	if _, err := ParseAttachments("@~/tanya-attach-missing.png", AttachOptions{}); err != nil {
		t.Errorf("~ 展开后不存在应静默: %v", err)
	}
}

func TestParseAttachmentsLimits(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, pngBytes(t, 64, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAttachments("@"+big, AttachOptions{MaxBytes: 16}); err == nil {
		t.Error("超过大小上限应报错")
	}
	if _, err := ParseAttachments("@"+big+" @"+big, AttachOptions{MaxCount: 1}); err == nil {
		t.Error("超过张数上限应报错")
	}
}

func TestParseAttachmentsURL(t *testing.T) {
	imgs, err := ParseAttachments("看图 @https://example.com/pic.jpg?x=1", AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 || imgs[0].URL != "https://example.com/pic.jpg?x=1" || imgs[0].Name != "pic.jpg" {
		t.Fatalf("外链应原样保留: %+v", imgs)
	}
	long := "https://example.com/" + strings.Repeat("a", MaxImageURLLen)
	if _, err := ParseAttachments("@"+long, AttachOptions{}); err == nil {
		t.Error("超长外链应报错")
	}
}

func TestParseAttachmentsDataURL(t *testing.T) {
	raw := pngBytes(t, 5, 6)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	imgs, err := ParseAttachments("@"+url, AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 || imgs[0].MIME != "image/png" || imgs[0].Width != 5 || imgs[0].Height != 6 {
		t.Fatalf("data URL 应解析出尺寸: %+v", imgs)
	}
	if _, err := ParseAttachments("@data:image/png;base64,not-base64!!", AttachOptions{}); err == nil {
		t.Error("非法 base64 应报错")
	}
	if _, err := ParseAttachments("@data:text/plain;base64,aGk=", AttachOptions{}); err == nil {
		t.Error("非图像 data URL 应报错")
	}
	if _, err := ParseAttachments("@data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("plain")), AttachOptions{}); err == nil {
		t.Error("内容非图像应报错")
	}
}

func TestSniffImageMIME(t *testing.T) {
	if got := SniffImageMIME(pngBytes(t, 1, 1)); got != "image/png" {
		t.Errorf("png: %q", got)
	}
	if got := SniffImageMIME([]byte("not an image")); got != "" {
		t.Errorf("非图像: %q", got)
	}
	if got := normImageMIME("image/jpg"); got != "image/jpeg" {
		t.Errorf("jpg 归一化: %q", got)
	}
}

func TestWebpDimensions(t *testing.T) {
	extended := make([]byte, 30)
	copy(extended[0:], "RIFF")
	copy(extended[8:], "WEBP")
	copy(extended[12:], "VP8X")
	extended[24], extended[25], extended[26] = 0x2f, 0x00, 0x00
	extended[27], extended[28], extended[29] = 0x0f, 0x00, 0x00
	if w, h := webpDimensions(extended); w != 48 || h != 16 {
		t.Errorf("VP8X: %d×%d", w, h)
	}

	lossless := make([]byte, 25)
	copy(lossless[0:], "RIFF")
	copy(lossless[8:], "WEBP")
	copy(lossless[12:], "VP8L")
	lossless[20] = 0x2f
	b := uint32(9) | uint32(19)<<14
	lossless[21], lossless[22], lossless[23], lossless[24] = byte(b), byte(b>>8), byte(b>>16), byte(b>>24)
	if w, h := webpDimensions(lossless); w != 10 || h != 20 {
		t.Errorf("VP8L: %d×%d", w, h)
	}

	lossy := make([]byte, 30)
	copy(lossy[0:], "RIFF")
	copy(lossy[8:], "WEBP")
	copy(lossy[12:], "VP8 ")
	lossy[20], lossy[21], lossy[22] = 0x9d, 0x01, 0x2a
	lossy[23], lossy[24] = 0x1f, 0x00
	lossy[25], lossy[26] = 0x0f, 0x00
	if w, h := webpDimensions(lossy); w != 31 || h != 15 {
		t.Errorf("VP8: %d×%d", w, h)
	}

	if w, h := webpDimensions([]byte("short")); w != 0 || h != 0 {
		t.Errorf("短数据应返回 0: %d×%d", w, h)
	}
	if w, h := ImageDimensions(pngBytes(t, 7, 9), "image/png"); w != 7 || h != 9 {
		t.Errorf("png 尺寸: %d×%d", w, h)
	}
}

func TestPrepareContentKeepsText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, pngBytes(t, 3, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	line := "看一下 @" + path
	in, err := PrepareContent(line, AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if in.Text != line {
		t.Errorf("文本应原样保留: %q", in.Text)
	}
	if len(in.Images) != 1 {
		t.Fatalf("应带 1 张图: %+v", in.Images)
	}
	plain, err := PrepareContent("普通文本", AttachOptions{})
	if err != nil || len(plain.Images) != 0 || plain.Text != "普通文本" {
		t.Errorf("无附件应原样透传: %+v %v", plain, err)
	}
}

func TestPrepareContentRejectsImageOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, pngBytes(t, 3, 3), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareContent("@"+path, AttachOptions{}); err == nil {
		t.Error("纯图消息应报错")
	}
	if _, err := PrepareContent("@"+path+"   ", AttachOptions{}); err == nil {
		t.Error("纯图加空白仍应报错")
	}
}

func TestStripAttachTokensAndImagesText(t *testing.T) {
	if got := strings.TrimSpace(stripAttachTokens("看图 @a.png 结束", AttachOptions{})); got != "看图  结束" {
		t.Errorf("剥离后: %q", got)
	}
	if got := strings.TrimSpace(stripAttachTokens(`看图 @"a b.png"`, AttachOptions{})); got != "看图" {
		t.Errorf("引号剥离后: %q", got)
	}
	text := imagesText([]agent.ImageRef{{Name: "a.png", Bytes: 1234}, {Name: "b.jpg", Bytes: 2 << 20}})
	if !strings.Contains(text, "[图 ") || !strings.Contains(text, "a.png 1.2k") || !strings.Contains(text, "b.jpg 2.0M") {
		t.Errorf("占位文本: %q", text)
	}
	if imagesText(nil) != "" {
		t.Error("无图应为空串")
	}
}

func TestHistoryTextWithImages(t *testing.T) {
	m := agent.Message{Role: "user", Content: "看图", Images: []agent.ImageRef{{Name: "a.png", Bytes: 100}}}
	if got := historyText(m); !strings.HasPrefix(got, "[图 ") || !strings.HasSuffix(got, "看图") {
		t.Errorf("历史文本应带图像占位: %q", got)
	}
	only := agent.Message{Role: "user", Images: []agent.ImageRef{{Name: "a.png", Bytes: 100}}}
	if got := historyText(only); !strings.HasPrefix(got, "[图 ") || strings.Contains(got, "看图") {
		t.Errorf("纯图历史文本: %q", got)
	}
}

func TestAttachCompletion(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &completer{workspaceDir: func() string { return dir }}
	if got := c.suggest("看图 @a"); got == "" {
		t.Error("ghost 应补路径后缀")
	}
	cands := c.complete("看图 @")
	if len(cands) == 0 {
		t.Fatal("Tab 应有候选")
	}
	found := false
	for _, cand := range cands {
		if strings.HasSuffix(cand.Insert, "a.png") && strings.HasPrefix(cand.Insert, "看图 @") {
			found = true
		}
	}
	if !found {
		t.Errorf("候选应保留行首文本: %+v", cands)
	}
	if cands := c.complete("看图 @\""); len(cands) == 0 {
		t.Error("引号形态也应有候选")
	} else if cands[0].Insert != "看图 @a.png" {
		t.Errorf("引号形态应统一补成非引号形态: %+v", cands[:1])
	}
	if got := c.complete("a@b.com"); len(got) != 0 {
		t.Errorf("邮箱不应触发补全: %+v", got)
	}
	if got := c.suggest("/load @"); got != "" && strings.HasSuffix(got, ".png") {
		t.Errorf("斜杠命令行不应走 @ 补全: %q", got)
	}
}

func TestAttachCompletionSpaceNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "my dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{"a.png", "my file.png", "myfile.png", `we"ird.png`, `we'ird.png`}
	files = append(files, filepath.Join("my dir", "inner.png"))
	for _, n := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &completer{workspaceDir: func() string { return dir }}

	inserts := func(line string) []string {
		var out []string
		for _, cand := range c.complete(line) {
			out = append(out, cand.Insert)
		}
		return out
	}
	has := func(line, want string) bool {
		for _, got := range inserts(line) {
			if got == want {
				return true
			}
		}
		return false
	}

	if got := c.suggest("@a"); got != ".png" {
		t.Errorf("行首 token 的 ghost: %q", got)
	}
	if !has("@a", "@a.png") {
		t.Errorf("行首 token 的候选: %v", inserts("@a"))
	}
	if !has("@my", "@my file.png") {
		t.Errorf("含空格名应原样进候选（无引号、无闭合）: %v", inserts("@my"))
	}
	if !has("@my", "@my dir/") {
		t.Errorf("含空格目录应原样进候选: %v", inserts("@my"))
	}
	if got := c.suggest("@my fi"); got != "le.png" {
		t.Errorf("含空格候选的 ghost 应直接可用: %q", got)
	}
	if !has(`@"my fi`, "@my file.png") {
		t.Errorf("引号形态应统一补成非引号形态: %v", inserts(`@"my fi`))
	}
	if got := c.suggest(`@"my fi`); got != "" {
		t.Errorf("引号 token 不给 ghost（ghost 无法去掉已输入的引号）: %q", got)
	}
	if !has(`@"my dir/in`, "@my dir/inner.png") {
		t.Errorf("引号形态下钻: %v", inserts(`@"my dir/in`))
	}
	if !has("@we", `@we"ird.png`) || !has("@we", `@we'ird.png`) {
		t.Errorf("名字含引号字符（非首位）应可补: %v", inserts("@we"))
	}
	if got := inserts("a@b"); len(got) != 0 {
		t.Errorf("邮箱不应触发: %v", got)
	}
}

func TestAttachPathLongestMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "my dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"plain.png", "note.png 结束", filepath.Join("my dir", "inner.png")} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "my file.png"), pngBytes(t, 1, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.png @my file.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := AttachOptions{Workspace: func() string { return dir }}

	tokens := func(line string) []string {
		return attachTokens(line, opt)
	}
	eq := func(line string, want ...string) {
		t.Helper()
		got := tokens(line)
		if len(got) != len(want) {
			t.Errorf("attachTokens(%q) = %v want %v", line, got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("attachTokens(%q) = %v want %v", line, got, want)
				return
			}
		}
	}

	eq("看图 @my file.png 这张图里有什么", "my file.png")
	eq("@my file.png 说明", "my file.png")
	eq("@my dir/inner.png 看图", "my dir/inner.png")
	eq("@plain.png @my file.png 说明", "plain.png", "my file.png")
	eq("@plain.png @my file.png", "plain.png", "my file.png")
	eq("@note.png 结束", "note.png 结束")
	eq("@my dir 看图", "my")
	eq("@nope file.png 看图", "nope")
	eq("@https://example.com/a.png 看图", "https://example.com/a.png")
	eq("@data:image/png;base64,AAAA 看图", "data:image/png;base64,AAAA")
	eq(`@"my file.png`)
	eq("@ 看图")

	if _, err := ParseAttachments("@my note.txt 看图", opt); err == nil {
		t.Error("存在的非图像长名应按校验不过报错，而非静默")
	}
	if got := strings.TrimSpace(stripAttachTokens("看图 @my file.png 这张图", opt)); got != "看图  这张图" {
		t.Errorf("stripAttachTokens 应只剥附件: %q", got)
	}
	imgs, err := ParseAttachments("@my file.png 说明", opt)
	if err != nil {
		t.Fatalf("含空格路径应解析成功: %v", err)
	}
	if len(imgs) != 1 || imgs[0].Name != "my file.png" {
		t.Fatalf("解析结果: %+v", imgs)
	}
}
