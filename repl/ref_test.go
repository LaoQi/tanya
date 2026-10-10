package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRefTokens(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"@a.png 看图", []string{"a.png"}},
		{"看图 @a.png", []string{"a.png"}},
		{"看图 @a.png @b.png", []string{"a.png", "b.png"}},
		{"a@b.com 不是引用", nil},
		{`看图 @"my shot.png" 结束`, []string{"my shot.png"}},
		{`@"未闭合`, nil},
		{"@", nil},
		{"没有引用", nil},
	}
	for _, c := range cases {
		got := refTokens(c.line, RefOptions{})
		if len(got) != len(c.want) {
			t.Errorf("refTokens(%q) = %v want %v", c.line, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("refTokens(%q) = %v want %v", c.line, got, c.want)
				break
			}
		}
	}
}

func TestParseRefsSkipsMissing(t *testing.T) {
	refs := ParseRefs("看图 @"+filepath.Join(t.TempDir(), "nope.png")+" 结束", RefOptions{})
	if len(refs) != 0 {
		t.Errorf("不存在的路径应静默: %+v", refs)
	}
}

func TestParseRefsLocalFile(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("hello world")
	path := writeFile(t, filepath.Join(dir, "note.txt"), raw)
	refs := ParseRefs("看一下 @"+path+" 还有引号 @"+path, RefOptions{})
	if len(refs) != 2 {
		t.Fatalf("应解析两条引用: %+v", refs)
	}
	if refs[0].Path != path || refs[0].Name != "note.txt" || refs[0].Size != int64(len(raw)) {
		t.Errorf("引用元信息: %+v", refs[0])
	}
}

func TestParseRefsAcceptsAnyRegularFile(t *testing.T) {
	dir := t.TempDir()
	png := writeFile(t, filepath.Join(dir, "shot.png"), []byte("not really a png"))
	txt := writeFile(t, filepath.Join(dir, "note.txt"), []byte("x"))
	if got := ParseRefs("@"+png, RefOptions{}); len(got) != 1 {
		t.Errorf("图像文件: %+v", got)
	}
	if got := ParseRefs("@"+txt, RefOptions{}); len(got) != 1 {
		t.Errorf("非图像文件同样是引用: %+v", got)
	}
	if got := ParseRefs("@"+dir, RefOptions{}); len(got) != 0 {
		t.Errorf("目录不是引用: %+v", got)
	}
	missing := filepath.Join(dir, "gone.txt")
	if got := ParseRefs("@"+missing, RefOptions{}); len(got) != 0 {
		t.Errorf("缺失文件应静默: %+v", got)
	}
}

func TestParseRefsDropsURLAndDataURL(t *testing.T) {
	if got := ParseRefs("看图 @https://example.com/pic.jpg?x=1", RefOptions{}); len(got) != 0 {
		t.Errorf("外链已不再支持，应按普通文本处理: %+v", got)
	}
	if got := ParseRefs("@data:image/png;base64,AAAA 看图", RefOptions{}); len(got) != 0 {
		t.Errorf("data URL 已不再支持: %+v", got)
	}
}

func TestParseRefsRespectsWorkspaceAndTilde(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), []byte("x"))
	refs := ParseRefs("@a.txt", RefOptions{Workspace: func() string { return dir }})
	if len(refs) != 1 || refs[0].Path != filepath.Join(dir, "a.txt") {
		t.Fatalf("相对路径应按工作区解析: %+v", refs)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无家目录")
	}
	if got := ParseRefs("@~/tanya-ref-missing.txt", RefOptions{}); len(got) != 0 {
		t.Errorf("~ 展开后不存在应静默: %+v", got)
	}
}

func TestRefsText(t *testing.T) {
	got := refsText([]Ref{{Name: "a.png", Size: 1234}, {Name: "b.txt", Size: 2 << 20}})
	if !strings.HasPrefix(got, "[引用 ") || !strings.Contains(got, "a.png 1.2k") || !strings.Contains(got, "b.txt 2.0M") {
		t.Errorf("引用回显: %q", got)
	}
	if refsText(nil) != "" {
		t.Error("无引用应为空串")
	}
}

func TestRefCompletion(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shots"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.png"), []byte("x"))
	writeFile(t, filepath.Join(dir, "note.txt"), []byte("x"))
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
	if got := c.suggest("看图 @n"); got != "ote.txt" {
		t.Errorf("非图像文件同样进候选: %q", got)
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

func TestRefCompletionSpaceNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "my dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{"a.png", "my file.png", "myfile.png", `we"ird.png`, `we'ird.png`}
	files = append(files, filepath.Join("my dir", "inner.png"))
	for _, n := range files {
		writeFile(t, filepath.Join(dir, n), []byte("x"))
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

func TestRefPathLongestMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "my dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"plain.png", "note.png 结束", filepath.Join("my dir", "inner.png")} {
		writeFile(t, filepath.Join(dir, n), []byte("x"))
	}
	writeFile(t, filepath.Join(dir, "my file.png"), []byte("x"))
	writeFile(t, filepath.Join(dir, "plain.png @my file.png"), []byte("x"))
	writeFile(t, filepath.Join(dir, "note.png"), []byte("x"))
	writeFile(t, filepath.Join(dir, "my note.txt"), []byte("x"))
	opt := RefOptions{Workspace: func() string { return dir }}

	eq := func(line string, want ...string) {
		t.Helper()
		got := refTokens(line, opt)
		if len(got) != len(want) {
			t.Errorf("refTokens(%q) = %v want %v", line, got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("refTokens(%q) = %v want %v", line, got, want)
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

	refs := ParseRefs("@my file.png 说明", opt)
	if len(refs) != 1 || refs[0].Name != "my file.png" {
		t.Fatalf("含空格路径应解析成功: %+v", refs)
	}
	if refs := ParseRefs("@my note.txt 看图", opt); len(refs) != 1 || refs[0].Name != "my note.txt" {
		t.Errorf("非图像长名同样按最长存在路径匹配: %+v", refs)
	}
}
