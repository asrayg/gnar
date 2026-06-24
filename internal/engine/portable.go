package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/asrayg/gnar/internal/model"
	"github.com/asrayg/gnar/internal/store"
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
// Import is atomic and all-or-nothing: it parses and embeds every record first
// (honoring context cancellation), then commits the inserts in a single
// transaction. If anything fails — a malformed record, a cancelled context, a DB
// error — nothing is written and the returned ImportResult is zero. Embedding is
// done before the transaction opens, so the store lock is never held during
// network calls.
func (e *Engine) Import(ctx context.Context, r io.Reader) (ImportResult, error) {
	// Phase 1: decode + validate the whole stream (no DB writes).
	dec := json.NewDecoder(r) // no fixed token cap; tolerates blank lines
	var mems []model.Memory
	for {
		if err := ctx.Err(); err != nil {
			return ImportResult{}, err
		}
		var rec ExportRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return ImportResult{}, fmt.Errorf("record %d: %w", len(mems)+1, err)
		}
		if rec.Project == "" || rec.Content == "" {
			return ImportResult{}, fmt.Errorf("record %d: missing project or content", len(mems)+1)
		}
		kind := rec.Kind
		if kind == "" {
			kind = model.KindNote
		}
		if !model.ValidKind(kind) {
			return ImportResult{}, fmt.Errorf("record %d: invalid kind %q", len(mems)+1, kind)
		}
		created := rec.CreatedAt
		if created.IsZero() {
			created = e.now()
		}
		updated := rec.UpdatedAt
		if updated.IsZero() {
			updated = created
		}
		mems = append(mems, model.Memory{
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
		})
	}

	// Phase 2: embed every record up front (outside the DB lock). Best-effort —
	// on provider failure we fall back to keyword-only (nil vectors).
	vecs, embIDs := e.embedAll(ctx, mems)

	// Phase 3: insert everything atomically. On any error the tx rolls back and
	// the store is unchanged, so res is meaningful only on success.
	var res ImportResult
	err := e.store.RunInTx(func(tx *store.Tx) error {
		for i, m := range mems {
			exists, err := tx.ExistsSimilar(m.Project, m.Kind, m.Content)
			if err != nil {
				return err
			}
			if exists {
				res.Skipped++
				continue
			}
			if _, err := tx.Insert(m, vecs[i], embIDs[i]); err != nil {
				return fmt.Errorf("record %d: %w", i+1, err)
			}
			res.Added++
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

// embedAll embeds the text of each memory, returning per-record vectors and
// embedder ids. It batches to bound request size and degrades to keyword-only
// (nil vector, empty id) when the embedder fails.
func (e *Engine) embedAll(ctx context.Context, mems []model.Memory) ([][]float32, []string) {
	vecs := make([][]float32, len(mems))
	embIDs := make([]string, len(mems))
	const batch = 128
	for start := 0; start < len(mems); start += batch {
		end := start + batch
		if end > len(mems) {
			end = len(mems)
		}
		texts := make([]string, 0, end-start)
		for _, m := range mems[start:end] {
			texts = append(texts, embedText(m.Title, m.Content))
		}
		got, err := e.emb.Embed(ctx, texts)
		if err != nil || len(got) != len(texts) {
			continue // leave this batch keyword-only
		}
		id := e.emb.ID()
		for j := range got {
			vecs[start+j] = got[j]
			embIDs[start+j] = id
		}
	}
	return vecs, embIDs
}
