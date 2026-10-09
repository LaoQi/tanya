package agent

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func dataPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func coordImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 7, A: 255})
		}
	}
	return img
}

func coordPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	return dataPNG(t, coordImage(w, h))
}

func tinyGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func dataJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
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

func orientedJPEG(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	base := dataJPEG(t, img)
	out := append([]byte{0xFF, 0xD8}, buildExifAPP1(orientation)...)
	return append(out, base[2:]...)
}

func TestImageDimensionsOrientationSwap(t *testing.T) {
	if w, h := ImageDimensions(coordPNG(t, 30, 20), "image/png"); w != 30 || h != 20 {
		t.Errorf("png 尺寸: %d×%d", w, h)
	}
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	if w, h := ImageDimensions(dataJPEG(t, img), "image/jpeg"); w != 40 || h != 20 {
		t.Errorf("方向 1 尺寸: %d×%d", w, h)
	}
	if w, h := ImageDimensions(orientedJPEG(t, img, 2), "image/jpeg"); w != 40 || h != 20 {
		t.Errorf("方向 2 不应交换: %d×%d", w, h)
	}
	if w, h := ImageDimensions(orientedJPEG(t, img, 5), "image/jpeg"); w != 20 || h != 40 {
		t.Errorf("方向 5 应交换: %d×%d", w, h)
	}
	if w, h := ImageDimensions(orientedJPEG(t, img, 6), "image/jpeg"); w != 20 || h != 40 {
		t.Errorf("方向 6 应交换: %d×%d", w, h)
	}
	if w, h := ImageDimensions(orientedJPEG(t, img, 8), "image/jpeg"); w != 20 || h != 40 {
		t.Errorf("方向 8 应交换: %d×%d", w, h)
	}
}

func TestOrientImageMapping(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	src.SetRGBA(1, 0, color.RGBA{G: 255, A: 255})
	src.SetRGBA(0, 1, color.RGBA{B: 255, A: 255})
	src.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, A: 255})
	grid := func(img image.Image) string {
		var sb strings.Builder
		for y := 0; y < 2; y++ {
			if y > 0 {
				sb.WriteByte('/')
			}
			for x := 0; x < 2; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				switch {
				case r > 0 && g == 0:
					sb.WriteByte('R')
				case g > 0 && r == 0:
					sb.WriteByte('G')
				case b > 0:
					sb.WriteByte('B')
				default:
					sb.WriteByte('Y')
				}
			}
		}
		return sb.String()
	}
	cases := map[int]string{
		1: "RG/BY",
		2: "GR/YB",
		3: "YB/GR",
		4: "BY/RG",
		5: "RB/GY",
		6: "BR/YG",
		7: "YG/BR",
		8: "GY/RB",
	}
	for o, want := range cases {
		if got := grid(orientImage(src, o)); got != want {
			t.Errorf("o=%d 全格: %q want %q", o, got, want)
		}
	}
	if got := orientImage(src, 1); got != image.Image(src) {
		t.Errorf("o=1 应原样返回: %v", got)
	}
}

func TestSourceWindowAllOrientations(t *testing.T) {
	src := coordImage(40, 20)
	rects := [][4]int{
		{5, 7, 10, 5},
		{0, 0, 20, 20},
		{10, 12, 9, 7},
	}
	for o := 1; o <= 8; o++ {
		full := orientImage(src, o)
		for _, rc := range rects {
			x0, y0, x1, y1 := rc[0], rc[1], rc[0]+rc[2], rc[1]+rc[3]
			var got image.Image = sourceWindow(src, o, x0, y0, x1, y1)
			if o > 1 {
				got = orientImage(got, o)
			}
			if got.Bounds().Dx() != rc[2] || got.Bounds().Dy() != rc[3] {
				t.Fatalf("o=%d 区域 %v 尺寸: %v", o, rc, got.Bounds())
			}
			for y := 0; y < rc[3]; y++ {
				for x := 0; x < rc[2]; x++ {
					if got.At(x, y) != full.At(x0+x, y0+y) {
						t.Errorf("o=%d 区域 %v (%d,%d): %v want %v", o, rc, x, y, got.At(x, y), full.At(x0+x, y0+y))
					}
				}
			}
		}
	}
}

func TestCropImagePNGExact(t *testing.T) {
	src := coordPNG(t, 100, 80)
	out, used, err := CropImage(src, "image/png", ImageRegion{X: 10, Y: 20, Width: 30, Height: 40}, "high", false)
	if err != nil {
		t.Fatalf("裁剪失败: %v", err)
	}
	if used != (ImageRegion{X: 10, Y: 20, Width: 30, Height: 40}) {
		t.Errorf("实际区域: %+v", used)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("输出应合法 PNG: %v", err)
	}
	if img.Bounds().Dx() != 30 || img.Bounds().Dy() != 40 {
		t.Fatalf("输出尺寸: %v", img.Bounds())
	}
	want00 := color.RGBA{R: 10, G: 20, B: 7, A: 255}
	if got := img.At(0, 0); got != want00 {
		t.Errorf("输出(0,0): %v", got)
	}
	wantEdge := color.RGBA{R: 39, G: 59, B: 7, A: 255}
	if got := img.At(29, 39); got != wantEdge {
		t.Errorf("输出(29,39): %v", got)
	}
}

