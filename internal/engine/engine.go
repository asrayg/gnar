// Package engine is Gnar's orchestration layer. Both front-ends (the CLI and the
// MCP server) are thin shells over an Engine, so they can never drift in
// behavior. The engine wires together the store, the embedder, and config.
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/asraygopa/gnar/internal/config"
	"github.com/asraygopa/gnar/internal/embed"
	"github.com/asraygopa/gnar/internal/model"
	"github.com/asraygopa/gnar/internal/store"
)

// Engine is the high-level Gnar service.
type Engine struct {
	store *store.Store
	emb   embed.Embedder
	cfg   *config.Config
	now   func() time.Time
}

// Open constructs an Engine from config: it opens the database and builds the
// configured embedder.
func Open(cfg *config.Config) (*Engine, error) {
	st, err := store.Open(config.DBPath())
	if err != nil {
		return nil, err
	}
	emb, err := embed.New(embed.Options{
		Provider: cfg.Embed.Provider,
		Model:    cfg.Embed.Model,
		BaseURL:  cfg.Embed.BaseURL,
		APIKey:   cfg.APIKey(),
		Dim:      cfg.Embed.Dim,
	})
	if err != nil {
		st.Close()
		return nil, err
	}
	return &Engine{store: st, emb: emb, cfg: cfg, now: time.Now}, nil
}

// New builds an Engine from explicit dependencies (used in tests).
func New(st *store.Store, emb embed.Embedder, cfg *config.Config) *Engine {
	if cfg == nil {
		c := config.Defaults()
		cfg = &c
	}
	return &Engine{store: st, emb: emb, cfg: cfg, now: time.Now}
}

// Close releases the underlying store.
func (e *Engine) Close() error { return e.store.Close() }

// Embedder returns the active embedder (for diagnostics).
func (e *Engine) Embedder() embed.Embedder { return e.emb }

// candidateCap returns the configured per-project scan cap.
func (e *Engine) candidateCap() int {
	if e.cfg != nil && e.cfg.CandidateCap > 0 {
		return e.cfg.CandidateCap
	}
	return 5000
}

// embedText is the text fed to the embedder for a memory.
func embedText(title, content string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return content
	}
	return title + "\n" + content
}

// tryEmbed embeds text best-effort. On failure it returns (nil, "") so callers
// can persist the memory anyway — keyword recall still works without a vector.
func (e *Engine) tryEmbed(ctx context.Context, text string) ([]float32, string) {
	if strings.TrimSpace(text) == "" {
		return nil, ""
	}
	v, err := embed.EmbedOne(ctx, e.emb, text)
	if err != nil || len(v) == 0 {
		return nil, ""
	}
	return v, e.emb.ID()
}

// --- Remember ---

// RememberInput describes a memory to store.
type RememberInput struct {
	Project string
	Dir     string
	Kind    model.Kind
	Title   string
	Content string
	Tags    []string
	Files   []string
	Source  string
	Session string
	Pinned  bool
	Meta    map[string]any
}

// Remember stores a memory and returns it (with its assigned id).
func (e *Engine) Remember(ctx context.Context, in RememberInput) (model.Memory, error) {
	if strings.TrimSpace(in.Content) == "" && strings.TrimSpace(in.Title) == "" {
		return model.Memory{}, fmt.Errorf("nothing to remember: content is empty")
	}
	kind := in.Kind
	if kind == "" {
		kind = model.KindNote
	}
	if !model.ValidKind(kind) {
		return model.Memory{}, fmt.Errorf("invalid kind %q (want one of %v)", kind, model.AllKinds)
	}
	proj := ResolveProject(in.Project, in.Dir)
	now := e.now()
	m := model.Memory{
		Project:   proj.ID,
		Kind:      kind,
		Title:     strings.TrimSpace(in.Title),
		Content:   in.Content,
		Tags:      normalizeTags(in.Tags),
		Files:     in.Files,
		Source:    in.Source,
		Session:   in.Session,
		Meta:      in.Meta,
		Pinned:    in.Pinned,
		CreatedAt: now,
		UpdatedAt: now,
	}
	vec, embID := e.tryEmbed(ctx, embedText(m.Title, m.Content))
	id, err := e.store.Insert(m, vec, embID)
	if err != nil {
		return model.Memory{}, err
	}
	m.ID = id
	if embID != "" {
		_ = e.store.SetMeta("embed_id", embID)
	}
	return m, nil
}

// --- Get / List / Update / Forget ---

// Get returns a memory by id.
func (e *Engine) Get(id int64) (model.Memory, bool, error) {
	return e.store.Get(id)
}

// ListInput filters a listing.
type ListInput struct {
	Project     string
	Dir         string
	AllProjects bool
	Kinds       []model.Kind
	Limit       int
}

// List returns recent memories for a project (or all projects).
func (e *Engine) List(in ListInput) ([]model.Memory, error) {
	q := store.Query{Kinds: in.Kinds, Limit: in.Limit}
	if !in.AllProjects {
		q.Project = ResolveProject(in.Project, in.Dir).ID
	}
	return e.store.List(q)
}

// UpdateInput patches a memory. Nil fields are left unchanged.
type UpdateInput struct {
	ID      int64
	Title   *string
	Content *string
	Kind    *model.Kind
	Tags    *[]string
	Files   *[]string
	Pinned  *bool
}

// Update applies a patch and re-embeds if the text changed.
func (e *Engine) Update(ctx context.Context, in UpdateInput) (model.Memory, error) {
	m, ok, err := e.store.Get(in.ID)
	if err != nil {
		return model.Memory{}, err
	}
	if !ok {
		return model.Memory{}, fmt.Errorf("memory %d not found", in.ID)
	}
	textChanged := false
	if in.Title != nil {
		m.Title = strings.TrimSpace(*in.Title)
		textChanged = true
	}
	if in.Content != nil {
		m.Content = *in.Content
		textChanged = true
	}
	if in.Kind != nil {
		if !model.ValidKind(*in.Kind) {
			return model.Memory{}, fmt.Errorf("invalid kind %q", *in.Kind)
		}
		m.Kind = *in.Kind
	}
	if in.Tags != nil {
		m.Tags = normalizeTags(*in.Tags)
	}
	if in.Files != nil {
		m.Files = *in.Files
	}
	if in.Pinned != nil {
		m.Pinned = *in.Pinned
	}
	m.UpdatedAt = e.now()

	var vec []float32
	var embID string
	if textChanged {
		if v, id := e.tryEmbed(ctx, embedText(m.Title, m.Content)); id != "" {
			vec, embID = v, id
		} else {
			// Embedding failed (e.g. provider down): keep the existing vector
			// rather than overwriting it with NULL. Mirrors Reindex's guard so a
			// transient failure can never destroy stored embeddings.
			vec, embID, _ = e.store.GetEmbedding(m.ID)
		}
	} else {
		// text unchanged: keep the existing embedding untouched
		vec, embID, _ = e.store.GetEmbedding(m.ID)
	}
	if err := e.store.Update(m, vec, embID); err != nil {
		return model.Memory{}, err
	}
	return m, nil
}

// Forget archives (soft) or deletes (hard) a memory.
func (e *Engine) Forget(id int64, hard bool) (bool, error) {
	if hard {
		return e.store.Delete(id)
	}
	return e.store.SetArchived(id, true)
}

// normalizeTags trims, lowercases, and de-duplicates tags.
func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
