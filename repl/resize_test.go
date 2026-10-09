package repl

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/LaoQi/tanya/agent"
)

func bigPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rnd := rand.New(rand.NewSource(7))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = byte(rnd.Intn(256))
		img.Pix[i+1] = byte(rnd.Intn(256))
		img.Pix[i+2] = byte(rnd.Intn(256))
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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

func jpegWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	base := buf.Bytes()
	out := append([]byte{0xFF, 0xD8}, buildExifAPP1(orientation)...)
	return append(out, base[2:]...)
}

func TestMaybeResizeImageScalesToTarget(t *testing.T) {
	data := bigPNG(t, 1400, 700)
	got := MaybeResizeImage(data, "image/png", "", true)
	if len(got) >= len(data) {
		t.Errorf("应缩小体积: %d → %d", len(data), len(got))
	}
	w, h := ImageDimensions(got, "image/png")
	if w != ImageResizeSide || h != ImageResizeSide/2 {
		t.Errorf("目标长边与保比: %d×%d", w, h)
	}

	low := MaybeResizeImage(data, "image/png", "low", true)
	w, h = ImageDimensions(low, "image/png")
	if w != ImageResizeLowSide || h != ImageResizeLowSide/2 {
		t.Errorf("low 档长边 512: %d×%d", w, h)
	}
}

func TestMaybeResizeImageSkipsSmallAndUnsupported(t *testing.T) {
	small := bigPNG(t, 100, 80)
	if got := MaybeResizeImage(small, "image/png", "", true); !bytes.Equal(got, small) {
		t.Error("小于目标不应缩放")
	}
	gif := []byte("GIF89a")
	if got := MaybeResizeImage(gif, "image/gif", "low", true); !bytes.Equal(got, gif) {
		t.Error("GIF 不应缩放")
	}
	webp := []byte("RIFF....WEBPVP8 ")
	if got := MaybeResizeImage(webp, "image/webp", "low", true); !bytes.Equal(got, webp) {
		t.Error("WebP 不应缩放")
	}
	data := bigPNG(t, 1400, 700)
	if got := MaybeResizeImage(data, "image/png", "low", false); !bytes.Equal(got, data) {
		t.Error("关闭开关应原样")
	}
}

func TestMaybeResizeImageSkipsRotatedJPEG(t *testing.T) {
	rotated := jpegWithOrientation(t, 1600, 1000, 6)
	if got := jpegOrientation(rotated); got != 6 {
		t.Fatalf("应解析出方向 6: %d", got)
	}
	if got := MaybeResizeImage(rotated, "image/jpeg", "low", true); !bytes.Equal(got, rotated) {
		t.Error("带 EXIF 方向的 JPEG 应跳过缩放")
	}
	upright := jpegWithOrientation(t, 1600, 1000, 1)
	if got := jpegOrientation(upright); got != 1 {
		t.Fatalf("方向应为 1: %d", got)
	}
	resized := MaybeResizeImage(upright, "image/jpeg", "low", true)
	if len(resized) >= len(upright) {
		t.Errorf("正立 JPEG 应缩放: %d → %d", len(upright), len(resized))
	}
	w, h := ImageDimensions(resized, "image/jpeg")
	if w != ImageResizeLowSide || h != ImageResizeLowSide*5/8 {
		t.Errorf("JPEG 缩放尺寸: %d×%d", w, h)
	}
}

func TestJpegOrientationOnPlainJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil); err != nil {
		t.Fatal(err)
	}
	if got := jpegOrientation(buf.Bytes()); got != 1 {
		t.Errorf("无 EXIF 应为 1: %d", got)
	}
	if got := jpegOrientation([]byte("notjpeg")); got != 1 {
		t.Errorf("非 JPEG 应为 1: %d", got)
	}
}

func TestParseAttachmentsAppliesResize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	raw := bigPNG(t, 1400, 700)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	imgs, err := ParseAttachments("@"+path, AttachOptions{Resize: true, Detail: "low"})
	if err != nil {
		t.Fatal(err)
	}
	img := imgs[0]
	if img.Width != ImageResizeLowSide || img.Height != ImageResizeLowSide/2 {
		t.Errorf("元信息应为缩放后尺寸: %d×%d", img.Width, img.Height)
	}
	if img.Bytes >= len(raw) {
		t.Errorf("字节数应为实际发送量: %d (原 %d)", img.Bytes, len(raw))
	}
	if want := len(img.Data) * 3 / 4; img.Bytes > want+4 {
		t.Errorf("base64 长度应与字节数一致: data=%d bytes=%d", len(img.Data), img.Bytes)
	}
	plain, err := ParseAttachments("@"+path, AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plain[0].Width != 1400 {
		t.Errorf("关闭缩放应保留原尺寸: %d", plain[0].Width)
	}
}

func TestParseAttachmentsResizesDataURL(t *testing.T) {
	raw := bigPNG(t, 1400, 1400)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	imgs, err := ParseAttachments("@"+url, AttachOptions{Resize: true, Detail: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if imgs[0].Width != ImageResizeLowSide || imgs[0].Bytes >= len(raw) {
		t.Errorf("data URL 应同样缩放: %+v", imgs[0])
	}
	if _, err := base64.StdEncoding.DecodeString(imgs[0].Data); err != nil {
		t.Errorf("缩放后应为合法 base64: %v", err)
	}
}

func TestResizeTargetSide(t *testing.T) {
	cases := map[string]int{"": ImageResizeSide, "low": ImageResizeLowSide, "high": ImageResizeSide, "original": ImageResizeSide, "bogus": ImageResizeSide}
	for detail, want := range cases {
		if got := ResizeTargetSide(detail); got != want {
			t.Errorf("ResizeTargetSide(%q) = %d want %d", detail, got, want)
		}
	}
	if got := (agent.ImageRef{Width: 2000, Height: 1000}).Width; got != 2000 {
		t.Errorf("占位断言: %d", got)
	}
}
