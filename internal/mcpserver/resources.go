package mcpserver

import (
	"context"
	"fmt"

	"github.com/asraygopa/gnar/internal/engine"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerResources exposes the project's resume/context briefs as MCP resources
// and a prompt, so clients can auto-load "where you left off" into a fresh context
// without an explicit tool call.
func (s *Server) registerResources(srv *mcp.Server) {
	srv.AddResource(&mcp.Resource{
		Name:        "resume",
		URI:         "gnar://resume",
		Title:       "Resume — where you left off",
		Description: "The latest handoff plus pinned facts, recent decisions, and open todos for the current project.",
		MIMEType:    "text/markdown",
	}, s.resumeResource)

	srv.AddResource(&mcp.Resource{
		Name:        "context",
		URI:         "gnar://context",
		Title:       "Project context overview",
		Description: "An at-a-glance overview of the current project's memory.",
		MIMEType:    "text/markdown",
	}, s.contextResource)

	srv.AddPrompt(&mcp.Prompt{
		Name:        "resume",
		Title:       "Resume this project",
		Description: "Load where work left off for the current project as a primer message.",
	}, s.resumePrompt)
}

func (s *Server) resumeResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	b, err := s.eng.Resume(ctx, engine.ResumeInput{Dir: s.dir})
	if err != nil {
		return nil, err
	}
	return markdownResource("gnar://resume", b.Brief), nil
}

func (s *Server) contextResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	b, err := s.eng.Context(ctx, "", s.dir)
	if err != nil {
		return nil, err
	}
	return markdownResource("gnar://context", b.Brief), nil
}

func (s *Server) resumePrompt(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	b, err := s.eng.Resume(ctx, engine.ResumeInput{Dir: s.dir})
	if err != nil {
		return nil, err
	}
	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Resume brief for %s", b.Project),
		Messages: []*mcp.PromptMessage{
			{
				Role:    "user",
				Content: &mcp.TextContent{Text: "Here is where we left off on this project. Continue from here.\n\n" + b.Brief},
			},
		},
	}, nil
}

func markdownResource(uri, text string) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{URI: uri, MIMEType: "text/markdown", Text: text},
		},
	}
}
