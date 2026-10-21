package c1device

import "testing"

func TestOrientationLogicalSize(t *testing.T) {
	cases := []struct {
		orientation Orientation
		width       int
		height      int
		portrait    bool
	}{
		{Rotate0, DisplayWidth, DisplayHeight, false},
		{Rotate90, DisplayHeight, DisplayWidth, true},
		{Rotate180, DisplayWidth, DisplayHeight, false},
		{Rotate270, DisplayHeight, DisplayWidth, true},
	}
	for _, tc := range cases {
		width, height := tc.orientation.LogicalSize()
		if width != tc.width || height != tc.height {
			t.Fatalf("%v size = %dx%d, want %dx%d", tc.orientation, width, height, tc.width, tc.height)
		}
		if tc.orientation.Portrait() != tc.portrait {
			t.Fatalf("%v portrait = %v", tc.orientation, tc.orientation.Portrait())
		}
		if !tc.orientation.Valid() {
			t.Fatalf("%v should be valid", tc.orientation)
		}
	}
	if Orientation(4).Valid() {
		t.Fatal("orientation 4 must be invalid")
	}
}

func TestOrientationMapToPhysical(t *testing.T) {
	cases := []struct {
		orientation Orientation
		x, y        int
		px, py      int
	}{
		{Rotate0, 0, 0, 0, 0},
		{Rotate0, 10, 20, 10, 20},
		// Logical top-left of a CW-held device lands on the panel bottom-left.
		{Rotate90, 0, 0, 0, DisplayHeight - 1},
		{Rotate90, 3, 5, 5, DisplayHeight - 1 - 3},
		{Rotate180, 0, 0, DisplayWidth - 1, DisplayHeight - 1},
		{Rotate180, 1, 2, DisplayWidth - 2, DisplayHeight - 3},
		{Rotate270, 0, 0, DisplayWidth - 1, 0},
		{Rotate270, 3, 5, DisplayWidth - 6, 3},
	}
	for _, tc := range cases {
		px, py := tc.orientation.MapToPhysical(tc.x, tc.y)
		if px != tc.px || py != tc.py {
			t.Fatalf("%v map(%d,%d) = (%d,%d), want (%d,%d)", tc.orientation, tc.x, tc.y, px, py, tc.px, tc.py)
		}
	}
}

func TestOrientationRemapKey(t *testing.T) {
	// Physical Up becomes visual Right when the device is turned clockwise.
	got := Rotate90.RemapKey(KeyUp)
	if got != KeyRight {
		t.Fatalf("Rotate90 Up -> %v, want Right", got)
	}
	got = Rotate90.RemapKey(KeyLeft)
	if got != KeyUp {
		t.Fatalf("Rotate90 Left -> %v, want Up", got)
	}
	got = Rotate270.RemapKey(KeyUp)
	if got != KeyLeft {
		t.Fatalf("Rotate270 Up -> %v, want Left", got)
	}
	got = Rotate180.RemapKey(KeyDown)
	if got != KeyUp {
		t.Fatalf("Rotate180 Down -> %v, want Up", got)
	}
	if Rotate0.RemapKey(KeyOK) != KeyOK {
		t.Fatal("Rotate0 must keep non-arrow keys")
	}
	if Rotate90.RemapKey(KeyOK) != KeyOK || Rotate90.RemapKey(KeyBack) != KeyBack {
		t.Fatal("portrait must keep OK/Back")
	}
}

func framePixel(frame Frame, x, y int) bool {
	offset := (y/8)*DisplayWidth + x
	return frame[offset]&(0x80>>uint(y&7)) != 0
}

func TestCanvasOrientationFrameRotation(t *testing.T) {
	// One black pixel in each portrait canvas maps to the expected panel pixel.
	cases := []struct {
		orientation Orientation
		x, y        int
		px, py      int
	}{
		{Rotate90, 1, 2, 2, DisplayHeight - 2},
		{Rotate270, 1, 2, DisplayWidth - 3, 1},
		{Rotate180, 1, 2, DisplayWidth - 2, DisplayHeight - 3},
	}
	for _, tc := range cases {
		canvas := NewCanvasOrientation(tc.orientation)
		canvas.setBlack(tc.x, tc.y)
		frame := canvas.Frame(128)
		for y := 0; y < DisplayHeight; y++ {
			for x := 0; x < DisplayWidth; x++ {
				want := x == tc.px && y == tc.py
				if framePixel(frame, x, y) != want {
					t.Fatalf("%v pixel(%d,%d) black=%v, want only (%d,%d)", tc.orientation, x, y, framePixel(frame, x, y), tc.px, tc.py)
				}
			}
		}
	}
}

func TestRotate90IsNotMirrored(t *testing.T) {
	// An L-shaped trio is chiral: a transpose/mirror would swap the corner.
	canvas := NewCanvasOrientation(Rotate90)
	canvas.setBlack(0, 0) // logical top-left
	canvas.setBlack(2, 0) // logical top-right neighbor
	canvas.setBlack(0, 2) // logical bottom-left neighbor
	frame := canvas.Frame(128)
	// Top-left of the held device is panel bottom-left; the arm along logical +x
	// must run up the panel, and the arm along logical +y must run across it.
	if !framePixel(frame, 0, DisplayHeight-1) || !framePixel(frame, 0, DisplayHeight-3) || !framePixel(frame, 2, DisplayHeight-1) {
		t.Fatal("Rotate90 L-corner not in true quarter-turn position")
	}
	// A mirrored transpose would place a pixel at panel (2, DisplayHeight-3).
	if framePixel(frame, 2, DisplayHeight-3) {
		t.Fatal("Rotate90 is mirrored: opposite corner of the L is filled")
	}
}

func TestCanvasOrientationDrawsInLogicalBounds(t *testing.T) {
	canvas := NewCanvasOrientation(Rotate90)
	if canvas.Width() != DisplayHeight || canvas.Height() != DisplayWidth {
		t.Fatalf("portrait canvas = %dx%d", canvas.Width(), canvas.Height())
	}
	// Logical top-right is the panel top-left when the device is turned CW.
	canvas.setBlack(canvas.Width()-1, 0)
	frame := canvas.Frame(128)
	px, py := Rotate90.MapToPhysical(canvas.Width()-1, 0)
	if !framePixel(frame, px, py) {
		t.Fatalf("Rotate90 logical top-right not mapped to (%d,%d)", px, py)
	}
	if !framePixel(frame, 0, 0) {
		t.Fatal("Rotate90 logical top-right must land on panel origin")
	}
}
