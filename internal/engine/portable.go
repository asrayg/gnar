package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/asraygopa/gnar/internal/model"
	"github.com/asraygopa/gnar/internal/store"
)

// ExportRecord is the portable, embedder-independent form of a memory. Embeddings
// are intentionally omitted — they are re-computed on import so an export can move
// between machines and embedding providers.
type ExportRecord struct {
	Project   string         `json:"project"`
	Kind      model.Kind     `json:"kind"`
	Title     string         `json:"title,omitempty"`
	Content   string         `json:"content"`
	Tags      []string       `json:"tags,omitempty"`
	Files     []string       `json:"files,omitempty"`
	Source    string         `json:"source,omitempty"`
	Session   string         `json:"session,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
	Pinned    bool           `json:"pinned,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Export writes every non-archived memory as JSON Lines (one record per line)
// and returns the count written.
func (e *Engine) Export(w io.Writer) (int, error) {
	rows, err := e.store.Candidates(store.Query{IncludeArchived: false, OrderBy: "created_asc"})
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	n := 0
	for _, r := range rows {
		m := r.Memory
		rec := ExportRecord{
			Project:   m.Project,
			Kind:      m.Kind,
			Title:     m.Title,
			Content:   m.Content,
			Tags:      m.Tags,
			Files:     m.Files,
			Source:    m.Source,
			Session:   m.Session,
			Meta:      m.Meta,
			Pinned:    m.Pinned,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		}
		if err := enc.Encode(rec); err != nil {
			return n, err
		}
		n++
	}
	return n, bw.Flush()
}

// ImportResult summarizes an import run.
type ImportResult struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

// Import reads JSON records produced by Export (JSON Lines, or any whitespace-
// separated JSON stream) and inserts them, re-embedding each. Records whose
// (project, kind, content) already exist — archived or not — are skipped, so
// importing the same file twice is idempotent and a soft-deleted memory is not
// resurrected as a duplicate.
//
// Import is not transactional: if it returns an error mid-stream, records already
// inserted remain (re-running is safe thanks to the dedup above). The returned
// ImportResult is always populated with progress so far, even on error. The
// context is honored — cancellation aborts cleanly.
func (e *Engine) Import(ctx context.Context, r io.Reader) (ImportResult, error) {
	var res ImportResult
	dec := json.NewDecoder(r) // no fixed token cap; tolerates blank lines
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var rec ExportRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return res, fmt.Errorf("record %d: %w", res.Added+res.Skipped+1, err)
		}
		if rec.Project == "" || rec.Content == "" {
			return res, fmt.Errorf("record %d: missing project or content", res.Added+res.Skipped+1)
		}
		kind := rec.Kind
		if kind == "" {
			kind = model.KindNote
		}
		if !model.ValidKind(kind) {
			return res, fmt.Errorf("record %d: invalid kind %q", res.Added+res.Skipped+1, kind)
		}
		exists, err := e.store.ExistsSimilar(rec.Project, kind, rec.Content)
		if err != nil {
			return res, err
		}
		if exists {
			res.Skipped++
			continue
		}
		now := e.now()
		created := rec.CreatedAt
		if created.IsZero() {
			created = now
		}
		updated := rec.UpdatedAt
		if updated.IsZero() {
			updated = created
		}
		m := model.Memory{
			Project:   rec.Project,
			Kind:      kind,
			Title:     rec.Title,
			Content:   rec.Content,
			Tags:      normalizeTags(rec.Tags),
			Files:     rec.Files,
			Source:    rec.Source,
			Session:   rec.Session,
			Meta:      rec.Meta,
			Pinned:    rec.Pinned,
			CreatedAt: created,
			UpdatedAt: updated,
		}
		vec, embID := e.tryEmbed(ctx, embedText(m.Title, m.Content))
		if _, err := e.store.Insert(m, vec, embID); err != nil {
			return res, fmt.Errorf("record %d: %w", res.Added+res.Skipped+1, err)
		}
		res.Added++
	}
	return res, nil
}
