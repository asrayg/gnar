package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asraygopa/gnar/internal/config"
	"github.com/asraygopa/gnar/internal/embed"
	"github.com/asraygopa/gnar/internal/engine"
	"github.com/asraygopa/gnar/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectTestClient wires an in-process client to a gnar MCP server over an
// in-memory transport and returns the client session.
func connectTestClient(t *testing.T) (*mcp.ClientSession, context.Context) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Defaults()
	eng := engine.New(st, embed.NewHash(256), &cfg)
	srv := New(eng, t.TempDir()).Build()

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, ctx
}

func callText(t *testing.T, cs *mcp.ClientSession, ctx context.Context, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("call %s returned tool error: %v", name, res.Content)
	}
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("call %s: first content not text", name)
	}
	return tc.Text
}

func TestMCPToolsListed(t *testing.T) {
	cs, ctx := connectTestClient(t)
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"gnar_remember": false, "gnar_recall": false, "gnar_handoff": false,
		"gnar_resume": false, "gnar_context": false, "gnar_list": false,
		"gnar_get": false, "gnar_update": false, "gnar_forget": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %s not registered", name)
		}
	}
}

func TestMCPRememberRecallRoundtrip(t *testing.T) {
	cs, ctx := connectTestClient(t)
	out := callText(t, cs, ctx, "gnar_remember", map[string]any{
		"content": "the deploy pipeline uses GitHub Actions",
		"kind":    "fact",
		"project": "demo",
	})
	if !strings.Contains(out, "Remembered") {
		t.Fatalf("unexpected remember output: %q", out)
	}
	rec := callText(t, cs, ctx, "gnar_recall", map[string]any{
		"query": "deploy pipeline", "project": "demo",
	})
	if !strings.Contains(rec, "GitHub Actions") {
		t.Fatalf("recall did not find memory: %q", rec)
	}
}

func TestMCPSourceFromClientName(t *testing.T) {
	cs, ctx := connectTestClient(t)
	// Remember without an explicit source — should default to the client name.
	callText(t, cs, ctx, "gnar_remember", map[string]any{"content": "x", "project": "demo"})
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "gnar_list", Arguments: map[string]any{"project": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The structured output carries the records; verify via get to read source.
	got := callText(t, cs, ctx, "gnar_get", map[string]any{"id": 1})
	if !strings.Contains(got, "claude-code") {
		t.Fatalf("source not derived from client name; get output:\n%s", got)
	}
	_ = res
}

func TestMCPHandoffAndResumeResource(t *testing.T) {
	cs, ctx := connectTestClient(t)
	callText(t, cs, ctx, "gnar_handoff", map[string]any{
		"goal": "ship v1", "state": "tests pass", "project": "demo",
	})
	// resume tool
	brief := callText(t, cs, ctx, "gnar_resume", map[string]any{"project": "demo"})
	if !strings.Contains(brief, "ship v1") {
		t.Fatalf("resume missing handoff goal: %q", brief)
	}
	// resume resource
	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "gnar://resume"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Contents) == 0 || rr.Contents[0].MIMEType != "text/markdown" {
		t.Fatalf("bad resume resource: %+v", rr.Contents)
	}
}

func TestMCPResumePrompt(t *testing.T) {
	cs, ctx := connectTestClient(t)
	prompts, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range prompts.Prompts {
		if p.Name == "resume" {
			found = true
		}
	}
	if !found {
		t.Fatal("resume prompt not registered")
	}
	gp, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gp.Messages) == 0 {
		t.Fatal("resume prompt has no messages")
	}
}
