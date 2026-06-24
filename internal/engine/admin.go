package engine

import (
	"context"
	"path/filepath"

	"github.com/asraygopa/gnar/internal/model"
)

// Stats returns a summary of the store and embedder health.
func (e *Engine) Stats() (model.Stats, error) {
	total, byKind, projects, err := e.store.Counts()
	if err != nil {
		return model.Stats{}, err
	}
	storedID, _, _ := e.store.GetMeta("embed_id")
	top, err := e.store.TopProjects(10)
	if err != nil {
		return model.Stats{}, err
	}
	for i := range top {
		top[i].Name = filepath.Base(top[i].Project)
	}
	return model.Stats{
		DBPath:     e.store.Path(),
		Total:      total,
		ByKind:     byKind,
		Projects:   projects,
		Embedder:   e.emb.ID(),
		StoredDim:  e.emb.Dim(),
		EmbedMatch: storedID == "" || storedID == e.emb.ID(),
		TopProj:    top,
	}, nil
}

// Reindex re-embeds every non-archived memory with the current embedder. It
// returns the number of memories re-embedded. Use after switching providers.
func (e *Engine) Reindex(ctx context.Context) (int, error) {
	mems, err := e.store.AllForReindex()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range mems {
		vec, embID := e.tryEmbed(ctx, embedText(m.Title, m.Content))
		if embID == "" {
			continue // skip un-embeddable / provider failure
		}
		if err := e.store.UpdateEmbedding(m.ID, vec, embID); err != nil {
			return n, err
		}
		n++
	}
	_ = e.store.SetMeta("embed_id", e.emb.ID())
	return n, nil
}
