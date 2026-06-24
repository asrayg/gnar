// Package embed provides a pluggable embeddings layer. The default "hash"
// provider needs no external service, so semantic recall works out of the box;
// "openai" and "ollama" providers give real semantic quality when configured.
package embed

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Embedder turns text into fixed-dimension vectors.
type Embedder interface {
	// Embed returns one vector per input text, in the same order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim is the fixed output dimension.
	Dim() int
	// ID uniquely identifies this embedder+model+dim, e.g.
	// "openai/text-embedding-3-small/1536". Used to detect when stored vectors
	// were produced by a different embedder and must be re-indexed.
	ID() string
}

// Options configures New.
type Options struct {
	Provider string // "hash" | "openai" | "ollama"
	Model    string
	BaseURL  string
	APIKey   string
	Dim      int
}

// New constructs an Embedder from options. Unknown providers fall back to hash.
func New(o Options) (Embedder, error) {
	switch strings.ToLower(o.Provider) {
	case "", "hash", "local":
		dim := o.Dim
		if dim <= 0 {
			dim = 256
		}
		return NewHash(dim), nil

	case "openai", "openai-compatible", "openai_compatible":
		model := o.Model
		if model == "" {
			model = "text-embedding-3-small"
		}
		base := o.BaseURL
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		dim := o.Dim
		if dim <= 0 {
			dim = defaultOpenAIDim(model)
		}
		return &OpenAI{
			BaseURL: strings.TrimRight(base, "/"),
			APIKey:  o.APIKey,
			Model:   model,
			Dims:    dim,
			HTTP:    &http.Client{Timeout: 60 * time.Second},
		}, nil

	case "ollama":
		model := o.Model
		if model == "" {
			model = "nomic-embed-text"
		}
		base := o.BaseURL
		if base == "" {
			base = "http://localhost:11434"
		}
		dim := o.Dim
		if dim <= 0 {
			dim = 768 // nomic-embed-text
		}
		return &Ollama{
			BaseURL: strings.TrimRight(base, "/"),
			Model:   model,
			Dims:    dim,
			HTTP:    &http.Client{Timeout: 60 * time.Second},
		}, nil

	default:
		return nil, fmt.Errorf("unknown embedding provider %q (want hash, openai, or ollama)", o.Provider)
	}
}

func defaultOpenAIDim(model string) int {
	switch model {
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-3-small", "text-embedding-ada-002":
		return 1536
	default:
		return 1536
	}
}

// EmbedOne is a convenience wrapper for embedding a single string.
func EmbedOne(ctx context.Context, e Embedder, text string) ([]float32, error) {
	vs, err := e.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vs) != 1 {
		return nil, fmt.Errorf("embedder returned %d vectors for 1 input", len(vs))
	}
	return vs[0], nil
}
