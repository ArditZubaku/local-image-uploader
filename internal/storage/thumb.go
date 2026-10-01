package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "image/gif"
	_ "image/png"
)

// Thumbnailer renders small previews of images and caches them on disk.
//
// It exists for one reason: a phone browsing a photo folder over Wi-Fi
// should not pull tens of megabytes of full-resolution JPEG just to render
// a grid. A 320 px preview is ~20 KB, so a 60-photo folder goes from
// ~300 MB to ~1 MB.
type Thumbnailer struct {
	cacheDir string
	sem      chan struct{}
}

var thumbExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
}

// CanThumbnail reports whether a preview can be rendered for this name.
// HEIC/HEIF and video are excluded: the standard library cannot decode
// them, and pulling in a decoder is not worth a dependency here.
func CanThumbnail(name string) bool {
	return thumbExts[strings.ToLower(filepath.Ext(name))]
}

func NewThumbnailer(cacheDir string) (*Thumbnailer, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating thumbnail cache: %w", err)
	}

	// Each in-flight render holds a full decoded frame in memory, so a
	// 12 MP photo costs ~48 MB. Cap the concurrency rather than let a
	// grid of 60 images fan out and swap the machine.
	workers := min(runtime.NumCPU(), 4)

	return &Thumbnailer{cacheDir: cacheDir, sem: make(chan struct{}, workers)}, nil
}

// Thumb returns the path to a cached JPEG preview of src, no larger than
// maxDim on its longest side, rendering it first if necessary.
func (t *Thumbnailer) Thumb(src string, maxDim int) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}

	// Keying on mtime and size means a replaced file gets a new cache
	// entry, which in turn lets the HTTP layer mark previews immutable.
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%d|%d|%d", src, info.ModTime().UnixNano(), info.Size(), maxDim))
	dst := filepath.Join(t.cacheDir, hex.EncodeToString(sum[:16])+".jpg")

	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}

	t.sem <- struct{}{}
	defer func() { <-t.sem }()

	// Another request may have rendered it while we waited for a slot.
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}

	if err := render(src, dst, maxDim); err != nil {
		return "", err
	}

	return dst, nil
}

func render(src, dst string, maxDim int) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decoding %s: %w", filepath.Base(src), err)
	}

	small := downscale(img, maxDim)
	small = reorient(small, jpegOrientation(raw))

	// Write to a temp name and rename: a request that arrives mid-render
	// must never find a half-written JPEG under the final cache key.
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".thumb-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := jpeg.Encode(tmp, small, &jpeg.Options{Quality: 78}); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), dst)
}

// downscale box-filters img down so its longest side is at most maxDim.
// Averaging whole source boxes (rather than sampling) is what keeps a
// 4000 px photo from aliasing into noise at 320 px.
func downscale(img image.Image, maxDim int) *image.NRGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()

	src, ok := img.(*image.NRGBA)
	if !ok || src.Bounds().Min != (image.Point{}) {
		src = image.NewNRGBA(image.Rect(0, 0, sw, sh))
		draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	}

	if sw <= maxDim && sh <= maxDim {
		return src
	}

	tw, th := sw, sh
	if sw >= sh {
		tw, th = maxDim, max(1, sh*maxDim/sw)
	} else {
		tw, th = max(1, sw*maxDim/sh), maxDim
	}

	out := image.NewNRGBA(image.Rect(0, 0, tw, th))
	for y := range th {
		y0, y1 := y*sh/th, max(y*sh/th+1, (y+1)*sh/th)
		for x := range tw {
			x0, x1 := x*sw/tw, max(x*sw/tw+1, (x+1)*sw/tw)

			var r, g, bl, a, n uint32
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride:]
				for sx := x0; sx < x1; sx++ {
					p := row[sx*4:]
					r += uint32(p[0])
					g += uint32(p[1])
					bl += uint32(p[2])
					a += uint32(p[3])
					n++
				}
			}

			o := out.Pix[y*out.Stride+x*4:]
			o[0] = uint8(r / n)
			o[1] = uint8(g / n)
			o[2] = uint8(bl / n)
			o[3] = uint8(a / n)
		}
	}

	return out
}

// reorient applies an EXIF orientation to an already-downscaled image, so
// photos shot sideways on a phone don't come back sideways in the grid.
func reorient(img *image.NRGBA, orientation int) *image.NRGBA {
	if orientation <= 1 || orientation > 8 {
		return img
	}

	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	swap := orientation >= 5
	ow, oh := w, h
	if swap {
		ow, oh = h, w
	}

	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch orientation {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			copy(out.Pix[ny*out.Stride+nx*4:][:4], img.Pix[y*img.Stride+x*4:][:4])
		}
	}

	return out
}

// jpegOrientation walks JPEG segment headers looking for the EXIF
// orientation tag, returning 1 (no transform) for anything it can't read.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}

	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}

		segLen := int(binary.BigEndian.Uint16(data[i+2:]))
		if segLen < 2 || i+2+segLen > len(data) {
			return 1
		}
		if marker == 0xE1 {
			if o, ok := exifOrientation(data[i+4 : i+2+segLen]); ok {
				return o
			}
		}
		i += 2 + segLen
	}

	return 1
}

func exifOrientation(seg []byte) (int, bool) {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 0, false
	}
	tiff := seg[6:]

	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, false
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return 0, false
	}

	off := int(bo.Uint32(tiff[4:]))
	if off < 8 || off+2 > len(tiff) {
		return 0, false
	}

	for k := range int(bo.Uint16(tiff[off:])) {
		e := off + 2 + k*12
		if e+12 > len(tiff) {
			return 0, false
		}
		if bo.Uint16(tiff[e:]) == 0x0112 {
			return int(bo.Uint16(tiff[e+8:])), true
		}
	}

	return 0, false
}
