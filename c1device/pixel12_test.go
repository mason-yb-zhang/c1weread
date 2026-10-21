package c1device

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"os"
	"reflect"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func pixel12Fixture() []byte {
	data := []byte("C1P1")
	for i, cp := range []rune{' ', 'A', 'g', '中', '\ufffd'} {
		record := make([]byte, 37)
		binary.LittleEndian.PutUint32(record, uint32(cp))
		record[4] = []byte{4, 8, 6, 12, 12}[i]
		if cp != ' ' {
			for y := 0; y < 16; y++ {
				binary.BigEndian.PutUint16(record[5+2*y:], 0x8001|uint16((i+y)*137))
			}
		}
		data = append(data, record...)
	}
	return data
}

func TestPixel12Validation(t *testing.T) {
	base := pixel12Fixture()
	cases := map[string][]byte{
		"nil": nil, "short": []byte("C1"), "empty": []byte("C1P1"),
		"header":    append([]byte("C1BF"), base[4:]...),
		"truncated": base[:len(base)-1], "extra": append(append([]byte(nil), base...), 0),
		"no fallback": base[:len(base)-37],
	}
	for name, cp := range map[string]uint32{"surrogate": 0xd800, "high surrogate": 0xdfff, "out of range": 0x110000, "overflow rune": 0xffffffff, "duplicate": ' ', "unsorted": 0} {
		data := append([]byte(nil), base...)
		binary.LittleEndian.PutUint32(data[4+37:], cp)
		cases[name] = data
	}
	for _, width := range []byte{0, 17, 255} {
		data := append([]byte(nil), base...)
		data[8] = width
		cases[fmt.Sprintf("width %d", width)] = data
	}
	emptyFallback := append([]byte(nil), base...)
	clear(emptyFallback[len(emptyFallback)-32:])
	cases["empty replacement"] = emptyFallback
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if face, err := NewPixel12Face(data); err == nil || face != nil {
				t.Fatal("accepted invalid font")
			}
		})
	}
	for width := byte(1); width <= 16; width++ {
		data := append([]byte(nil), base...)
		data[8] = width
		binary.LittleEndian.PutUint32(data[4:], 0)
		if _, err := NewPixel12Face(data); err != nil {
			t.Fatalf("valid scalar 0, width %d: %v", width, err)
		}
	}
}

