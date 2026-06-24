package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/asrayg/gnar/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// textResult wraps a string as a tool result text-content block. Returning this
// explicitly (rather than nil) gives agents a readable summary alongside the
// structured output the SDK derives from the typed return value.
func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// clientSource resolves the memory source: an explicit value wins, otherwise the
// connecting MCP client's reported name (e.g. "claude-code", "cursor"), otherwise
// a generic fallback. This preserves provenance across agents automatically.
func clientSource(req *mcp.CallToolRequest, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if req != nil && req.Session != nil {
		if ip := req.Session.InitializeParams(); ip != nil && ip.ClientInfo != nil {
			if name := strings.TrimSpace(ip.ClientInfo.Name); name != "" {
				return name
			}
		}
	}
	return "agent"
}

func projName(projectID string) string {
	return filepath.Base(projectID)
}

func parseKinds(ss []string) []model.Kind {
	var out []model.Kind
	for _, s := range ss {
		k := model.Kind(strings.ToLower(strings.TrimSpace(s)))
		if model.ValidKind(k) {
			out = append(out, k)
		}
	}
	return out
}

func toMemoryOut(m model.Memory) memoryOut {
	return memoryOut{
		ID:      m.ID,
		Project: projName(m.Project),
		Kind:    string(m.Kind),
		Title:   m.Title,
		Content: m.Content,
		Tags:    m.Tags,
		Source:  m.Source,
	}
}

func toMemoryOuts(mems []model.Memory) []memoryOut {
	out := make([]memoryOut, len(mems))
	for i, m := range mems {
		out[i] = toMemoryOut(m)
	}
	return out
}

func renderMemList(mems []model.Memory) string {
	if len(mems) == 0 {
		return "No matching memories."
	}
	var b strings.Builder
	for _, m := range mems {
		line := m.Title
		if line == "" {
			line = firstLine(m.Content, 100)
		}
		fmt.Fprintf(&b, "[#%d] (%s) %s", m.ID, m.Kind, line)
		if len(m.Tags) > 0 {
			fmt.Fprintf(&b, "  {%s}", strings.Join(m.Tags, ", "))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderMem(m model.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d (%s) in %s\n", m.ID, m.Kind, projName(m.Project))
	if m.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", m.Title)
	}
	if len(m.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(m.Tags, ", "))
	}
	if len(m.Files) > 0 {
		fmt.Fprintf(&b, "Files: %s\n", strings.Join(m.Files, ", "))
	}
	if m.Source != "" {
		fmt.Fprintf(&b, "Source: %s\n", m.Source)
	}
	b.WriteString("\n")
	b.WriteString(m.Content)
	return b.String()
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
