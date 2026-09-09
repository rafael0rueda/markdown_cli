package graphics

import "testing"

func TestFit(t *testing.T) {
	// A 9x18 cell is what a common terminal font actually reports.
	cell := Constraints{CellWidth: 9, CellHeight: 18}

	tests := []struct {
		name             string
		imgW, imgH       int
		c                Constraints
		wantCols         int
		wantRows         int
		wantPxW, wantPxH int
	}{
		{
			name: "fits exactly", imgW: 90, imgH: 180,
			c:        Constraints{MaxCols: 20, CellWidth: 9, CellHeight: 18},
			wantCols: 10, wantRows: 10, wantPxW: 90, wantPxH: 180,
		},
		{
			name: "scaled down to the column limit", imgW: 900, imgH: 900,
			c:        Constraints{MaxCols: 10, CellWidth: 9, CellHeight: 18},
			wantCols: 10, wantRows: 5, wantPxW: 90, wantPxH: 90,
		},
		{
			name: "scaled down to the row limit", imgW: 180, imgH: 900,
			c:        Constraints{MaxCols: 100, MaxRows: 10, CellWidth: 9, CellHeight: 18},
			wantCols: 4, wantRows: 10, wantPxW: 36, wantPxH: 180,
		},
		{
			name: "small images are not enlarged", imgW: 16, imgH: 16,
			c:        Constraints{MaxCols: 80, CellWidth: 9, CellHeight: 18},
			wantCols: 2, wantRows: 1, wantPxW: 16, wantPxH: 16,
		},
		{
			name: "a single pixel still occupies a cell", imgW: 1, imgH: 1,
			c:        Constraints{MaxCols: 80, CellWidth: 9, CellHeight: 18},
			wantCols: 1, wantRows: 1, wantPxW: 1, wantPxH: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fit(tt.imgW, tt.imgH, tt.c)
			if got.Cols != tt.wantCols || got.Rows != tt.wantRows {
				t.Errorf("footprint = %dx%d cells, want %dx%d",
					got.Cols, got.Rows, tt.wantCols, tt.wantRows)
			}
			if got.PixelWidth != tt.wantPxW || got.PixelHeight != tt.wantPxH {
				t.Errorf("pixels = %dx%d, want %dx%d",
					got.PixelWidth, got.PixelHeight, tt.wantPxW, tt.wantPxH)
			}
		})
	}
	_ = cell
}

// TestFitPreservesAspectRatio is the property that matters visually: a
// distorted image is immediately obvious and always wrong.
func TestFitPreservesAspectRatio(t *testing.T) {
	sizes := [][2]int{{1600, 900}, {900, 1600}, {640, 480}, {1000, 37}, {37, 1000}}
	for _, size := range sizes {
		for _, maxCols := range []int{5, 20, 80, 200} {
			geo := Fit(size[0], size[1], Constraints{
				MaxCols: maxCols, MaxRows: 40, CellWidth: 9, CellHeight: 18,
			})
			want := float64(size[0]) / float64(size[1])
			got := float64(geo.PixelWidth) / float64(geo.PixelHeight)
			// Pixel dimensions are whole numbers, so each is up to half a
			// pixel off. That is negligible at 400 pixels and substantial at
			// 2, which is where an extreme aspect ratio ends up, so the bound
			// is derived from the result size rather than fixed.
			tolerance := want * (0.5/float64(geo.PixelWidth) + 0.5/float64(geo.PixelHeight))
			if diff := got - want; diff > tolerance || diff < -tolerance {
				t.Errorf("%dx%d in %d cols: aspect %.3f, want %.3f",
					size[0], size[1], maxCols, got, want)
			}
		}
	}
}

// TestFitNeverExceedsConstraints guards the invariant the layout depends on:
// the reserved rows must actually contain the image.
func TestFitNeverExceedsConstraints(t *testing.T) {
	for _, imgW := range []int{1, 37, 640, 4000} {
		for _, imgH := range []int{1, 37, 480, 4000} {
			for _, maxCols := range []int{1, 3, 40, 200} {
				for _, maxRows := range []int{0, 1, 5, 50} {
					c := Constraints{MaxCols: maxCols, MaxRows: maxRows, CellWidth: 9, CellHeight: 18}
					geo := Fit(imgW, imgH, c)
					if geo.Cols > maxCols {
						t.Fatalf("%dx%d in %d cols: got %d cols", imgW, imgH, maxCols, geo.Cols)
					}
					if maxRows > 0 && geo.Rows > maxRows {
						t.Fatalf("%dx%d in %d rows: got %d rows", imgW, imgH, maxRows, geo.Rows)
					}
					if geo.Cols < 1 || geo.Rows < 1 {
						t.Fatalf("%dx%d: degenerate footprint %dx%d", imgW, imgH, geo.Cols, geo.Rows)
					}
				}
			}
		}
	}
}

// TestFitIsIdempotent is what lets Encode recover the geometry Measure chose
// by passing the footprint back in as the constraint.
func TestFitIsIdempotent(t *testing.T) {
	for _, imgW := range []int{16, 240, 1600, 4000} {
		for _, imgH := range []int{16, 160, 900, 4000} {
			for _, maxCols := range []int{4, 30, 120} {
				c := Constraints{MaxCols: maxCols, MaxRows: 30, CellWidth: 9, CellHeight: 18}
				first := Fit(imgW, imgH, c)

				again := Fit(imgW, imgH, Constraints{
					MaxCols: first.Cols, MaxRows: first.Rows,
					CellWidth: 9, CellHeight: 18,
				})
				if again.PixelWidth != first.PixelWidth || again.PixelHeight != first.PixelHeight {
					t.Errorf("%dx%d in %d cols: re-fitting changed the pixels from %dx%d to %dx%d",
						imgW, imgH, maxCols,
						first.PixelWidth, first.PixelHeight,
						again.PixelWidth, again.PixelHeight)
				}
			}
		}
	}
}

func TestFitDefaultCellSize(t *testing.T) {
	// With no cell size reported the defaults stand in.
	got := Fit(80, 160, Constraints{MaxCols: 40})
	want := Fit(80, 160, Constraints{
		MaxCols: 40, CellWidth: DefaultCellWidth, CellHeight: DefaultCellHeight,
	})
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFitDegenerateInput(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {-1, 10}, {10, -1}, {0, 100}} {
		if got := (Fit(size[0], size[1], Constraints{MaxCols: 40})); got.Cols != 0 || got.Rows != 0 {
			t.Errorf("Fit(%d, %d) = %+v, want a zero footprint", size[0], size[1], got)
		}
	}
}