func TestPixel12MetricsFallbackAndOwnership(t *testing.T) {
	data := pixel12Fixture()
	face, err := NewPixel12Face(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := face.face.(*bitmapFace); ok {
		t.Fatal("pixel12 must use font.Drawer path")
	}
	if face.LineHeight() != 16 || face.ascent != 12 {
		t.Fatal("wrong layout metrics")
	}
	wantMetrics := font.Metrics{Height: fixed.I(16), Ascent: fixed.I(12), Descent: fixed.I(4), XHeight: fixed.I(8), CapHeight: fixed.I(11)}
	if face.face.Metrics() != wantMetrics {
		t.Fatalf("metrics: %+v", face.face.Metrics())
	}
	for _, r := range []rune{' ', 'A', 'g', '中', '\ufffd', '?', -1, 0xd800, 0x110000} {
		width, present := map[rune]int{' ': 4, 'A': 8, 'g': 6, '中': 12, '\ufffd': 12}[r]
		if !present {
			width = 12
		}
		a, ok := face.face.GlyphAdvance(r)
		bounds, ba, bok := face.face.GlyphBounds(r)
		dr, mask, mp, ga, gok := face.face.Glyph(fixed.Point26_6{X: fixed.I(-4) + 31, Y: fixed.I(8) + 63}, r)
		if a != fixed.I(width) || a != ba || a != ga || ok != present || bok != ok || gok != ok {
			t.Fatalf("%U advance/status mismatch", r)
		}
		wantBounds := fixed.Rectangle26_6{Min: fixed.P(0, -12), Max: fixed.P(16, 4)}
		if r == ' ' {
			wantBounds = fixed.Rectangle26_6{}
		}
		if bounds != wantBounds || dr != image.Rect(-4, -4, 12, 12) || mp != (image.Point{}) || mask.Bounds() != image.Rect(0, 0, 16, 16) {
			t.Fatalf("%U bounds/mask mismatch %v %v", r, bounds, dr)
		}
		if face.face.Kern(r, 'A') != 0 {
			t.Fatal("unexpected kerning")
		}
	}
	if face.Measure("Ag中?") != 38 || face.Measure("") != 0 {
		t.Fatal("wrong measurement")
	}
	if got := face.Wrap("A中A中", 20); !reflect.DeepEqual(got, []string{"A中", "A中"}) {
		t.Fatalf("wrap=%q", got)
	}
	a, b := NewCanvas(), NewCanvas()
	a.DrawText(face, 0, 0, "Ag中?")
	clear(data)
	b.DrawText(face, 0, 0, "Ag中?")
	if !bytes.Equal(a.image.Pix, b.image.Pix) {
		t.Fatal("font aliases caller data")
	}
	if err := face.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPixel12AllThresholdsAndClipping(t *testing.T) {
	data := pixel12Fixture()
	face, err := NewPixel12Face(data)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	for _, origin := range []image.Point{{0, 0}, {-5, -3}, {DisplayWidth - 9, DisplayHeight - 8}, {-100, 0}, {0, -16}, {DisplayWidth, 0}, {0, DisplayHeight}} {
		want := NewCanvas()
		left := origin.X
		for _, glyph := range []int{1, 2, 3, 4} {
			record := data[4+37*glyph:]
			for y := 0; y < 16; y++ {
				row := binary.BigEndian.Uint16(record[5+2*y:])
				for x := 0; x < 16; x++ {
					if row&(0x8000>>uint(x)) != 0 {
						want.setBlack(left+x, origin.Y+y)
					}
				}
			}
			left += int(record[4])
		}
		for threshold := 1; threshold <= 255; threshold++ {
			got := NewCanvas()
			got.DrawTextThreshold(face, origin.X, origin.Y, "Ag中?", uint8(threshold))
			if !bytes.Equal(got.image.Pix, want.image.Pix) {
				t.Fatalf("origin=%v threshold=%d differs from original bits", origin, threshold)
			}
		}
	}
}

func TestPixel12GeneratedAssetAllBits(t *testing.T) {
	data, err := os.ReadFile("../book-reader/assets/fusion12.bin")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("fusion12.bin asset not bundled with this c1device checkout")
		}
		t.Fatal(err)
	}
	const wantHash = "93704a164e0a44e6186d49b69c1f8acf44219113c3c907583367220f6195a9c2"
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != wantHash {
		t.Fatalf("asset SHA256=%s", got)
	}
	if (len(data)-4)/37 != 36278 {
		t.Fatal("retained cmap coverage changed")
	}
	face, err := NewPixel12Face(data)
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	for offset := 4; offset < len(data); offset += 37 {
		record := data[offset : offset+37]
		r := rune(binary.LittleEndian.Uint32(record))
		dr, mask, mp, advance, ok := face.face.Glyph(fixed.P(0, 12), r)
		if !ok || advance != fixed.I(int(record[4])) || dr != image.Rect(0, 0, 16, 16) || mp != (image.Point{}) {
			t.Fatalf("%U glyph contract", r)
		}
		ink := image.Rectangle{}
		for y := -1; y <= 16; y++ {
			for x := -1; x <= 16; x++ {
				want := uint8(0)
				if x >= 0 && x < 16 && y >= 0 && y < 16 && binary.BigEndian.Uint16(record[5+2*y:])&(0x8000>>uint(x)) != 0 {
					want = 255
					ink = ink.Union(image.Rect(x, y, x+1, y+1))
				}
				if got := color.AlphaModel.Convert(mask.At(x, y)).(color.Alpha).A; got != want {
					t.Fatalf("%U pixel %d,%d=%d want %d", r, x, y, got, want)
				}
			}
		}
		bounds, ba, bok := face.face.GlyphBounds(r)
		if !ink.Empty() {
			ink = ink.Add(image.Pt(0, -12))
		}
		if !bok || ba != advance || bounds != (fixed.Rectangle26_6{Min: fixed.P(ink.Min.X, ink.Min.Y), Max: fixed.P(ink.Max.X, ink.Max.Y)}) {
			t.Fatalf("%U ink bounds %v want %v", r, bounds, ink)
		}
	}
	for _, r := range []rune{'\u00c0', '\u2502', '\ue100'} {
		advance, ok := face.face.GlyphAdvance(r)
		fallbackAdvance, _ := face.face.GlyphAdvance('\ufffd')
		_, mask, _, glyphAdvance, glyphOK := face.face.Glyph(fixed.P(0, 12), r)
		_, fallbackMask, _, _, fallbackOK := face.face.Glyph(fixed.P(0, 12), '\ufffd')
		if ok || glyphOK || !fallbackOK || advance != fallbackAdvance || glyphAdvance != fallbackAdvance {
			t.Fatalf("excluded %U must use replacement with missing-glyph status", r)
		}
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				if mask.At(x, y) != fallbackMask.At(x, y) {
					t.Fatalf("excluded %U replacement pixel mismatch at %d,%d", r, x, y)
				}
			}
		}
		a, b := NewCanvas(), NewCanvas()
		a.DrawTextThreshold(face, 0, 0, string(r), 255)
		b.DrawTextThreshold(face, 0, 0, "\ufffd", 255)
		if !bytes.Equal(a.image.Pix, b.image.Pix) {
			t.Fatalf("excluded %U does not render replacement", r)
		}
	}
	for _, text := range []string{"gypqj", "，。！？；：、（）《》“”‘’", ",.!?;:()[]{}"} {
		bounds, _ := font.BoundString(face.face, text)
		if bounds.Min.Y < fixed.I(-12) || bounds.Max.Y > fixed.I(4) {
			t.Fatalf("punctuation/descenders outside cell: %q %v", text, bounds)
		}
	}
}
