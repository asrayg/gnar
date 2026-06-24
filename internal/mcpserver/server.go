// Package mcpserver exposes the Gnar engine as an MCP server over stdio so any
// agent or IDE that speaks the Model Context Protocol can share one memory.
//
// IMPORTANT: the stdio transport owns stdout for JSON-RPC framing. Never write
// to stdout here — log to stderr only.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/asraygopa/gnar/internal/engine"
	"github.com/asraygopa/gnar/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the reported server version (overridable via -ldflags -X).
var Version = "0.1.0"

// Server bundles the engine with a default working directory used for project
// auto-detection when a tool call omits an explicit project.
type Server struct {
	eng *engine.Engine
	dir string
}

// New creates an MCP server over the given engine. dir is the default directory
// for project detection (typically the process working directory).
func New(eng *engine.Engine, dir string) *Server {
	return &Server{eng: eng, dir: dir}
}

// Build constructs the underlying MCP server with all tools, resources, and
// prompts registered. Exposed so tests can attach an in-memory transport.
func (s *Server) Build() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "gnar", Version: Version}, &mcp.ServerOptions{
		Instructions: "Gnar is shared, persistent memory across agents, IDEs, and context " +
			"windows. Use gnar_resume at the start of a session to load where things " +
			"left off; gnar_remember to save decisions, facts, and todos; and " +
			"gnar_handoff before your context window fills up or you switch tools.",
	})
	s.register(srv)
	s.registerResources(srv)
	return srv
}

// Run serves over stdio until the client disconnects.
func (s *Server) Run(ctx context.Context) error {
	return s.Build().Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "gnar_remember",
		Description: "Save a memory to shared storage so other agents/IDEs/future contexts can recall it. " +
			"Use for decisions (with rationale), durable facts, todos, and reusable snippets.",
	}, s.remember)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "gnar_recall",
		Description: "Search shared memory by meaning and keywords. Returns the most relevant memories " +
			"for the current project (or all projects).",
	}, s.recall)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "gnar_handoff",
		Description: "Record a context-window handoff: the current goal, state, next steps, open questions, " +
			"and files in play. Call this before your context fills up or before switching tools so the " +
			"next agent can resume seamlessly.",
	}, s.handoff)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "gnar_resume",
		Description: "Load where work left off for a project: the latest handoff plus pinned facts, recent " +
			"decisions, and open todos, rendered as a brief. Call this at the start of a session.",
	}, s.resume)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gnar_context",
		Description: "Get an at-a-glance overview of a project's memory: last handoff, pinned items, decisions, todos, and recent notes.",
	}, s.contextTool)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gnar_list",
		Description: "List recent memories for a project, optionally filtered by kind.",
	}, s.list)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gnar_get",
		Description: "Fetch a single memory by its numeric id.",
	}, s.get)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gnar_update",
		Description: "Update fields of an existing memory by id (title, content, kind, tags, pinned).",
	}, s.update)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gnar_forget",
		Description: "Archive (or permanently delete) a memory by id.",
	}, s.forget)
}

// --- tool: remember ---

type rememberIn struct {
	Content string   `json:"content" jsonschema:"the memory text to store"`
	Kind    string   `json:"kind,omitempty" jsonschema:"one of: note, decision, fact, todo, snippet (default note)"`
	Title   string   `json:"title,omitempty" jsonschema:"a short title/summary"`
	Tags    []string `json:"tags,omitempty" jsonschema:"tags for later filtering"`
	Files   []string `json:"files,omitempty" jsonschema:"related file paths"`
	Pinned  bool     `json:"pinned,omitempty" jsonschema:"pin this memory so it always surfaces on resume"`
	Project string   `json:"project,omitempty" jsonschema:"project namespace; defaults to the auto-detected current project"`
	Source  string   `json:"source,omitempty" jsonschema:"the agent/IDE saving this (e.g. claude-code, cursor)"`
	Session string   `json:"session,omitempty" jsonschema:"an optional session/context id"`
}

