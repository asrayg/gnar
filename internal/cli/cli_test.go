package cli

import (
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

// capture redirects os.Stdout for the duration of fn and returns what was written.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

// isolatedHome points GNAR_HOME at a temp dir and clears env overrides that
// would otherwise leak into the test.
func isolatedHome(t *testing.T) {
	t.Helper()
	t.Setenv("GNAR_HOME", t.TempDir())
	t.Setenv("GNAR_PROJECT", "testproj")
	for _, k := range []string{"GNAR_EMBED_PROVIDER", "GNAR_EMBED_MODEL", "GNAR_EMBED_BASE_URL", "GNAR_SOURCE"} {
		t.Setenv(k, "")
	}
}

func TestCLIRememberRecallFlow(t *testing.T) {
	isolatedHome(t)
	if code := Run([]string{"remember", "-k", "decision", "SQLite with WAL for storage", "--tag", "db"}); code != 0 {
		t.Fatalf("remember exit = %d", code)
	}
	if code := Run([]string{"remember", "buy more coffee", "--tag", "life"}); code != 0 {
		t.Fatalf("remember 2 exit = %d", code)
	}
	out := capture(t, func() { Run([]string{"recall", "storage sqlite"}) })
	if !strings.Contains(out, "SQLite with WAL") {
		t.Fatalf("recall missed the relevant memory:\n%s", out)
	}
	// tag filter
	tagged := capture(t, func() { Run([]string{"recall", "--tag", "db", "storage"}) })
	if strings.Contains(tagged, "coffee") {
		t.Fatalf("tag filter leaked untagged memory:\n%s", tagged)
	}
}

func TestCLIHandoffResume(t *testing.T) {
	isolatedHome(t)
	Run([]string{"handoff", "-g", "ship the CLI", "-s", "commands wired", "--next", "write tests"})
	out := capture(t, func() { Run([]string{"resume"}) })
	if !strings.Contains(out, "ship the CLI") || !strings.Contains(out, "write tests") {
		t.Fatalf("resume brief incomplete:\n%s", out)
	}
}

func TestCLIExportImportRoundtrip(t *testing.T) {
	isolatedHome(t)
	Run([]string{"remember", "-k", "fact", "first fact"})
	Run([]string{"remember", "-k", "todo", "first todo"})
	dump := capture(t, func() { Run([]string{"export"}) })
	if strings.Count(dump, "\n") != 2 {
		t.Fatalf("expected 2 JSONL lines, got:\n%s", dump)
	}

	// import into a second isolated home
	t.Setenv("GNAR_HOME", t.TempDir())
	f := t.TempDir() + "/dump.jsonl"
	if err := os.WriteFile(f, []byte(dump), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() { Run([]string{"import", f}) })
	if !strings.Contains(out, "imported 2") {
		t.Fatalf("import output: %s", out)
	}
	// second import is idempotent
	out2 := capture(t, func() { Run([]string{"import", f}) })
	if !strings.Contains(out2, "imported 0, skipped 2") {
		t.Fatalf("import not idempotent: %s", out2)
	}
}

func TestCLIEditValidation(t *testing.T) {
	isolatedHome(t)
	Run([]string{"remember", "a memory"})
	// no field flags → error
	if code := Run([]string{"edit", "1"}); code != 1 {
		t.Fatalf("edit with no fields exit = %d, want 1", code)
	}
	// invalid --pin value → error
	if code := Run([]string{"edit", "1", "--pin", "maybe"}); code != 1 {
		t.Fatalf("edit with bad --pin exit = %d, want 1", code)
	}
	// valid edit succeeds
	if code := Run([]string{"edit", "1", "-t", "new title", "--pin", "true"}); code != 0 {
		t.Fatalf("valid edit exit = %d, want 0", code)
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	if code := Run([]string{"frobnicate"}); code != 2 {
		t.Fatalf("unknown command exit = %d, want 2", code)
	}
}

func TestCLIVersion(t *testing.T) {
	out := capture(t, func() { Run([]string{"version"}) })
	if !strings.Contains(out, "gnar") {
		t.Fatalf("version output: %s", out)
	}
}

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	title := fs.String("t", "", "")
	var tags stringSlice
	fs.Var(&tags, "tag", "")

	pos, err := parseInterspersed(fs, []string{"hello", "-t", "Title", "world", "--tag", "a", "--tag", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pos, " ") != "hello world" {
		t.Fatalf("positional = %q, want 'hello world'", strings.Join(pos, " "))
	}
	if *title != "Title" {
		t.Fatalf("title = %q", *title)
	}
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("tags = %v, want [a b]", tags)
	}
}
