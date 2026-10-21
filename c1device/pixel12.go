package c1device

import (
	"encoding/binary"
	"fmt"
	"image"
	"math/bits"
	"sort"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// NewPixel12Face renders native 12px glyphs without resampling. C1P1 records
// contain a LE32 Unicode scalar, advance byte, and sixteen BE16 bitmap rows.
// The 16px cell retains descenders; its baseline is 12px below the cell top.
func NewPixel12Face(data []byte) (*Face, error) {
	if len(data) <= 4 || string(data[:4]) != "C1P1" || (len(data)-4)%37 != 0 {
		return nil, fmt.Errorf("invalid pixel12 font")
	}
	p := &pixel12Font{data: append([]byte(nil), data[4:]...), fallback: -1}
	var last uint32
	for i := 0; i < len(p.data)/37; i++ {
		cp := binary.LittleEndian.Uint32(p.data[i*37:])
		width := p.data[i*37+4]
		if cp > 0x10ffff || cp >= 0xd800 && cp <= 0xdfff || i > 0 && cp <= last || width < 1 || width > 16 {
			return nil, fmt.Errorf("invalid pixel12 glyph %d", i)
		}
		if cp == 0xfffd {
			p.fallback = i
		}
		last = cp
	}
	if p.fallback < 0 {
		return nil, fmt.Errorf("pixel12 font missing replacement glyph")
	}
	if p.inkBounds(p.fallback).Empty() {
		return nil, fmt.Errorf("pixel12 font has empty replacement glyph")
	}
	return &Face{face: p, lineHeight: 16, ascent: 12}, nil
}

type pixel12Font struct {
	data     []byte
	fallback int
}

var _ font.Face = (*pixel12Font)(nil)

func (p *pixel12Font) index(r rune) (int, bool) {
	n := len(p.data) / 37
	i := sort.Search(n, func(i int) bool { return rune(binary.LittleEndian.Uint32(p.data[i*37:])) >= r })
	if i < n && rune(binary.LittleEndian.Uint32(p.data[i*37:])) == r {
		return i, true
	}
	return p.fallback, false
}

func (p *pixel12Font) Close() error                   { return nil }
func (p *pixel12Font) Kern(r0, r1 rune) fixed.Int26_6 { return 0 }
func (p *pixel12Font) Metrics() font.Metrics {
	return font.Metrics{Height: fixed.I(16), Ascent: fixed.I(12), Descent: fixed.I(4), XHeight: fixed.I(8), CapHeight: fixed.I(11)}
}
func (p *pixel12Font) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	i, ok := p.index(r)
	return fixed.I(int(p.data[i*37+4])), ok
}
func (p *pixel12Font) inkBounds(i int) image.Rectangle {
	bounds := image.Rectangle{}
	for y := 0; y < 16; y++ {
		row := binary.BigEndian.Uint16(p.data[i*37+5+y*2:])
		if row != 0 {
			bounds = bounds.Union(image.Rect(bits.LeadingZeros16(row), y, 16-bits.TrailingZeros16(row), y+1))
		}
	}
	return bounds
}
func (p *pixel12Font) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	i, ok := p.index(r)
	bounds := p.inkBounds(i)
	if !bounds.Empty() {
		bounds = bounds.Add(image.Pt(0, -12))
	}
	return fixed.Rectangle26_6{Min: fixed.P(bounds.Min.X, bounds.Min.Y), Max: fixed.P(bounds.Max.X, bounds.Max.Y)}, fixed.I(int(p.data[i*37+4])), ok
}
func (p *pixel12Font) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	i, ok := p.index(r)
	record := p.data[i*37 : (i+1)*37]
	left, top := dot.X.Floor(), dot.Y.Floor()-12
	// Advance is not an ink bound: retain all columns, including overhangs.
	return image.Rect(left, top, left+16, top+16), bitmapMask{rows: record[5:], width: 16}, image.Point{}, fixed.I(int(record[4])), ok
}
