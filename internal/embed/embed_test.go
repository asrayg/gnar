package embed

import (
	"context"
	"testing"
)

func TestHashDeterministicAndDim(t *testing.T) {
	h := NewHash(128)
	if h.Dim() != 128 {
		t.Fatalf("dim = %d, want 128", h.Dim())
	}
	ctx := context.Background()
	a, err := EmbedOne(ctx, h, "the quick brown fox")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := EmbedOne(ctx, h, "the quick brown fox")
	if len(a) != 128 {
		t.Fatalf("len = %d, want 128", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("hash embedding not deterministic at %d", i)
		}
	}
}

func TestHashSimilarityOrdering(t *testing.T) {
	h := NewHash(512)
	ctx := context.Background()
	vecs, err := h.Embed(ctx, []string{
		"database migration with sqlite and wal mode",
		"sqlite database migrations and wal journaling",
		"the weather is sunny and warm today",
	})
	if err != nil {
		t.Fatal(err)
	}
	simRelated := Cosine(vecs[0], vecs[1])
	simUnrelated := Cosine(vecs[0], vecs[2])
	if simRelated <= simUnrelated {
		t.Fatalf("related (%.3f) should score higher than unrelated (%.3f)", simRelated, simUnrelated)
	}
}

func TestCosineEdgeCases(t *testing.T) {
	if c := Cosine(nil, nil); c != 0 {
		t.Fatalf("empty cosine = %v, want 0", c)
	}
	if c := Cosine([]float32{1, 0}, []float32{1, 0, 0}); c != 0 {
		t.Fatalf("mismatched length cosine = %v, want 0", c)
	}
	if c := Cosine([]float32{0, 0}, []float32{1, 1}); c != 0 {
		t.Fatalf("zero vector cosine = %v, want 0", c)
	}
	if c := Cosine([]float32{1, 1}, []float32{1, 1}); c < 0.999 {
		t.Fatalf("identical cosine = %v, want ~1", c)
	}
}

func TestNewProviders(t *testing.T) {
	hash, err := New(Options{Provider: "hash", Dim: 64})
	if err != nil || hash.Dim() != 64 {
		t.Fatalf("hash provider: %v dim=%d", err, hash.Dim())
	}
	oa, err := New(Options{Provider: "openai"})
	if err != nil || oa.Dim() != 1536 {
		t.Fatalf("openai default dim = %d, want 1536 (%v)", oa.Dim(), err)
	}
	ol, err := New(Options{Provider: "ollama"})
	if err != nil || ol.Dim() != 768 {
		t.Fatalf("ollama default dim = %d, want 768 (%v)", ol.Dim(), err)
	}
	if _, err := New(Options{Provider: "bogus"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
