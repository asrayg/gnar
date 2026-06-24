package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Ollama is an embedder for Ollama's native /api/embed endpoint.
type Ollama struct {
	BaseURL string // e.g. http://localhost:11434
	Model   string
	Dims    int
	HTTP    *http.Client
}

func (c *Ollama) Dim() int   { return c.Dims }
func (c *Ollama) ID() string { return fmt.Sprintf("ollama/%s/%d", c.Model, c.Dims) }

type ollamaReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaResp struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error"`
}

// Embed implements Embedder.
func (c *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(ollamaReq{Model: c.Model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	var out ollamaResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("ollama response (status %d): %s", resp.StatusCode, truncate(raw, 200))
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		msg := out.Error
		if msg == "" {
			msg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("ollama embed error: %s", msg)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama: asked for %d, got %d", len(texts), len(out.Embeddings))
	}
	return out.Embeddings, nil
}
