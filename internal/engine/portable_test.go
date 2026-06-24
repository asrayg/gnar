package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/asraygopa/gnar/internal/model"
)

func TestExportImportRoundtrip(t *testing.T) {
	src := newTestEngine(t)
	ctx := context.Background()
	mustRemember(t, src, "decision about caching", model.KindDecision)
	mustRemember(t, src, "a durable fact", model.KindFact)
	mustRemember(t, src, "an open todo", model.KindTodo)

	var buf bytes.Buffer
	n, err := src.Export(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("exported %d, want 3", n)
	}
	if lines := strings.Count(strings.TrimRight(buf.String(), "\n"), "\n"); lines != 2 {
		t.Fatalf("expected 3 JSONL lines (2 newlines between), got %d", lines)
	}

	// Import into a fresh engine.
	dst := newTestEngine(t)
	res, err := dst.Import(ctx, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 3 || res.Skipped != 0 {
		t.Fatalf("import = %+v, want added 3 skipped 0", res)
	}

	// Imported memories are searchable and re-embedded.
	got, _ := dst.Recall(ctx, RecallInput{Project: proj, Query: "caching", Limit: 5})
	if len(got) == 0 {
		t.Fatal("imported memory not searchable")
	}

	// Re-import is idempotent.
	res2, err := dst.Import(ctx, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Added != 0 || res2.Skipped != 3 {
		t.Fatalf("re-import = %+v, want added 0 skipped 3", res2)
	}
}

func TestImportRejectsBadRecord(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Import(context.Background(), strings.NewReader(`{"kind":"note"}`+"\n"))
	if err == nil {
		t.Fatal("expected error for record missing project/content")
	}
}

func TestImportIsAtomic(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	// A valid record followed by an invalid one: the whole import must fail and
	// leave the store completely unchanged (all-or-nothing).
	stream := `{"project":"p","kind":"note","content":"good one"}` + "\n" +
		`{"project":"p","kind":"bogus","content":"bad kind"}` + "\n"
	res, err := e.Import(ctx, strings.NewReader(stream))
	if err == nil {
		t.Fatal("expected import to fail on the invalid record")
	}
	if res.Added != 0 {
		t.Fatalf("atomic import should report 0 added on failure, got %d", res.Added)
	}
	got, _ := e.List(ListInput{Project: "p", AllProjects: true})
	if len(got) != 0 {
		t.Fatalf("atomic import left %d rows behind; rollback failed", len(got))
	}
}

func TestImportContextCancel(t *testing.T) {
	e := newTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	_, err := e.Import(ctx, strings.NewReader(`{"project":"p","kind":"note","content":"x"}`+"\n"))
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	got, _ := e.List(ListInput{AllProjects: true})
	if len(got) != 0 {
		t.Fatalf("cancelled import wrote %d rows", len(got))
	}
}
