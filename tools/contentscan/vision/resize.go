package vision

import "math"

// rgb is an 8-bit RGB raster, 3 bytes per pixel, rows top to bottom.
type rgb struct {
	w, h int
	pix  []byte
}

// Bicubic resampling as Pillow does it (ImagingResample, 8 bits per channel):
// the filter support widens when shrinking, so a downscale is antialiased,
// and coefficients are fixed-point, so the result is exact integer arithmetic
// and identical on every machine. The pinned model was trained and evaluated
// on images prepared this way; matching it keeps the scores the model card
// describes.

const resampleBits = 32 - 8 - 2

func bicubic(x float64) float64 {
	const a = -0.5
	if x < 0 {
		x = -x
	}
	if x < 1 {
		return ((a+2)*x-(a+3))*x*x + 1
	}
	if x < 2 {
		return (((x-5)*x+8)*x - 4) * a
	}
	return 0
}

// coeffs returns, for each of out positions, the first input index and the
// fixed-point weights of the inputs it reads.
func coeffs(in, out int) (bounds []int, kk []int32, ksize int) {
	scale := float64(in) / float64(out)
	filterscale := math.Max(scale, 1)
	support := 2 * filterscale
	ksize = int(math.Ceil(support))*2 + 1
	bounds = make([]int, out*2)
	kk = make([]int32, out*ksize)
	w := make([]float64, ksize)
	for xx := 0; xx < out; xx++ {
		center := (float64(xx) + 0.5) * scale
		xmin := int(center - support + 0.5)
		if xmin < 0 {
			xmin = 0
		}
		xmax := int(center + support + 0.5)
		if xmax > in {
			xmax = in
		}
		n := xmax - xmin
		var sum float64
		for x := 0; x < n; x++ {
			w[x] = bicubic((float64(x+xmin) - center + 0.5) / filterscale)
			sum += w[x]
		}
		for x := 0; x < n; x++ {
			v := w[x]
			if sum != 0 {
				v /= sum
			}
			if v < 0 {
				kk[xx*ksize+x] = int32(-0.5 + v*(1<<resampleBits))
			} else {
				kk[xx*ksize+x] = int32(0.5 + v*(1<<resampleBits))
			}
		}
		bounds[xx*2], bounds[xx*2+1] = xmin, n
	}
	return bounds, kk, ksize
}

func clip8(v int32) byte {
	v >>= resampleBits
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// resize resamples src to w x h: a horizontal pass, then a vertical one.
func resize(src rgb, w, h int) rgb {
	cur := src
	if w != src.w {
		bounds, kk, ksize := coeffs(src.w, w)
		dst := rgb{w: w, h: src.h, pix: make([]byte, w*src.h*3)}
		parallel(src.h, func(lo, hi int) {
			for y := lo; y < hi; y++ {
				row := src.pix[y*src.w*3:]
				for xx := 0; xx < w; xx++ {
					xmin, n := bounds[xx*2], bounds[xx*2+1]
					k := kk[xx*ksize:]
					s0, s1, s2 := int32(1)<<(resampleBits-1), int32(1)<<(resampleBits-1), int32(1)<<(resampleBits-1)
					for x := 0; x < n; x++ {
						p := row[(xmin+x)*3:]
						s0 += int32(p[0]) * k[x]
						s1 += int32(p[1]) * k[x]
						s2 += int32(p[2]) * k[x]
					}
					o := dst.pix[(y*w+xx)*3:]
					o[0], o[1], o[2] = clip8(s0), clip8(s1), clip8(s2)
				}
			}
		})
		cur = dst
	}
	if h != cur.h {
		bounds, kk, ksize := coeffs(cur.h, h)
		dst := rgb{w: cur.w, h: h, pix: make([]byte, cur.w*h*3)}
		in := cur
		parallel(h, func(lo, hi int) {
			for yy := lo; yy < hi; yy++ {
				ymin, n := bounds[yy*2], bounds[yy*2+1]
				k := kk[yy*ksize:]
				for x := 0; x < in.w; x++ {
					s0, s1, s2 := int32(1)<<(resampleBits-1), int32(1)<<(resampleBits-1), int32(1)<<(resampleBits-1)
					for y := 0; y < n; y++ {
						p := in.pix[((ymin+y)*in.w+x)*3:]
						s0 += int32(p[0]) * k[y]
						s1 += int32(p[1]) * k[y]
						s2 += int32(p[2]) * k[y]
					}
					o := dst.pix[(yy*in.w+x)*3:]
					o[0], o[1], o[2] = clip8(s0), clip8(s1), clip8(s2)
				}
			}
		})
		cur = dst
	}
	return cur
}

// crop copies the side x side square at (x0, y0).
func crop(src rgb, x0, y0, side int) rgb {
	dst := rgb{w: side, h: side, pix: make([]byte, side*side*3)}
	for y := 0; y < side; y++ {
		copy(dst.pix[y*side*3:(y+1)*side*3], src.pix[((y0+y)*src.w+x0)*3:])
	}
	return dst
}

// normalise converts a raster to the model's input: channel, row, column
// order, scaled to [0, 1] and shifted by mean 0.5 and std 0.5.
func normalise(img rgb) []float32 {
	n := img.w * img.h
	out := make([]float32, 3*n)
	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			out[c*n+i] = (float32(img.pix[i*3+c])/255 - 0.5) / 0.5
		}
	}
	return out
}