type memoryOut struct {
	ID      int64    `json:"id"`
	Project string   `json:"project"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title,omitempty"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Source  string   `json:"source,omitempty"`
}

func (s *Server) remember(ctx context.Context, req *mcp.CallToolRequest, in rememberIn) (*mcp.CallToolResult, memoryOut, error) {
	m, err := s.eng.Remember(ctx, engine.RememberInput{
		Project: in.Project,
		Dir:     s.dir,
		Kind:    model.Kind(strings.ToLower(in.Kind)),
		Title:   in.Title,
		Content: in.Content,
		Tags:    in.Tags,
		Files:   in.Files,
		Source:  clientSource(req, in.Source),
		Session: in.Session,
		Pinned:  in.Pinned,
	})
	if err != nil {
		return nil, memoryOut{}, err
	}
	out := toMemoryOut(m)
	return textResult(fmt.Sprintf("Remembered #%d (%s) in %s", m.ID, m.Kind, projName(m.Project))), out, nil
}

// --- tool: recall ---

type recallIn struct {
	Query       string   `json:"query" jsonschema:"what to search for (natural language or keywords)"`
	Kinds       []string `json:"kinds,omitempty" jsonschema:"restrict to these kinds (note, decision, fact, todo, snippet, handoff)"`
	Tags        []string `json:"tags,omitempty" jsonschema:"restrict to memories carrying any of these tags"`
	Limit       int      `json:"limit,omitempty" jsonschema:"max results (default 10)"`
	Project     string   `json:"project,omitempty" jsonschema:"project namespace; defaults to current project"`
	AllProjects bool     `json:"all_projects,omitempty" jsonschema:"search across every project instead of just the current one"`
}

type recallOut struct {
	Results []memoryOut `json:"results"`
}

func (s *Server) recall(ctx context.Context, _ *mcp.CallToolRequest, in recallIn) (*mcp.CallToolResult, recallOut, error) {
	mems, err := s.eng.Recall(ctx, engine.RecallInput{
		Project:     in.Project,
		Dir:         s.dir,
		AllProjects: in.AllProjects,
		Query:       in.Query,
		Kinds:       parseKinds(in.Kinds),
		Tags:        in.Tags,
		Limit:       in.Limit,
	})
	if err != nil {
		return nil, recallOut{}, err
	}
	out := recallOut{Results: toMemoryOuts(mems)}
	return textResult(renderMemList(mems)), out, nil
}

// --- tool: handoff ---

type handoffIn struct {
	Goal      string   `json:"goal,omitempty" jsonschema:"what you are trying to accomplish"`
	State     string   `json:"state,omitempty" jsonschema:"where things stand right now"`
	NextSteps []string `json:"next_steps,omitempty" jsonschema:"the next concrete steps to take"`
	OpenQs    []string `json:"open_questions,omitempty" jsonschema:"unresolved questions or blockers"`
	Files     []string `json:"files,omitempty" jsonschema:"files currently in play"`
	Branch    string   `json:"branch,omitempty" jsonschema:"git branch (auto-detected if omitted)"`
	Project   string   `json:"project,omitempty" jsonschema:"project namespace; defaults to current project"`
	Source    string   `json:"source,omitempty" jsonschema:"the agent/IDE handing off"`
	Session   string   `json:"session,omitempty" jsonschema:"an optional session/context id"`
}

func (s *Server) handoff(ctx context.Context, req *mcp.CallToolRequest, in handoffIn) (*mcp.CallToolResult, memoryOut, error) {
	m, err := s.eng.Handoff(ctx, engine.HandoffInput{
		Project: in.Project,
		Dir:     s.dir,
		Source:  clientSource(req, in.Source),
		Session: in.Session,
		Handoff: model.Handoff{
			Goal:      in.Goal,
			State:     in.State,
			NextSteps: in.NextSteps,
			OpenQs:    in.OpenQs,
			Files:     in.Files,
			Branch:    in.Branch,
		},
	})
	if err != nil {
		return nil, memoryOut{}, err
	}
	return textResult(fmt.Sprintf("Handoff #%d recorded for %s. The next session can `gnar_resume`.", m.ID, projName(m.Project))), toMemoryOut(m), nil
}

// --- tool: resume / context ---

type resumeIn struct {
	Project string `json:"project,omitempty" jsonschema:"project namespace; defaults to current project"`
	Query   string `json:"query,omitempty" jsonschema:"optionally also pull memories relevant to this query"`
}

type briefOut struct {
	Project string             `json:"project"`
	Brief   string             `json:"brief"`
	Bundle  model.ResumeBundle `json:"bundle"`
}

func (s *Server) resume(ctx context.Context, _ *mcp.CallToolRequest, in resumeIn) (*mcp.CallToolResult, briefOut, error) {
	b, err := s.eng.Resume(ctx, engine.ResumeInput{Project: in.Project, Dir: s.dir, Query: in.Query})
	if err != nil {
		return nil, briefOut{}, err
	}
	return textResult(b.Brief), briefOut{Project: b.Project, Brief: b.Brief, Bundle: b}, nil
}

func (s *Server) contextTool(ctx context.Context, _ *mcp.CallToolRequest, in resumeIn) (*mcp.CallToolResult, briefOut, error) {
	b, err := s.eng.Context(ctx, in.Project, s.dir)
	if err != nil {
		return nil, briefOut{}, err
	}
	return textResult(b.Brief), briefOut{Project: b.Project, Brief: b.Brief, Bundle: b}, nil
}

// --- tool: list ---

type listIn struct {
	Kinds       []string `json:"kinds,omitempty" jsonschema:"restrict to these kinds"`
	Limit       int      `json:"limit,omitempty" jsonschema:"max results (default 20)"`
	Project     string   `json:"project,omitempty" jsonschema:"project namespace; defaults to current project"`
	AllProjects bool     `json:"all_projects,omitempty" jsonschema:"list across all projects"`
}

func (s *Server) list(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, recallOut, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	mems, err := s.eng.List(engine.ListInput{
		Project:     in.Project,
		Dir:         s.dir,
		AllProjects: in.AllProjects,
		Kinds:       parseKinds(in.Kinds),
		Limit:       limit,
	})
	if err != nil {
		return nil, recallOut{}, err
	}
	return textResult(renderMemList(mems)), recallOut{Results: toMemoryOuts(mems)}, nil
}

// --- tool: get ---

type getIn struct {
	ID int64 `json:"id" jsonschema:"the memory id"`
}

func (s *Server) get(_ context.Context, _ *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, memoryOut, error) {
	m, ok, err := s.eng.Get(in.ID)
	if err != nil {
		return nil, memoryOut{}, err
	}
	if !ok {
		return nil, memoryOut{}, fmt.Errorf("memory %d not found", in.ID)
	}
	return textResult(renderMem(m)), toMemoryOut(m), nil
}

// --- tool: update ---

type updateIn struct {
	ID      int64     `json:"id" jsonschema:"the memory id to update"`
	Title   *string   `json:"title,omitempty" jsonschema:"new title"`
	Content *string   `json:"content,omitempty" jsonschema:"new content"`
	Kind    *string   `json:"kind,omitempty" jsonschema:"new kind"`
	Tags    *[]string `json:"tags,omitempty" jsonschema:"replacement tags"`
	Pinned  *bool     `json:"pinned,omitempty" jsonschema:"pin or unpin"`
}

func (s *Server) update(ctx context.Context, _ *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, memoryOut, error) {
	patch := engine.UpdateInput{ID: in.ID, Title: in.Title, Content: in.Content, Tags: in.Tags, Pinned: in.Pinned}
	if in.Kind != nil {
		k := model.Kind(strings.ToLower(*in.Kind))
		patch.Kind = &k
	}
	m, err := s.eng.Update(ctx, patch)
	if err != nil {
		return nil, memoryOut{}, err
	}
	return textResult(fmt.Sprintf("Updated #%d", m.ID)), toMemoryOut(m), nil
}

// --- tool: forget ---

type forgetIn struct {
	ID   int64 `json:"id" jsonschema:"the memory id to forget"`
	Hard bool  `json:"hard,omitempty" jsonschema:"permanently delete instead of archiving"`
}

type forgetOut struct {
	ID      int64 `json:"id"`
	Removed bool  `json:"removed"`
}

func (s *Server) forget(_ context.Context, _ *mcp.CallToolRequest, in forgetIn) (*mcp.CallToolResult, forgetOut, error) {
	ok, err := s.eng.Forget(in.ID, in.Hard)
	if err != nil {
		return nil, forgetOut{}, err
	}
	verb := "archived"
	if in.Hard {
		verb = "deleted"
	}
	if !ok {
		return textResult(fmt.Sprintf("memory %d not found", in.ID)), forgetOut{ID: in.ID, Removed: false}, nil
	}
	return textResult(fmt.Sprintf("%s #%d", verb, in.ID)), forgetOut{ID: in.ID, Removed: true}, nil
}
