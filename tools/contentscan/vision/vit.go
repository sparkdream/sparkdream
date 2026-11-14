package vision

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sync"
)

// ViT is a Vision Transformer image classifier in the layout timm's
// VisionTransformer saves (class token, learned position embedding, pre-norm
// blocks, class-token pooling): the architecture of the pinned image-safety
// model. Inference is plain float32 arithmetic in a fixed order, so every
// worker running the same weights computes the same scores, which the quorum
// rule relies on (docs/content-scanning.md §5.2). Explicit float32
// conversions keep the compiler from fusing multiply-adds on architectures
// that would.
type ViT struct {
	// Size is the square input side in pixels; Patch the patch side.
	Size, Patch int
	dim, heads  int
	classes     int

	patchW, patchB []float32 // [dim, 3*Patch*Patch], [dim]
	cls, pos       []float32 // [dim], [tokens*dim]
	blocks         []vitBlock
	normW, normB   []float32
	headW, headB   []float32 // [classes, dim], [classes]
}

type vitBlock struct {
	norm1W, norm1B []float32
	qkvW, qkvB     []float32 // [3*dim, dim]
	projW, projB   []float32 // [dim, dim]
	norm2W, norm2B []float32
	fc1W, fc1B     []float32 // [hidden, dim]
	fc2W, fc2B     []float32 // [dim, hidden]
}

// layerNormEps is timm's LayerNorm epsilon for Vision Transformers.
const layerNormEps = 1e-6

// headDim is the per-head width of every standard ViT (tiny 192/3, small
// 384/6, base 768/12). The weights file does not record the head count.
const headDim = 64

// NewViT builds the model from safetensors bytes. Every dimension is read
// from the tensor shapes and checked against the others.
func NewViT(weights []byte) (*ViT, error) {
	ts, err := parseSafetensors(weights)
	if err != nil {
		return nil, err
	}
	get := func(name string, shape ...int) ([]float32, error) {
		t, ok := ts[name]
		if !ok {
			return nil, fmt.Errorf("vision: weights have no tensor %q", name)
		}
		if len(shape) > 0 {
			if len(t.shape) != len(shape) {
				return nil, fmt.Errorf("vision: tensor %q has shape %v", name, t.shape)
			}
			for i, d := range shape {
				if d >= 0 && t.shape[i] != d {
					return nil, fmt.Errorf("vision: tensor %q has shape %v", name, t.shape)
				}
			}
		}
		return t.data, nil
	}

	pe, ok := ts["patch_embed.proj.weight"]
	if !ok || len(pe.shape) != 4 || pe.shape[1] != 3 || pe.shape[2] != pe.shape[3] || pe.shape[2] == 0 {
		return nil, fmt.Errorf("vision: weights are not a Vision Transformer (patch embedding)")
	}
	m := &ViT{dim: pe.shape[0], Patch: pe.shape[2], patchW: pe.data}
	if m.dim == 0 || m.dim%headDim != 0 {
		return nil, fmt.Errorf("vision: unsupported embedding width %d", m.dim)
	}
	m.heads = m.dim / headDim
	pos, ok := ts["pos_embed"]
	if !ok || len(pos.shape) != 3 || pos.shape[2] != m.dim || pos.shape[1] < 2 {
		return nil, fmt.Errorf("vision: weights are not a Vision Transformer (position embedding)")
	}
	grid := int(math.Round(math.Sqrt(float64(pos.shape[1] - 1))))
	if grid*grid != pos.shape[1]-1 {
		return nil, fmt.Errorf("vision: position embedding is not a square grid plus a class token")
	}
	m.Size, m.pos = grid*m.Patch, pos.data
	if m.patchB, err = get("patch_embed.proj.bias", m.dim); err != nil {
		return nil, err
	}
	if m.cls, err = get("cls_token", 1, 1, m.dim); err != nil {
		return nil, err
	}
	for i := 0; ; i++ {
		p := fmt.Sprintf("blocks.%d.", i)
		if _, ok := ts[p+"attn.qkv.weight"]; !ok {
			break
		}
		for _, unsupported := range []string{"ls1.gamma", "attn.q_norm.weight", "attn.q_bias"} {
			if _, ok := ts[p+unsupported]; ok {
				return nil, fmt.Errorf("vision: unsupported Vision Transformer variant (%s)", p+unsupported)
			}
		}
		var b vitBlock
		fc1, ok := ts[p+"mlp.fc1.weight"]
		if !ok || len(fc1.shape) != 2 || fc1.shape[1] != m.dim || fc1.shape[0] == 0 {
			return nil, fmt.Errorf("vision: tensor %q is missing or misshapen", p+"mlp.fc1.weight")
		}
		hidden := fc1.shape[0]
		b.fc1W = fc1.data
		for _, f := range []struct {
			dst   *[]float32
			name  string
			shape []int
		}{
			{&b.norm1W, "norm1.weight", []int{m.dim}}, {&b.norm1B, "norm1.bias", []int{m.dim}},
			{&b.qkvW, "attn.qkv.weight", []int{3 * m.dim, m.dim}}, {&b.qkvB, "attn.qkv.bias", []int{3 * m.dim}},
			{&b.projW, "attn.proj.weight", []int{m.dim, m.dim}}, {&b.projB, "attn.proj.bias", []int{m.dim}},
			{&b.norm2W, "norm2.weight", []int{m.dim}}, {&b.norm2B, "norm2.bias", []int{m.dim}},
			{&b.fc1B, "mlp.fc1.bias", []int{hidden}},
			{&b.fc2W, "mlp.fc2.weight", []int{m.dim, hidden}}, {&b.fc2B, "mlp.fc2.bias", []int{m.dim}},
		} {
			if *f.dst, err = get(p+f.name, f.shape...); err != nil {
				return nil, err
			}
		}
		m.blocks = append(m.blocks, b)
	}
	if len(m.blocks) == 0 {
		return nil, fmt.Errorf("vision: weights have no transformer blocks")
	}
	for _, unsupported := range []string{"norm_pre.weight", "fc_norm.weight", "reg_token", "dist_token"} {
		if _, ok := ts[unsupported]; ok {
			return nil, fmt.Errorf("vision: unsupported Vision Transformer variant (%s)", unsupported)
		}
	}
	if m.normW, err = get("norm.weight", m.dim); err != nil {
		return nil, err
	}
	if m.normB, err = get("norm.bias", m.dim); err != nil {
		return nil, err
	}
	if m.headW, err = get("head.weight", -1, m.dim); err != nil {
		return nil, err
	}
	m.classes = len(m.headW) / m.dim
	if m.headB, err = get("head.bias", m.classes); err != nil {
		return nil, err
	}
	return m, nil
}

