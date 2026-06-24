package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// OpenAI is an embedder for any OpenAI-compatible /v1/embeddings endpoint
// (OpenAI, LM Studio, llama.cpp, vLLM, Ollama's /v1, ...).
type OpenAI struct {
	BaseURL string // e.g. https://api.openai.com/v1
	APIKey  string
	Model   string
	Dims    int
	HTTP    *http.Client
}

func (c *OpenAI) Dim() int { return c.Dims }
func (c *OpenAI) ID() string {
	return fmt.Sprintf("openai/%s/%d", c.Model, c.Dims)
}

type openAIReq struct {
	Input          []string `json:"input"`
	Model          string   `json:"model"`
	Dimensions     *int     `json:"dimensions,omitempty"`
	EncodingFormat string   `json:"encoding_format"`
}

type openAIResp struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed implements Embedder.
func (c *OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	reqBody := openAIReq{Input: texts, Model: c.Model, EncodingFormat: "float"}
	// Only OpenAI's own v3 models honor `dimensions`; send it only when targeting
	// the canonical OpenAI endpoint to stay compatible with local servers.
	if c.Dims > 0 && isOpenAIHost(c.BaseURL) && supportsDimensions(c.Model) {
		d := c.Dims
		reqBody.Dimensions = &d
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	var out openAIResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("embeddings response (status %d): %s", resp.StatusCode, truncate(raw, 200))
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("status %d", resp.StatusCode)
		if out.Error != nil {
			msg = out.Error.Message
		}
		return nil, fmt.Errorf("embeddings error: %s", msg)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: asked for %d, got %d", len(texts), len(out.Data))
	}
	// Re-order by Index to be safe.
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].Index < out.Data[j].Index })
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}

func isOpenAIHost(base string) bool {
	return bytes.Contains([]byte(base), []byte("api.openai.com"))
}

func supportsDimensions(model string) bool {
	return model == "text-embedding-3-small" || model == "text-embedding-3-large"
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
