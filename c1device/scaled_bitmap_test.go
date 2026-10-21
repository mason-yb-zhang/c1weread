package c1device

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"reflect"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func scaledBitmapFixture() []byte {
	data := []byte("C1BF")
	for glyph, cp := range []rune{'A', '中', '\ufffd'} {
		record := make([]byte, 37)
		binary.LittleEndian.PutUint32(record, uint32(cp))
		record[4] = 16
		if cp == 'A' {
			record[4] = 8
		}
		for y := 0; y < 16; y++ {
			binary.BigEndian.PutUint16(record[5+2*y:], uint16(0x8001>>uint(y%5))^uint16((y+glyph)*0x123))
		}
		data = append(data, record...)
	}
	return data
}

func TestScaledBitmapSizesAndAdvances(t *testing.T) {
	for _, size := range []int{12, 14, 16, 18, 20, 24} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			for _, lineHeight := range []int{size, size + 5} {
				face, err := NewScaledBitmapFace(scaledBitmapFixture(), size, lineHeight)
				if err != nil {
					t.Fatal(err)
				}
				defer face.Close()
				if face.ascent != size || face.LineHeight() != lineHeight {
					t.Fatalf("face metrics: ascent=%d height=%d", face.ascent, face.LineHeight())
				}
				wantMetrics := font.Metrics{Height: fixed.I(lineHeight), Ascent: fixed.I(size), XHeight: fixed.I(size / 2), CapHeight: fixed.I(size)}
				if got := face.face.Metrics(); got != wantMetrics {
					t.Fatalf("metrics=%+v, want %+v", got, wantMetrics)
				}
				for _, r := range []rune{'A', '中', '\ufffd', '?', -1, 0x110000} {
					width := size
					if r == 'A' {
						width /= 2
					}
					a, ok := face.face.GlyphAdvance(r)
					bounds, ba, bok := face.face.GlyphBounds(r)
					dot := fixed.Point26_6{X: fixed.I(-4) + 31, Y: fixed.I(7) + 63}
					dr, mask, mp, ga, gok := face.face.Glyph(dot, r)
					wantOK := size == 16 || r == 'A' || r == '中' || r == '\ufffd'
					if a != fixed.I(width) || ba != a || ga != a || ok != wantOK || bok != ok || gok != ok {
						t.Fatalf("%U inconsistent advances/status: %v/%v/%v %v/%v/%v", r, a, ba, ga, ok, bok, gok)
					}
					if bounds != (fixed.Rectangle26_6{Min: fixed.P(0, -size), Max: fixed.P(width, 0)}) || dr != image.Rect(-4, 7-size, -4+width, 7) {
						t.Fatalf("%U incorrect bounds: %v %v", r, bounds, dr)
					}
					if mp != (image.Point{}) || mask.Bounds() != image.Rect(0, 0, width, size) || mask.ColorModel() != color.AlphaModel {
						t.Fatalf("%U incorrect mask", r)
					}
					if face.face.Kern(r, 'A') != 0 {
						t.Fatal("unexpected kerning")
					}
				}
				text := "A中?A"
				wantWidth := 3 * size
				drawer := font.Drawer{Dst: image.NewAlpha(image.Rect(0, 0, wantWidth, size)), Src: image.White, Face: face.face, Dot: fixed.P(0, size)}
				drawer.DrawString(text)
				_, boundAdvance := font.BoundString(face.face, text)
				if face.Measure(text) != wantWidth || drawer.Dot.X != fixed.I(wantWidth) || boundAdvance != drawer.Dot.X {
					t.Fatalf("measure=%d drawer=%v bounds=%v", face.Measure(text), drawer.Dot.X, boundAdvance)
				}
				if got := face.Wrap("A中A中", size+size/2); !reflect.DeepEqual(got, []string{"A中", "A中"}) {
					t.Fatalf("wrap=%q", got)
				}
				if face.Measure("") != 0 || face.Wrap("A", 0) != nil {
					t.Fatal("empty measurement or zero-width wrapping")
				}
			}
		})
	}
}

func TestScaledBitmapNearestNeighborAndMaskEdges(t *testing.T) {
	data := scaledBitmapFixture()
	for _, size := range []int{12, 14, 16, 18, 20, 24} {
		face, err := NewScaledBitmapFace(data, size, size)
		if err != nil {
			t.Fatal(err)
		}
		defer face.Close()
		for glyph, r := range []rune{'A', '中', '\ufffd', '?'} {
			if glyph == 3 {
				glyph = 2
			}
			record := data[4+37*glyph:]
			sourceWidth := int(record[4])
			width := sourceWidth * size / 16
			_, mask, _, _, _ := face.face.Glyph(fixed.Point26_6{}, r)
			for y := -1; y <= size; y++ {
				for x := -1; x <= width; x++ {
					want := uint8(0)
					if x >= 0 && y >= 0 && x < width && y < size {
						row := binary.BigEndian.Uint16(record[5+2*(y*16/size):])
						if row&(0x8000>>uint(x*sourceWidth/width)) != 0 {
							want = 255
						}
					}
					if got := color.AlphaModel.Convert(mask.At(x, y)).(color.Alpha).A; got != want {
						t.Fatalf("size %d %U (%d,%d): alpha=%d want=%d", size, r, x, y, got, want)
					}
				}
			}
			maxInt := int(^uint(0) >> 1)
			for _, p := range []image.Point{{maxInt, maxInt}, {-maxInt - 1, -maxInt - 1}} {
				if got := color.AlphaModel.Convert(mask.At(p.X, p.Y)).(color.Alpha).A; got != 0 {
					t.Fatalf("out-of-bounds mask alpha=%d", got)
				}
			}
		}
	}
}

