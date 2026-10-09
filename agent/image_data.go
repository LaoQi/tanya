package agent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
)

type ImageRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

var (
	ErrImageRegionUnsupported = errors.New("image region: unsupported format")
	ErrImageRegionInvalid     = errors.New("image region: invalid rect")
	ErrImageRegionEmpty       = errors.New("image region: empty intersection")
)

const (
	ImageResizeLowSide = 512
	ImageResizeSide    = 1300
	JPEGQuality        = 85
	sniffLen           = 512
)

func ResizeTargetSide(detail string) int {
	if NormalizeImageDetail(detail) == "low" {
		return ImageResizeLowSide
	}
	return ImageResizeSide
}

func MaybeResizeImage(data []byte, mime, detail string, enabled bool) []byte {
	if !enabled {
		return data
	}
	switch mime {
	case "image/png", "image/jpeg":
	default:
		return data
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	if mime == "image/jpeg" && JPEGOrientation(data) > 1 {
		return data
	}
	scaled := downscale(img, ResizeTargetSide(detail))
	if scaled == nil {
		return data
	}
	var buf bytes.Buffer
	switch mime {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: JPEGQuality}); err != nil {
			return data
		}
	default:
		if err := png.Encode(&buf, scaled); err != nil {
			return data
		}
	}
	if buf.Len() >= len(data) {
		return data
	}
	return buf.Bytes()
}

func CropImage(data []byte, mime string, r ImageRegion, detail string, enabled bool) ([]byte, ImageRegion, error) {
	switch mime {
	case "image/png", "image/jpeg":
	default:
		return nil, r, ErrImageRegionUnsupported
	}
	if r.Width <= 0 || r.Height <= 0 {
		return nil, r, ErrImageRegionInvalid
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, r, err
	}
	b := img.Bounds()
	o := 1
	if mime == "image/jpeg" {
		o = JPEGOrientation(data)
	}
	dw, dh := b.Dx(), b.Dy()
	if o >= 5 {
		dw, dh = dh, dw
	}
	x0 := clampInt(r.X, 0, dw)
	y0 := clampInt(r.Y, 0, dh)
	x1 := clampInt(r.X+r.Width, 0, dw)
	y1 := clampInt(r.Y+r.Height, 0, dh)
	if x1 <= x0 || y1 <= y0 {
		return nil, r, ErrImageRegionEmpty
	}
	crop := image.Image(sourceWindow(img, o, x0, y0, x1, y1))
	if o > 1 {
		crop = orientImage(crop, o)
	}
	out := crop
	if enabled {
		if scaled := downscale(crop, ResizeTargetSide(detail)); scaled != nil {
			out = scaled
		}
	}
	var buf bytes.Buffer
	if mime == "image/jpeg" {
		err = jpeg.Encode(&buf, out, &jpeg.Options{Quality: JPEGQuality})
	} else {
		err = png.Encode(&buf, out)
	}
	if err != nil {
		return nil, r, err
	}
	return buf.Bytes(), ImageRegion{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func sourceWindow(src image.Image, o int, x0, y0, x1, y1 int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var rx0, rx1, ry0, ry1 int
	switch o {
	case 2:
		rx0, rx1, ry0, ry1 = w-x1, w-x0, y0, y1
	case 3:
		rx0, rx1, ry0, ry1 = w-x1, w-x0, h-y1, h-y0
	case 4:
		rx0, rx1, ry0, ry1 = x0, x1, h-y1, h-y0
	case 5:
		rx0, rx1, ry0, ry1 = y0, y1, x0, x1
	case 6:
		rx0, rx1, ry0, ry1 = y0, y1, h-x1, h-x0
	case 7:
		rx0, rx1, ry0, ry1 = w-y1, w-y0, h-x1, h-x0
	case 8:
		rx0, rx1, ry0, ry1 = w-y1, w-y0, x0, x1
	default:
		rx0, rx1, ry0, ry1 = x0, x1, y0, y1
	}
	dst := image.NewRGBA(image.Rect(0, 0, rx1-rx0, ry1-ry0))
	draw.Draw(dst, dst.Bounds(), src, image.Pt(b.Min.X+rx0, b.Min.Y+ry0), draw.Src)
	return dst
}

func orientImage(src image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			dst.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}

func downscale(src image.Image, maxSide int) image.Image {
	if maxSide <= 0 {
		return nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= maxSide {
		return nil
	}
	nw, nh := maxSide, h*maxSide/w
	if h > w {
		nh, nw = maxSide, w*maxSide/h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0 := b.Min.Y + y*h/nh
		y1 := b.Min.Y + (y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0 := b.Min.X + x*w/nw
			x1 := b.Min.X + (x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r / n >> 8),
				G: uint8(g / n >> 8),
				B: uint8(bl / n >> 8),
				A: uint8(a / n >> 8),
			})
		}
	}
	return dst
}

func JPEGOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		switch {
		case marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7):
			i += 2
			continue
		case marker == 0xDA:
			return 1
		}
		size := int(data[i+2])<<8 | int(data[i+3])
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		if marker == 0xE1 && size >= 8 {
			seg := data[i+4 : i+2+size]
			if bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
				return exifOrientation(seg[6:])
			}
		}
		i += 2 + size
	}
	return 1
}

func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:4]) != 0x2A {
		return 1
	}
	off := int(bo.Uint32(tiff[4:8]))
	if off < 0 || off+2 > len(tiff) {
		return 1
	}
	count := int(bo.Uint16(tiff[off : off+2]))
	base := off + 2
	for i := 0; i < count; i++ {
		e := base + i*12
		if e+12 > len(tiff) {
			return 1
		}
		if bo.Uint16(tiff[e:e+2]) != 0x0112 {
			continue
		}
		v := int(bo.Uint16(tiff[e+8 : e+10]))
		if v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

func NormalizeImageMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	case "image/gif":
		return "image/gif"
	case "image/webp":
		return "image/webp"
	}
	return ""
}

func SniffImageMIME(data []byte) string {
	head := data
	if len(head) > sniffLen {
		head = head[:sniffLen]
	}
	return NormalizeImageMIME(http.DetectContentType(head))
}

func ImageDimensions(data []byte, mime string) (int, int) {
	if mime == "image/webp" {
		return WebPDimensions(data)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	w, h := cfg.Width, cfg.Height
	if mime == "image/jpeg" && w > 0 && h > 0 && JPEGOrientation(data) >= 5 {
		w, h = h, w
	}
	return w, h
}

func WebPDimensions(data []byte) (int, int) {
	if len(data) < 21 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0
	}
	switch string(data[12:16]) {
	case "VP8X":
		if len(data) < 30 {
			return 0, 0
		}
		w := int(data[24]) | int(data[25])<<8 | int(data[26])<<16
		h := int(data[27]) | int(data[28])<<8 | int(data[29])<<16
		return w + 1, h + 1
	case "VP8 ":
		if len(data) < 27 || data[20] != 0x9d || data[21] != 0x01 || data[22] != 0x2a {
			return 0, 0
		}
		return (int(data[23]) | int(data[24])<<8) & 0x3fff, (int(data[25]) | int(data[26])<<8) & 0x3fff
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2f {
			return 0, 0
		}
		b := uint32(data[21]) | uint32(data[22])<<8 | uint32(data[23])<<16 | uint32(data[24])<<24
		return int(b&0x3fff) + 1, int(b>>14&0x3fff) + 1
	}
	return 0, 0
}
