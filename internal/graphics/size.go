package graphics

import "math"

// Default cell dimensions in pixels, used when the terminal will not say.
//
// Getting this wrong distorts nothing - the aspect ratio is preserved in pixel
// space either way - but it does change how many rows an image is given, so a
// bad guess leaves a gap under the image or lets it overlap the text below.
// Most terminal fonts sit near a 1:2 ratio, and these are the numbers a
// terminal at a common size and font actually reports.
const (
	DefaultCellWidth  = 8
	DefaultCellHeight = 16
)

// Constraints bound how much of the screen an image may take.
type Constraints struct {
	// MaxCols is the width available in cells. Required.
	MaxCols int
	// MaxRows caps the height in cells. Zero means no limit.
	MaxRows int
	// CellWidth and CellHeight are the pixel size of one cell. Zero means the
	// terminal did not report it and the defaults are used.
	CellWidth, CellHeight int
}

// cellSize returns the cell dimensions to work with, substituting defaults.
func (c Constraints) cellSize() (width, height int) {
	width, height = c.CellWidth, c.CellHeight
	if width <= 0 {
		width = DefaultCellWidth
	}
	if height <= 0 {
		height = DefaultCellHeight
	}
	return width, height
}

// Geometry is where an image ends up: its footprint on the cell grid, and the
// pixel size it should be drawn at.
type Geometry struct {
	Cols, Rows              int
	PixelWidth, PixelHeight int
}

// Fit scales an image to sit inside the given constraints, preserving its
// aspect ratio.
//
// Images are never enlarged. Upscaling a 16-pixel icon to fill the column
// would be blurry and is not what anyone means by putting an icon in a
// document; showing it at its own size is both honest and cheaper.
func Fit(imgWidth, imgHeight int, c Constraints) Geometry {
	if imgWidth <= 0 || imgHeight <= 0 {
		return Geometry{}
	}
	cellW, cellH := c.cellSize()

	maxCols := c.MaxCols
	if maxCols < 1 {
		maxCols = 1
	}

	scale := 1.0
	if limit := float64(maxCols*cellW) / float64(imgWidth); limit < scale {
		scale = limit
	}
	if c.MaxRows > 0 {
		if limit := float64(c.MaxRows*cellH) / float64(imgHeight); limit < scale {
			scale = limit
		}
	}

	pxWidth := int(math.Round(float64(imgWidth) * scale))
	pxHeight := int(math.Round(float64(imgHeight) * scale))
	// Rounding can take a very small image to nothing; a single pixel is the
	// floor since there is no such thing as a zero-pixel image.
	pxWidth = max(pxWidth, 1)
	pxHeight = max(pxHeight, 1)

	// The footprint is however many whole cells the pixels cover. A partly
	// filled final row still has to be reserved, or the text below would be
	// drawn over the bottom of the image.
	geo := Geometry{
		Cols:        ceilDiv(pxWidth, cellW),
		Rows:        ceilDiv(pxHeight, cellH),
		PixelWidth:  pxWidth,
		PixelHeight: pxHeight,
	}

	// Rounding up to whole cells can push the footprint one column past the
	// limit; clamping is safe because the image is drawn inside the box.
	if geo.Cols > maxCols {
		geo.Cols = maxCols
	}
	if c.MaxRows > 0 && geo.Rows > c.MaxRows {
		geo.Rows = c.MaxRows
	}
	return geo
}

// ceilDiv divides a by b, rounding up. Both are assumed positive.
func ceilDiv(a, b int) int {
	if b <= 0 {
		return a
	}
	return (a + b - 1) / b
}
