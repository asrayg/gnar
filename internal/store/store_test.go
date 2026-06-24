package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/asrayg/gnar/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mem(project, content string, kind model.Kind) model.Memory {
	now := time.Now()
	return model.Memory{Project: project, Kind: kind, Content: content, CreatedAt: now, UpdatedAt: now}
}

func TestInsertGetRoundtrip(t *testing.T) {
	st := newTestStore(t)
	m := mem("/proj", "hello world", model.KindNote)
	m.Title = "greeting"
	m.Tags = []string{"a", "b"}
	m.Files = []string{"x.go"}
	m.Pinned = true
	m.Meta = map[string]any{"k": "v"}

	id, err := st.Insert(m, []float32{0.1, 0.2, 0.3}, "hash/bow3/3")
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.Get(id)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Title != "greeting" || got.Content != "hello world" || !got.Pinned {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" {
		t.Fatalf("tags mismatch: %v", got.Tags)
	}
	if got.Meta["k"] != "v" {
		t.Fatalf("meta mismatch: %v", got.Meta)
	}
}

func TestEmbeddingBlobRoundtrip(t *testing.T) {
	st := newTestStore(t)
	want := []float32{0.5, -0.25, 1.5, 0}
	id, _ := st.Insert(mem("/p", "x", model.KindFact), want, "id1")
	rows, err := st.Candidates(Query{Project: "/p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Memory.ID != id {
		t.Fatalf("candidates: %+v", rows)
	}
	got := rows[0].Embedding
	if len(got) != len(want) {
		t.Fatalf("embedding len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("embedding[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if rows[0].EmbedID != "id1" {
		t.Fatalf("embed id = %q", rows[0].EmbedID)
	}
}

func TestListFiltersAndArchive(t *testing.T) {
	st := newTestStore(t)
	st.Insert(mem("/p", "note one", model.KindNote), nil, "")
	dID, _ := st.Insert(mem("/p", "a decision", model.KindDecision), nil, "")
	st.Insert(mem("/other", "elsewhere", model.KindNote), nil, "")

	decisions, _ := st.List(Query{Project: "/p", Kinds: []model.Kind{model.KindDecision}})
	if len(decisions) != 1 || decisions[0].ID != dID {
		t.Fatalf("decision filter: %+v", decisions)
	}
	all, _ := st.List(Query{Project: "/p"})
	if len(all) != 2 {
		t.Fatalf("project filter: got %d want 2", len(all))
	}

	if ok, _ := st.SetArchived(dID, true); !ok {
		t.Fatal("archive returned false")
	}
	live, _ := st.List(Query{Project: "/p"})
	if len(live) != 1 {
		t.Fatalf("after archive: got %d want 1", len(live))
	}
	withArchived, _ := st.List(Query{Project: "/p", IncludeArchived: true})
	if len(withArchived) != 2 {
		t.Fatalf("include archived: got %d want 2", len(withArchived))
	}
}

func TestPinnedAndDeleteAndCounts(t *testing.T) {
	st := newTestStore(t)
	pm := mem("/p", "pinned", model.KindNote)
	pm.Pinned = true
	pID, _ := st.Insert(pm, nil, "")
	st.Insert(mem("/p", "plain", model.KindNote), nil, "")

	pinned, _ := st.List(Query{Project: "/p", PinnedOnly: true})
	if len(pinned) != 1 || pinned[0].ID != pID {
		t.Fatalf("pinned filter: %+v", pinned)
	}

	total, byKind, projects, err := st.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || byKind[model.KindNote] != 2 || projects != 1 {
		t.Fatalf("counts: total=%d note=%d proj=%d", total, byKind[model.KindNote], projects)
	}

	if ok, _ := st.Delete(pID); !ok {
		t.Fatal("delete returned false")
	}
	if _, ok, _ := st.Get(pID); ok {
		t.Fatal("memory still present after delete")
	}
}

func TestTagFilterAppliesBeforeLimit(t *testing.T) {
	st := newTestStore(t)
	base := time.Now()
	// Oldest row carries the rare tag; many newer rows do not.
	old := mem("/p", "old tagged memory", model.KindNote)
	old.Tags = []string{"rare"}
	old.CreatedAt = base
	old.UpdatedAt = base
	if _, err := st.Insert(old, nil, ""); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		m := mem("/p", "newer untagged", model.KindNote)
		m.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		m.UpdatedAt = m.CreatedAt
		st.Insert(m, nil, "")
	}
	// With a small cap, a naive post-cap filter would miss the old tagged row.
	got, err := st.List(Query{Project: "/p", Tags: []string{"rare"}, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "old tagged memory" {
		t.Fatalf("tag filter must run before LIMIT; got %d rows: %+v", len(got), got)
	}
}

func TestEscapeLikeTagMatch(t *testing.T) {
	st := newTestStore(t)
	a := mem("/p", "has go", model.KindNote)
	a.Tags = []string{"go"}
	st.Insert(a, nil, "")
	b := mem("/p", "has golang", model.KindNote)
	b.Tags = []string{"golang"}
	st.Insert(b, nil, "")
	// Querying tag "go" must not also match "golang" (quote-delimited).
	got, _ := st.List(Query{Project: "/p", Tags: []string{"go"}})
	if len(got) != 1 || got[0].Content != "has go" {
		t.Fatalf("tag 'go' matched too broadly: %+v", got)
	}
}

func TestExistsSimilarMatchesArchived(t *testing.T) {
	st := newTestStore(t)
	id, _ := st.Insert(mem("/p", "unique content", model.KindNote), nil, "")
	ok, err := st.ExistsSimilar("/p", model.KindNote, "unique content")
	if err != nil || !ok {
		t.Fatalf("ExistsSimilar live = %v (err %v)", ok, err)
	}
	// archived rows must still count, so import can't resurrect a duplicate
	st.SetArchived(id, true)
	ok, _ = st.ExistsSimilar("/p", model.KindNote, "unique content")
	if !ok {
		t.Fatal("ExistsSimilar must match archived rows")
	}
	// different project does not match
	ok, _ = st.ExistsSimilar("/other", model.KindNote, "unique content")
	if ok {
		t.Fatal("ExistsSimilar matched across projects")
	}
}

func TestMeta(t *testing.T) {
	st := newTestStore(t)
	if _, ok, _ := st.GetMeta("nope"); ok {
		t.Fatal("expected missing key")
	}
	if err := st.SetMeta("embed_id", "hash/bow3/256"); err != nil {
		t.Fatal(err)
	}
	v, ok, _ := st.GetMeta("embed_id")
	if !ok || v != "hash/bow3/256" {
		t.Fatalf("meta = %q ok=%v", v, ok)
	}
	// upsert
	st.SetMeta("embed_id", "openai/x/1536")
	v, _, _ = st.GetMeta("embed_id")
	if v != "openai/x/1536" {
		t.Fatalf("upsert failed: %q", v)
	}
}