// Classes is the number of output classes.
func (m *ViT) Classes() int { return m.classes }

// Logits runs the model on one normalised image: 3*Size*Size values in
// channel, row, column order.
func (m *ViT) Logits(ctx context.Context, chw []float32) ([]float32, error) {
	side := m.Size
	if len(chw) != 3*side*side {
		return nil, fmt.Errorf("vision: input has %d values, want %d", len(chw), 3*side*side)
	}
	grid, p, d := side/m.Patch, m.Patch, m.dim
	n := grid*grid + 1
	x := make([]float32, n*d)
	copy(x[:d], m.cls)
	patchLen := 3 * p * p
	parallel(grid*grid, func(lo, hi int) {
		patch := make([]float32, patchLen)
		for t := lo; t < hi; t++ {
			py, px := t/grid, t%grid
			for c := 0; c < 3; c++ {
				for i := 0; i < p; i++ {
					row := c*side*side + (py*p+i)*side + px*p
					copy(patch[(c*p+i)*p:(c*p+i+1)*p], chw[row:row+p])
				}
			}
			affine(x[(t+1)*d:(t+2)*d], patch, m.patchW, m.patchB)
		}
	})
	for i := range x {
		x[i] += m.pos[i]
	}

	h := make([]float32, n*d)
	qkv := make([]float32, n*3*d)
	att := make([]float32, n*d)
	hidden := len(m.blocks[0].fc1B)
	for bi := range m.blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b := &m.blocks[bi]

		parallel(n, func(lo, hi int) {
			for t := lo; t < hi; t++ {
				layerNorm(h[t*d:(t+1)*d], x[t*d:(t+1)*d], b.norm1W, b.norm1B)
				affine(qkv[t*3*d:(t+1)*3*d], h[t*d:(t+1)*d], b.qkvW, b.qkvB)
			}
		})
		scale := float32(1 / math.Sqrt(headDim))
		parallel(n, func(lo, hi int) {
			w := make([]float32, n)
			for t := lo; t < hi; t++ {
				for hd := 0; hd < m.heads; hd++ {
					q := qkv[t*3*d+hd*headDim : t*3*d+(hd+1)*headDim]
					maxv := float32(math.Inf(-1))
					for j := 0; j < n; j++ {
						k := qkv[j*3*d+d+hd*headDim : j*3*d+d+(hd+1)*headDim]
						w[j] = float32(dot(q, k) * scale)
						if w[j] > maxv {
							maxv = w[j]
						}
					}
					var sum float32
					for j := range w {
						w[j] = float32(math.Exp(float64(w[j] - maxv)))
						sum += w[j]
					}
					out := att[t*d+hd*headDim : t*d+(hd+1)*headDim]
					for i := range out {
						out[i] = 0
					}
					for j := 0; j < n; j++ {
						wj := w[j] / sum
						v := qkv[j*3*d+2*d+hd*headDim : j*3*d+2*d+(hd+1)*headDim]
						for i := range out {
							out[i] += float32(wj * v[i])
						}
					}
				}
			}
		})
		parallel(n, func(lo, hi int) {
			tmp := make([]float32, d)
			mid := make([]float32, hidden)
			for t := lo; t < hi; t++ {
				xt := x[t*d : (t+1)*d]
				affine(tmp, att[t*d:(t+1)*d], b.projW, b.projB)
				for i := range xt {
					xt[i] += tmp[i]
				}
				layerNorm(tmp, xt, b.norm2W, b.norm2B)
				affine(mid, tmp, b.fc1W, b.fc1B)
				for i, v := range mid {
					mid[i] = gelu(v)
				}
				affine(tmp, mid, b.fc2W, b.fc2B)
				for i := range xt {
					xt[i] += tmp[i]
				}
			}
		})
	}

	pooled := make([]float32, d)
	layerNorm(pooled, x[:d], m.normW, m.normB)
	logits := make([]float32, m.classes)
	affine(logits, pooled, m.headW, m.headB)
	return logits, nil
}