func TestCropImageClamp(t *testing.T) {
	src := coordPNG(t, 100, 80)
	cases := []struct {
		name string
		r    ImageRegion
		want ImageRegion
	}{
		{"右下越界", ImageRegion{X: 95, Y: 75, Width: 30, Height: 40}, ImageRegion{X: 95, Y: 75, Width: 5, Height: 5}},
		{"负起点", ImageRegion{X: -10, Y: -5, Width: 30, Height: 20}, ImageRegion{X: 0, Y: 0, Width: 20, Height: 15}},
		{"超大区域", ImageRegion{X: 50, Y: 40, Width: 999, Height: 999}, ImageRegion{X: 50, Y: 40, Width: 50, Height: 40}},
	}
	for _, c := range cases {
		out, used, err := CropImage(src, "image/png", c.r, "high", false)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if used != c.want {
			t.Errorf("%s: 实际区域 %+v want %+v", c.name, used, c.want)
		}
		img, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if img.Bounds().Dx() != c.want.Width || img.Bounds().Dy() != c.want.Height {
			t.Errorf("%s: 输出尺寸 %v want %d×%d", c.name, img.Bounds(), c.want.Width, c.want.Height)
		}
	}
}

func TestCropImageEmpty(t *testing.T) {
	src := coordPNG(t, 100, 80)
	cases := []struct {
		name string
		r    ImageRegion
	}{
		{"完全在外", ImageRegion{X: 150, Y: 150, Width: 10, Height: 10}},
		{"贴右边界外", ImageRegion{X: 100, Y: 0, Width: 10, Height: 10}},
		{"贴下边界外", ImageRegion{X: 0, Y: 80, Width: 10, Height: 10}},
	}
	for _, c := range cases {
		if _, _, err := CropImage(src, "image/png", c.r, "high", false); err != ErrImageRegionEmpty {
			t.Errorf("%s: err=%v want ErrImageRegionEmpty", c.name, err)
		}
	}
}

func TestCropImageUnsupported(t *testing.T) {
	if _, _, err := CropImage(tinyGIF(t), "image/gif", ImageRegion{X: 0, Y: 0, Width: 2, Height: 2}, "high", true); err != ErrImageRegionUnsupported {
		t.Errorf("GIF 应报不支持: %v", err)
	}
}

func TestCropImageDownscale(t *testing.T) {
	src := coordPNG(t, 3000, 1500)
	full := ImageRegion{X: 0, Y: 0, Width: 3000, Height: 1500}
	out, _, err := CropImage(src, "image/png", full, "high", true)
	if err != nil {
		t.Fatalf("裁剪失败: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1300 || img.Bounds().Dy() != 650 {
		t.Errorf("high 档应缩到 1300 长边: %v", img.Bounds())
	}
	out, _, err = CropImage(src, "image/png", full, "low", true)
	if err != nil {
		t.Fatal(err)
	}
	img, err = png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 256 {
		t.Errorf("low 档应缩到 512 长边: %v", img.Bounds())
	}
	out, _, err = CropImage(src, "image/png", full, "high", false)
	if err != nil {
		t.Fatal(err)
	}
	img, err = png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 3000 || img.Bounds().Dy() != 1500 {
		t.Errorf("关闭缩放应保留原尺寸: %v", img.Bounds())
	}
	small := ImageRegion{X: 0, Y: 0, Width: 1000, Height: 500}
	out, _, err = CropImage(src, "image/png", small, "high", true)
	if err != nil {
		t.Fatal(err)
	}
	img, err = png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1000 || img.Bounds().Dy() != 500 {
		t.Errorf("区域小于目标长边不应缩放: %v", img.Bounds())
	}
}

func TestCropImageJPEGOrientation(t *testing.T) {
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
	src := orientedJPEG(t, raw, 6)
	left, used, err := CropImage(src, "image/jpeg", ImageRegion{X: 0, Y: 0, Width: 10, Height: 40}, "high", false)
	if err != nil {
		t.Fatalf("裁剪失败: %v", err)
	}
	if used != (ImageRegion{X: 0, Y: 0, Width: 10, Height: 40}) {
		t.Errorf("实际区域: %+v", used)
	}
	img, err := jpeg.Decode(bytes.NewReader(left))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 10 || img.Bounds().Dy() != 40 {
		t.Fatalf("输出尺寸: %v", img.Bounds())
	}
	r, _, _, _ := img.At(5, 20).RGBA()
	if r < 200 {
		t.Errorf("方向 6 旋转后左半应为红: %d", r)
	}
	right, _, err := CropImage(src, "image/jpeg", ImageRegion{X: 10, Y: 0, Width: 10, Height: 40}, "high", false)
	if err != nil {
		t.Fatal(err)
	}
	img, err = jpeg.Decode(bytes.NewReader(right))
	if err != nil {
		t.Fatal(err)
	}
	_, _, b, _ := img.At(5, 20).RGBA()
	if b < 200 {
		t.Errorf("方向 6 旋转后右半应为蓝: %d", b)
	}
}
