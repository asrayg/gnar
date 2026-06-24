package embed

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// Hash is a deterministic, dependency-free embedder using the signed
// feature-hashing trick. It needs no external service and works offline.
//
// Semantic quality is limited — it captures lexical overlap, not deep meaning —
// but cosine similarity between two Hash vectors tracks shared vocabulary, so
// out-of-the-box recall returns sensible results. It hashes unigrams plus
// character 3-grams so morphological variants ("run"/"running") partially
// overlap.
type Hash struct {
	D int
}

// NewHash returns a Hash embedder of dimension d.
func NewHash(d int) *Hash {
	if d <= 0 {
		d = 256
	}
	return &Hash{D: d}
}

func (h *Hash) Dim() int   { return h.D }
func (h *Hash) ID() string { return fmt.Sprintf("hash/bow3/%d", h.D) }

// Embed implements Embedder.
func (h *Hash) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = h.embedOne(t)
	}
	return out, nil
}

func (h *Hash) embedOne(text string) []float32 {
	v := make([]float32, h.D)
	for _, tok := range tokenize(text) {
		h.add(v, tok, 1.0)
		// character 3-grams give partial overlap for related word forms.
		// Operate on runes so multibyte (CJK/accented) tokens aren't cut mid-rune.
		rs := []rune(tok)
		if len(rs) >= 4 {
			padded := make([]rune, 0, len(rs)+2)
			padded = append(padded, '^')
			padded = append(padded, rs...)
			padded = append(padded, '$')
			for j := 0; j+3 <= len(padded); j++ {
				h.add(v, "#"+string(padded[j:j+3]), 0.5)
			}
		}
	}
	l2normalize(v)
	return v
}

// add hashes feature into a bucket with a sign derived from a second hash,
// accumulating weight w (the signed hashing trick reduces collision bias).
func (h *Hash) add(v []float32, feature string, w float32) {
	hh := fnv.New64a()
	_, _ = hh.Write([]byte(feature))
	sum := hh.Sum64()
	bucket := sum % uint64(h.D)
	if sum&0x8000000000000000 != 0 {
		v[bucket] -= w
	} else {
		v[bucket] += w
	}
}

// tokenize lowercases and splits on non-alphanumeric runes.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func l2normalize(v []float32) {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(n))
	for i := range v {
		v[i] *= inv
	}
}
