package c1device

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// NewScaledBitmapFace scales native bitmap glyphs to the supported pixel sizes
// using nearest-neighbor sampling. Size 16 retains the native drawing path.
func NewScaledBitmapFace(data []byte, size int, lineHeight int) (*Face, error) {
	if size == 16 {
		return NewBitmapFace(data, lineHeight)
	}
	switch size {
	case 12, 14, 18, 20, 24:
	default:
		return nil, fmt.Errorf("unsupported bitmap font size %d", size)
	}
	if lineHeight < size {
		return nil, fmt.Errorf("bitmap line height %d is smaller than size %d", lineHeight, size)
	}
	native, err := NewBitmapFace(data, max(16, lineHeight))
	if err != nil {
		return nil, err
	}
	return &Face{
		face:       &scaledBitmapFace{bitmap: native.face.(*bitmapFace), size: size, lineHeight: lineHeight},
		lineHeight: lineHeight,
		ascent:     size,
	}, nil
}

type scaledBitmapFace struct {
	bitmap     *bitmapFace
	size       int
	lineHeight int
}

func (b *scaledBitmapFace) Close() error                   { return b.bitmap.Close() }
func (b *scaledBitmapFace) Kern(r0, r1 rune) fixed.Int26_6 { return 0 }
func (b *scaledBitmapFace) Metrics() font.Metrics {
	return font.Metrics{
		Height: fixed.I(b.lineHeight), Ascent: fixed.I(b.size),
		XHeight: fixed.I(b.size / 2), CapHeight: fixed.I(b.size),
	}
}
func (b *scaledBitmapFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	i := b.bitmap.index(r) * 37
	return fixed.I(int(b.bitmap.data[i+4]) * b.size / 16), rune(binary.LittleEndian.Uint32(b.bitmap.data[i:])) == r
}
func (b *scaledBitmapFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	a, ok := b.GlyphAdvance(r)
	return fixed.Rectangle26_6{Min: fixed.P(0, -b.size), Max: fixed.Point26_6{X: a}}, a, ok
}
func (b *scaledBitmapFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	i := b.bitmap.index(r) * 37
	w := int(b.bitmap.data[i+4])
	width := w * b.size / 16
	left, top := dot.X.Floor(), dot.Y.Floor()-b.size
	mask := scaledBitmapMask{
		source: bitmapMask{rows: b.bitmap.data[i+5 : i+37], width: w},
		width:  width, height: b.size,
	}
	ok := rune(binary.LittleEndian.Uint32(b.bitmap.data[i:])) == r
	return image.Rect(left, top, left+width, top+b.size), mask, image.Point{}, fixed.I(width), ok
}

type scaledBitmapMask struct {
	source        bitmapMask
	width, height int
}

func (m scaledBitmapMask) ColorModel() color.Model { return color.AlphaModel }
func (m scaledBitmapMask) Bounds() image.Rectangle { return image.Rect(0, 0, m.width, m.height) }
func (m scaledBitmapMask) At(x, y int) color.Color {
	if x < 0 || y < 0 || x >= m.width || y >= m.height {
		return color.Alpha{}
	}
	return m.source.At(x*m.source.width/m.width, y*16/m.height)
}
