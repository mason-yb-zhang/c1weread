package c1device

// Orientation is how the device is held relative to the natural landscape panel.
// Rotate90 means the device is turned clockwise, Rotate270 counter-clockwise.
type Orientation uint8

const (
	Rotate0 Orientation = iota
	Rotate90
	Rotate180
	Rotate270
)

// Valid reports whether the value is one of the four quarter turns.
func (orientation Orientation) Valid() bool {
	return orientation <= Rotate270
}

// Portrait reports whether logical coordinates use the tall 152x296 layout.
func (orientation Orientation) Portrait() bool {
	return orientation == Rotate90 || orientation == Rotate270
}

// LogicalSize returns the drawing surface for this orientation.
func (orientation Orientation) LogicalSize() (width, height int) {
	if orientation.Portrait() {
		return DisplayHeight, DisplayWidth
	}
	return DisplayWidth, DisplayHeight
}

// MapToPhysical converts a logical pixel to panel coordinates.
// Rotate90/270 are true quarter-turns, not diagonal transposes.
func (orientation Orientation) MapToPhysical(x, y int) (int, int) {
	switch orientation {
	case Rotate90:
		// Device turned clockwise: content is rotated counter-clockwise.
		return y, DisplayHeight - 1 - x
	case Rotate180:
		return DisplayWidth - 1 - x, DisplayHeight - 1 - y
	case Rotate270:
		// Device turned counter-clockwise: content is rotated clockwise.
		return DisplayWidth - 1 - y, x
	default:
		return x, y
	}
}

// RemapKey converts a physical key to the key the user sees after rotating
// the device, so ↑ still means "toward the top of the visible page".
func (orientation Orientation) RemapKey(key Key) Key {
	switch orientation {
	case Rotate90:
		switch key {
		case KeyUp:
			return KeyRight
		case KeyRight:
			return KeyDown
		case KeyDown:
			return KeyLeft
		case KeyLeft:
			return KeyUp
		}
	case Rotate180:
		switch key {
		case KeyUp:
			return KeyDown
		case KeyDown:
			return KeyUp
		case KeyLeft:
			return KeyRight
		case KeyRight:
			return KeyLeft
		}
	case Rotate270:
		switch key {
		case KeyUp:
			return KeyLeft
		case KeyLeft:
			return KeyDown
		case KeyDown:
			return KeyRight
		case KeyRight:
			return KeyUp
		}
	}
	return key
}