func TestScaledBitmapNative16Unchanged(t *testing.T) {
	data := scaledBitmapFixture()
	native, err := NewBitmapFace(data, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	scaled, err := NewScaledBitmapFace(data, 16, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer scaled.Close()
	if _, ok := scaled.face.(*bitmapFace); !ok {
		t.Fatal("size 16 did not retain native face")
	}
	for _, origin := range []image.Point{{0, 0}, {-5, -3}, {DisplayWidth - 7, DisplayHeight - 9}, {-100, 0}, {DisplayWidth, DisplayHeight}} {
		for _, threshold := range []uint8{0, 48, 128, 220, 255} {
			a, b := NewCanvas(), NewCanvas()
			a.DrawTextThreshold(native, origin.X, origin.Y, "A中?", threshold)
			b.DrawTextThreshold(scaled, origin.X, origin.Y, "A中?", threshold)
			if !bytes.Equal(a.image.Pix, b.image.Pix) || a.Frame(128) != b.Frame(128) {
				t.Fatalf("native pixels changed at %v threshold %d", origin, threshold)
			}
		}
	}
}

func TestScaledBitmapCanvasClipping(t *testing.T) {
	data := scaledBitmapFixture()
	for _, size := range []int{12, 14, 18, 20, 24} {
		face, err := NewScaledBitmapFace(data, size, size+4)
		if err != nil {
			t.Fatal(err)
		}
		defer face.Close()
		for _, origin := range []image.Point{{0, 0}, {-5, -3}, {DisplayWidth - 7, DisplayHeight - 9}, {-100, 0}, {0, -size}, {DisplayWidth, 0}, {0, DisplayHeight}} {
			for _, threshold := range []uint8{1, 48, 255} {
				canvas := NewCanvas()
				canvas.DrawTextThreshold(face, origin.X, origin.Y, "A中?", threshold)
				for y := 0; y < DisplayHeight; y++ {
					for x := 0; x < DisplayWidth; x++ {
						want := uint8(255)
						left := origin.X
						for glyph, sourceWidth := range []int{8, 16, 16} {
							width := sourceWidth * size / 16
							dx, dy := x-left, y-origin.Y
							if dx >= 0 && dx < width && dy >= 0 && dy < size {
								row := binary.BigEndian.Uint16(data[4+37*glyph+5+2*(dy*16/size):])
								if row&(0x8000>>uint(dx*sourceWidth/width)) != 0 {
									want = 0
								}
							}
							left += width
						}
						if got := canvas.image.GrayAt(x, y).Y; got != want {
							t.Fatalf("size %d origin %v threshold %d pixel (%d,%d)=%d want %d", size, origin, threshold, x, y, got, want)
						}
					}
				}
			}
		}
	}
}

func TestScaledBitmapValidationAndDataOwnership(t *testing.T) {
	for _, size := range []int{-1, 0, 1, 13, 15, 17, 25, int(^uint(0) >> 1)} {
		if face, err := NewScaledBitmapFace(scaledBitmapFixture(), size, 32); err == nil || face != nil {
			t.Fatalf("accepted unsupported size %d", size)
		}
	}
	for _, size := range []int{12, 14, 16, 18, 20, 24} {
		if face, err := NewScaledBitmapFace(scaledBitmapFixture(), size, size-1); err == nil || face != nil {
			t.Fatalf("accepted short line height for size %d", size)
		}
		invalidWidth := scaledBitmapFixture()
		invalidWidth[8] = 9
		missingFallback := scaledBitmapFixture()[:4+2*37]
		for _, data := range [][]byte{nil, []byte("C1BF"), []byte("NOPE"), append(scaledBitmapFixture(), 0), invalidWidth, missingFallback} {
			if face, err := NewScaledBitmapFace(data, size, size); err == nil || face != nil {
				t.Fatalf("accepted malformed font at size %d", size)
			}
		}
		data := scaledBitmapFixture()
		face, err := NewScaledBitmapFace(data, size, size)
		if err != nil {
			t.Fatal(err)
		}
		a, b := NewCanvas(), NewCanvas()
		a.DrawText(face, 0, 0, "A中?")
		clear(data)
		b.DrawText(face, 0, 0, "A中?")
		if !bytes.Equal(a.image.Pix, b.image.Pix) || face.Measure("A中?") != size/2+2*size {
			t.Fatal("caller mutation changed font")
		}
		if err := face.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
