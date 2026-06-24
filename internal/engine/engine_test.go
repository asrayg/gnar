package engine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/asraygopa/gnar/internal/config"
	"github.com/asraygopa/gnar/internal/embed"
	"github.com/asraygopa/gnar/internal/model"
	"github.com/asraygopa/gnar/internal/store"
)

// toggleEmbedder is a test embedder whose Embed can be made to fail on demand.
type toggleEmbedder struct {
	dim  int
	fail bool
}

func (e *toggleEmbedder) Dim() int   { return e.dim }
func (e *toggleEmbedder) ID() string { return "toggle/test/8" }
func (e *toggleEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if e.fail {
		return nil, errors.New("provider down")
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

// TestUpdatePreservesEmbeddingOnEmbedFailure guards the high-severity data-loss
// bug: a transient embed failure during a text edit must NOT wipe the stored
// vector.
func TestUpdatePreservesEmbeddingOnEmbedFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Defaults()
	emb := &toggleEmbedder{dim: 8}
	e := New(st, emb, &cfg)
	ctx := context.Background()

	m, err := e.Remember(ctx, RememberInput{Project: proj, Content: "original", Kind: model.KindNote})
	if err != nil {
		t.Fatal(err)
	}
	vec, embID, _ := st.GetEmbedding(m.ID)
	if len(vec) == 0 || embID == "" {
		t.Fatal("precondition: memory should have an embedding")
	}

	// Now make the embedder fail and edit the content.
	emb.fail = true
	newC := "edited content"
	if _, err := e.Update(ctx, UpdateInput{ID: m.ID, Content: &newC}); err != nil {
		t.Fatal(err)
	}
	gotVec, gotID, _ := st.GetEmbedding(m.ID)
	if len(gotVec) == 0 || gotID == "" {
		t.Fatalf("embedding was wiped on transient embed failure: vec=%d id=%q", len(gotVec), gotID)
	}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Defaults()
	return New(st, embed.NewHash(512), &cfg)
}

const proj = "/test/proj"

func TestRememberAndRecallRanking(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	mustRemember(t, e, "We chose SQLite with WAL mode for the storage layer", model.KindDecision)
	mustRemember(t, e, "Remember to buy oat milk and coffee beans", model.KindTodo)
	mustRemember(t, e, "The storage layer uses a single SQLite connection behind a mutex", model.KindFact)

	got, err := e.Recall(ctx, RecallInput{Project: proj, Query: "sqlite storage layer", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no results")
	}
	// The top result must be storage-related, not the oat milk todo.
	if got[0].Kind == model.KindTodo {
		t.Fatalf("oat milk ranked first; ranking broken: %q", got[0].Content)
	}
	for _, m := range got {
		if m.Score <= 0 {
			t.Fatalf("result has non-positive score: %+v", m)
		}
	}
}

func TestRecallEmptyQueryReturnsRecent(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	mustRemember(t, e, "first", model.KindNote)
	mustRemember(t, e, "second", model.KindNote)
	got, err := e.Recall(ctx, RecallInput{Project: proj, Query: "", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Content != "second" {
		t.Fatalf("expected recency order, got %+v", got)
	}
}

func TestProjectIsolation(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	if _, err := e.Remember(ctx, RememberInput{Project: "/a", Content: "alpha secret", Kind: model.KindNote}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Remember(ctx, RememberInput{Project: "/b", Content: "beta secret", Kind: model.KindNote}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Recall(ctx, RecallInput{Project: "/a", Query: "secret", Limit: 10})
	for _, m := range got {
		if m.Project != "/a" {
			t.Fatalf("cross-project leak: %+v", m)
		}
	}
	all, _ := e.Recall(ctx, RecallInput{AllProjects: true, Query: "secret", Limit: 10})
	if len(all) != 2 {
		t.Fatalf("all-projects search got %d, want 2", len(all))
	}
}

func TestHandoffAndResume(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	mustRemember(t, e, "pin me", model.KindFact) // not pinned, just context
	pinned, _ := e.Remember(ctx, RememberInput{Project: proj, Content: "critical fact", Kind: model.KindFact, Pinned: true})
	_ = pinned

	h, err := e.Handoff(ctx, HandoffInput{
		Project: proj,
		Handoff: model.Handoff{
			Goal:      "finish the engine",
			State:     "tests passing",
			NextSteps: []string{"write docs", "review"},
			OpenQs:    []string{"need a real embedder?"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.Kind != model.KindHandoff {
		t.Fatalf("handoff kind = %q", h.Kind)
	}

	b, err := e.Resume(ctx, ResumeInput{Project: proj})
	if err != nil {
		t.Fatal(err)
	}
	if b.Handoff == nil {
		t.Fatal("resume returned no handoff")
	}
	hf := handoffFromMeta(b.Handoff.Meta)
	if hf.Goal != "finish the engine" || len(hf.NextSteps) != 2 {
		t.Fatalf("handoff meta roundtrip failed: %+v", hf)
	}
	if len(b.Pinned) != 1 {
		t.Fatalf("expected 1 pinned, got %d", len(b.Pinned))
	}
	if b.Brief == "" {
		t.Fatal("empty brief")
	}
}

func TestUpdateAndForget(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	m := mustRemember(t, e, "original content", model.KindNote)

	newContent := "updated content about caching"
	pin := true
	upd, err := e.Update(ctx, UpdateInput{ID: m.ID, Content: &newContent, Pinned: &pin})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Content != newContent || !upd.Pinned {
		t.Fatalf("update failed: %+v", upd)
	}
	// recall should find the updated text
	got, _ := e.Recall(ctx, RecallInput{Project: proj, Query: "caching", Limit: 5})
	if len(got) == 0 {
		t.Fatal("updated content not searchable (re-embed failed)")
	}

	ok, err := e.Forget(m.ID, false)
	if err != nil || !ok {
		t.Fatalf("forget: ok=%v err=%v", ok, err)
	}
	live, _ := e.List(ListInput{Project: proj})
	if len(live) != 0 {
		t.Fatalf("archived memory still listed: %d", len(live))
	}
}

func TestInvalidKindRejected(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Remember(context.Background(), RememberInput{Project: proj, Content: "x", Kind: model.Kind("bogus")})
	if err == nil {
		t.Fatal("expected invalid kind error")
	}
}

func TestReindex(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	mustRemember(t, e, "alpha", model.KindNote)
	mustRemember(t, e, "beta", model.KindNote)
	n, err := e.Reindex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("reindexed %d, want 2", n)
	}
}

func mustRemember(t *testing.T, e *Engine, content string, kind model.Kind) model.Memory {
	t.Helper()
	m, err := e.Remember(context.Background(), RememberInput{Project: proj, Content: content, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
