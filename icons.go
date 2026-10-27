package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// makeIcons draws the app icon (a falling-then-rising balance line on green) at the
// sizes the PWA manifest and iOS home screen need. The artwork stays inside the
// central safe zone so the same image works as a maskable icon.
func makeIcons() map[string][]byte {
	out := map[string][]byte{}
	for name, size := range map[string]int{"icon-192.png": 192, "icon-512.png": 512, "apple-touch-icon.png": 180} {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		bg := color.RGBA{0x1f, 0x6f, 0x50, 0xff}
		for i := range img.Pix {
			img.Pix[i] = []uint8{bg.R, bg.G, bg.B, bg.A}[i%4]
		}
		s := float64(size)
		pts := [][2]float64{{0.25, 0.38}, {0.42, 0.55}, {0.55, 0.47}, {0.75, 0.66}}
		line(img, pts, s*0.045, color.RGBA{0xff, 0xff, 0xff, 0xff})
		base := [][2]float64{{0.25, 0.74}, {0.75, 0.74}}
		line(img, base, s*0.018, color.RGBA{0xb8, 0xdc, 0xcb, 0xff})
		var buf bytes.Buffer
		png.Encode(&buf, img)
		out[name] = buf.Bytes()
	}
	return out
}

// line strokes a polyline (points in 0..1 units) by stamping discs along each segment.
func line(img *image.RGBA, pts [][2]float64, width float64, c color.RGBA) {
	s := float64(img.Bounds().Dx())
	r := width / 2
	for i := 1; i < len(pts); i++ {
		x0, y0 := pts[i-1][0]*s, pts[i-1][1]*s
		x1, y1 := pts[i][0]*s, pts[i][1]*s
		steps := int(s)
		for k := 0; k <= steps; k++ {
			t := float64(k) / float64(steps)
			cx, cy := x0+(x1-x0)*t, y0+(y1-y0)*t
			for y := int(cy - r); y <= int(cy+r); y++ {
				for x := int(cx - r); x <= int(cx+r); x++ {
					if dx, dy := float64(x)-cx, float64(y)-cy; dx*dx+dy*dy <= r*r {
						img.SetRGBA(x, y, c)
					}
				}
			}
		}
	}
}