// softmax turns logits into probabilities.
func softmax(logits []float32) []float64 {
	maxv := math.Inf(-1)
	for _, v := range logits {
		maxv = math.Max(maxv, float64(v))
	}
	out := make([]float64, len(logits))
	var sum float64
	for i, v := range logits {
		out[i] = math.Exp(float64(v) - maxv)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// affine computes out = w·in + b for a row-major weight matrix [len(out), len(in)].
func affine(out, in, w, b []float32) {
	n := len(in)
	for o := range out {
		out[o] = dot(w[o*n:(o+1)*n], in) + b[o]
	}
}

// dot is the inner product, summed in four fixed lanes.
func dot(a, b []float32) float32 {
	b = b[:len(a)]
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		s0 += float32(a[i] * b[i])
		s1 += float32(a[i+1] * b[i+1])
		s2 += float32(a[i+2] * b[i+2])
		s3 += float32(a[i+3] * b[i+3])
	}
	for ; i < len(a); i++ {
		s0 += float32(a[i] * b[i])
	}
	return (s0 + s1) + (s2 + s3)
}

func layerNorm(out, in, w, b []float32) {
	var mean float64
	for _, v := range in {
		mean += float64(v)
	}
	mean /= float64(len(in))
	var variance float64
	for _, v := range in {
		dv := float64(v) - mean
		variance += dv * dv
	}
	inv := 1 / math.Sqrt(variance/float64(len(in))+layerNormEps)
	for i, v := range in {
		out[i] = float32((float64(v)-mean)*inv)*w[i] + b[i]
	}
}

// gelu is the exact (erf) GELU timm's Vision Transformers use.
func gelu(x float32) float32 {
	return float32(0.5 * float64(x) * (1 + math.Erf(float64(x)/math.Sqrt2)))
}

// parallel splits [0, n) across the CPUs. Each index is computed by exactly
// one goroutine with the same arithmetic, so the result does not depend on
// how many there are.
func parallel(n int, fn func(lo, hi int)) {
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		fn(0, n)
		return
	}
	var wg sync.WaitGroup
	chunk := (n + workers - 1) / workers
	for lo := 0; lo < n; lo += chunk {
		hi := lo + chunk
		if hi > n {
			hi = n
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			fn(lo, hi)
		}(lo, hi)
	}
	wg.Wait()
}
