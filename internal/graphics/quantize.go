package graphics

import (
	"image"
	"sort"
)

// histogramBits is the precision the color histogram is built at, per channel.
//
// Five bits collapses the 16 million possible colors into 32768 buckets. That
// is coarse enough to keep the histogram small and the median cut fast, and
// fine enough that the boxes it produces are a good guide to where the colors
// actually are - the averaging that follows works from the true pixel values,
// not the bucket, so the palette entries themselves are full precision.
const histogramBits = 5

// bucket is one entry of the color histogram: a count, and the running sum of
// the true colors that landed in it so an accurate average can be recovered.
type bucket struct {
	r, g, b uint8 // bucket representative, used for splitting
	sumR    uint64
	sumG    uint64
	sumB    uint64
	count   uint64
}

// histogram counts the colors in an image, ignoring transparent pixels.
func histogram(img *image.RGBA) []bucket {
	const shift = 8 - histogramBits
	index := make(map[uint16]int)
	var buckets []bucket

	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := img.Pix[(y-bounds.Min.Y)*img.Stride:]
		for x := 0; x < bounds.Dx(); x++ {
			p := row[x*4:]
			if p[3] < alphaThreshold {
				continue
			}
			r, g, b := p[0], p[1], p[2]
			key := uint16(r>>shift)<<(2*histogramBits) |
				uint16(g>>shift)<<histogramBits |
				uint16(b>>shift)

			i, ok := index[key]
			if !ok {
				i = len(buckets)
				index[key] = i
				buckets = append(buckets, bucket{r: r, g: g, b: b})
			}
			buckets[i].sumR += uint64(r)
			buckets[i].sumG += uint64(g)
			buckets[i].sumB += uint64(b)
			buckets[i].count++
		}
	}
	return buckets
}

// box is a region of color space during the median cut.
type box struct {
	buckets []bucket
}

// ranges returns the extent of the box along each channel.
func (b box) ranges() (dr, dg, db int) {
	minR, minG, minB := 255, 255, 255
	maxR, maxG, maxB := 0, 0, 0
	for _, e := range b.buckets {
		minR, maxR = min(minR, int(e.r)), max(maxR, int(e.r))
		minG, maxG = min(minG, int(e.g)), max(maxG, int(e.g))
		minB, maxB = min(minB, int(e.b)), max(maxB, int(e.b))
	}
	return maxR - minR, maxG - minG, maxB - minB
}

// weight is how many pixels the box accounts for.
func (b box) weight() uint64 {
	var n uint64
	for _, e := range b.buckets {
		n += e.count
	}
	return n
}

// average is the box's color, weighted by how often each entry occurs so that
// a dominant color is not pulled off target by a few stray pixels.
func (b box) average() [3]uint8 {
	var sr, sg, sb, n uint64
	for _, e := range b.buckets {
		sr += e.sumR
		sg += e.sumG
		sb += e.sumB
		n += e.count
	}
	if n == 0 {
		return [3]uint8{0, 0, 0}
	}
	return [3]uint8{uint8(sr / n), uint8(sg / n), uint8(sb / n)}
}

// medianCut reduces an image to a palette of at most maxColors entries.
//
// The classic algorithm: start with every color in one box, then repeatedly
// split whichever box covers the widest range of a channel, cutting it at the
// median so both halves hold a similar number of pixels. Splitting the widest
// box rather than the most populous is what keeps small but visually distinct
// regions - a red logo on a grey screenshot - from being averaged away.
func medianCut(img *image.RGBA, maxColors int) [][3]uint8 {
	buckets := histogram(img)
	if len(buckets) == 0 {
		return nil
	}
	if maxColors < 1 {
		maxColors = 1
	}

	boxes := []box{{buckets: buckets}}
	for len(boxes) < maxColors {
		idx, axis, extent := widestBox(boxes)
		if idx < 0 || extent == 0 {
			break // every remaining box holds a single color
		}
		a, b := splitBox(boxes[idx], axis)
		if len(a.buckets) == 0 || len(b.buckets) == 0 {
			break
		}
		boxes[idx] = a
		boxes = append(boxes, b)
	}

	palette := make([][3]uint8, 0, len(boxes))
	for _, bx := range boxes {
		palette = append(palette, bx.average())
	}
	return palette
}

// widestBox finds the splittable box with the largest extent along any single
// channel, and reports which channel that is.
func widestBox(boxes []box) (index, axis, extent int) {
	index, extent = -1, 0
	for i, bx := range boxes {
		if len(bx.buckets) < 2 {
			continue
		}
		dr, dg, db := bx.ranges()
		for a, d := range []int{dr, dg, db} {
			if d > extent {
				index, axis, extent = i, a, d
			}
		}
	}
	return index, axis, extent
}

// splitBox divides a box at the median of the given channel.
func splitBox(b box, axis int) (box, box) {
	channel := func(e bucket) uint8 {
		switch axis {
		case 0:
			return e.r
		case 1:
			return e.g
		}
		return e.b
	}
	sort.Slice(b.buckets, func(i, j int) bool {
		return channel(b.buckets[i]) < channel(b.buckets[j])
	})

	// Cut where half the pixels lie, not half the entries: a box holding one
	// very common color and many rare ones should not be split down the middle
	// of the rare ones.
	half := b.weight() / 2
	var running uint64
	cut := 1
	for i, e := range b.buckets {
		running += e.count
		if running >= half {
			cut = i + 1
			break
		}
	}
	if cut >= len(b.buckets) {
		cut = len(b.buckets) - 1
	}
	if cut < 1 {
		cut = 1
	}
	return box{buckets: b.buckets[:cut]}, box{buckets: b.buckets[cut:]}
}

// paletteLookup maps colors onto their nearest palette entry, caching results.
type paletteLookup struct {
	palette [][3]uint8
	// cache is indexed by a 15-bit color key. Dithering pushes each pixel to a
	// slightly different color, so an exact-match cache would rarely hit; at
	// five bits per channel neighbouring colors share an entry and the hit
	// rate is high, while the error introduced is below what dithering already
	// applies.
	cache []int16
}

func newPaletteLookup(palette [][3]uint8) *paletteLookup {
	cache := make([]int16, 1<<(3*histogramBits))
	for i := range cache {
		cache[i] = -1
	}
	return &paletteLookup{palette: palette, cache: cache}
}

// nearest returns the index of the closest palette entry to the given color.
func (p *paletteLookup) nearest(r, g, b int) int {
	const shift = 8 - histogramBits
	key := (r>>shift)<<(2*histogramBits) | (g>>shift)<<histogramBits | (b >> shift)
	if idx := p.cache[key]; idx >= 0 {
		return int(idx)
	}

	best, bestDist := 0, 1<<30
	for i, c := range p.palette {
		dr, dg, db := r-int(c[0]), g-int(c[1]), b-int(c[2])
		// Weighted to approximate how the eye divides its sensitivity between
		// the channels, the same weighting the color downgrade uses.
		d := 2*dr*dr + 4*dg*dg + 3*db*db
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	p.cache[key] = int16(best)
	return best
}
