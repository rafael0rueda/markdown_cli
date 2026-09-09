package graphics

import (
	"image"

	"golang.org/x/image/draw"
)

// scaleTo resizes an image to the given pixel dimensions.
//
// CatmullRom is used rather than a cheaper filter because terminal images are
// small and heavily downscaled - often by a factor of five or more - which is
// exactly the case where a poor filter produces visible aliasing. The cost is
// irrelevant at these sizes.
func scaleTo(src image.Image, width, height int) *image.RGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// toRGBA returns the image as an *image.RGBA, converting only when needed.
func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst
}

// fitForTransfer scales an image down to approximately the size it will be
// displayed at, and reports the result.
//
// This bounds how much data is sent to the terminal. Handing a terminal a
// 4000-pixel photograph to draw in a 60-cell box means base64-encoding several
// megabytes to produce a few thousand pixels of output.
func fitForTransfer(src image.Image, geo Geometry) image.Image {
	bounds := src.Bounds()
	if bounds.Dx() <= geo.PixelWidth && bounds.Dy() <= geo.PixelHeight {
		return src
	}
	return scaleTo(src, geo.PixelWidth, geo.PixelHeight)
}
