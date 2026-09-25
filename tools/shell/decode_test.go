package shell

import (
	"bytes"
	"strings"
	"testing"
)

func TestToUTF8Passthrough(t *testing.T) {
	for _, in := range []string{"", "ascii", "中文测试", "emoji 🎉 mixed"} {
		if got := toUTF8([]byte(in)); got != in {
			t.Errorf("合法 UTF-8 应原样直通: in=%q got=%q", in, got)
		}
	}
	if got := toUTF8(nil); got != "" {
		t.Errorf("空输入应返回空串: %q", got)
	}
}

func TestToUTF8InvalidRoutesToDecoder(t *testing.T) {
	orig := decodeStream
	t.Cleanup(func() { decodeStream = orig })
	decodeStream = func(b []byte) string { return "DECODED<" + string(b) + ">" }

	gbk := []byte{0xC4, 0xE3, 0xBA, 0xC3}
	got := toUTF8(gbk)
	if !strings.HasPrefix(got, "DECODED<") || !strings.HasSuffix(got, ">") {
		t.Errorf("非法 UTF-8 应路由到注入的解码器: %q", got)
	}
	if !bytes.Contains([]byte(got), gbk) {
		t.Errorf("解码器应收到原始字节: %q", got)
	}
}

func TestFinishJoinsContiguousSeam(t *testing.T) {
	orig := decodeStream
	t.Cleanup(func() { decodeStream = orig })
	var calls [][]byte
	decodeStream = func(b []byte) string { calls = append(calls, b); return "[" + string(b) + "]" }

	var chunks []Chunk
	c := streamCapture{chunks: &chunks}
	pad := strings.Repeat("a", MaxOutput-2)
	c.Write([]byte(pad))
	c.Write([]byte{0xC4, 0xE3})
	c.Write([]byte{0xC4, 0xE3})
	c.Write([]byte("b"))
	c.finish()

	if len(chunks) != 1 {
		t.Fatalf("连续流应合并为单一 chunk: %+v", chunks)
	}
	if len(calls) != 1 {
		t.Fatalf("middle==0 时应整体解码一次（跨 head/tail 边界的多字节序列不能被切断）: calls=%d", len(calls))
	}
	if len(calls[0]) != MaxOutput+3 {
		t.Errorf("解码器应收到完整拼接缓冲: len=%d", len(calls[0]))
	}
	if !strings.HasPrefix(chunks[0].Data, "[") || !strings.HasSuffix(chunks[0].Data, "]") {
		t.Errorf("chunk 应为解码器输出: %q…", chunks[0].Data[:16])
	}
	if !strings.HasPrefix(chunks[0].Data[1:], pad) || !strings.HasSuffix(chunks[0].Data, "b]") {
		t.Errorf("拼接内容错位: head=%q tail=%q", chunks[0].Data[1:16], chunks[0].Data[len(chunks[0].Data)-8:])
	}
	if chunks[0].Truncated != 0 {
		t.Errorf("连续流不应有截断计数: %d", chunks[0].Truncated)
	}
}

func TestFinishDecodesDisjointFragments(t *testing.T) {
	orig := decodeStream
	t.Cleanup(func() { decodeStream = orig })
	var calls [][]byte
	decodeStream = func(b []byte) string { calls = append(calls, b); return "[" + string(b) + "]" }

	var chunks []Chunk
	c := streamCapture{chunks: &chunks}
	invalid := strings.Repeat("\x81", MaxOutput*2+1)
	c.Write([]byte(invalid))
	c.finish()

	if len(chunks) != 2 {
		t.Fatalf("超限流应产出 2 chunk: %+v", chunks)
	}
	if len(calls) != 2 {
		t.Fatalf("不连续片段应各自解码: calls=%d", len(calls))
	}
	if len(calls[0]) != MaxOutput || len(calls[1]) != MaxOutput/2+1 {
		t.Errorf("解码输入长度异常: head=%d tail=%d", len(calls[0]), len(calls[1]))
	}
	if chunks[1].Truncated != MaxOutput/2 {
		t.Errorf("截断计数应保留: %d", chunks[1].Truncated)
	}
	for i, ch := range chunks {
		if !strings.HasPrefix(ch.Data, "[") || !strings.HasSuffix(ch.Data, "]") {
			t.Errorf("chunk[%d] 应为解码器输出", i)
		}
	}
}
