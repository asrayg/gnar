// Package model holds Gnar's domain types. It has no dependencies on other
// internal packages so every layer can share it without import cycles.
package model

import "time"

// Kind classifies a memory.
type Kind string

const (
	KindNote     Kind = "note"     // a general observation worth keeping
	KindDecision Kind = "decision" // a choice that was made, and why
	KindFact     Kind = "fact"     // a durable fact about the project/environment
	KindTodo     Kind = "todo"     // something still to be done
	KindSnippet  Kind = "snippet"  // a reusable code/command snippet
	KindHandoff  Kind = "handoff"  // a context-window handoff snapshot
)

// AllKinds is the canonical set of kinds, in display order.
var AllKinds = []Kind{KindNote, KindDecision, KindFact, KindTodo, KindSnippet, KindHandoff}

// ValidKind reports whether k is a known kind.
func ValidKind(k Kind) bool {
	for _, v := range AllKinds {
		if v == k {
			return true
		}
	}
	return false
}

// Memory is a single remembered thing.
type Memory struct {
	ID      int64          `json:"id"`
	Project string         `json:"project"`
	Kind    Kind           `json:"kind"`
	Title   string         `json:"title,omitempty"`
	Content string         `json:"content"`
	Tags    []string       `json:"tags,omitempty"`
	Files   []string       `json:"files,omitempty"`
	Source  string         `json:"source,omitempty"`  // agent/IDE that wrote it
	Session string         `json:"session,omitempty"` // context-window / session id
	Meta    map[string]any `json:"meta,omitempty"`    // structured extras (e.g. handoff payload)
	Pinned  bool           `json:"pinned,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Score is populated only on search results (higher is more relevant).
	Score float64 `json:"score,omitempty"`
}

// Handoff is the structured payload of a context-window handoff. It is stored
// as a Memory of KindHandoff, with these fields under Memory.Meta and a
// rendered markdown brief in Memory.Content.
type Handoff struct {
	Goal      string   `json:"goal,omitempty"`           // what we're trying to accomplish
	State     string   `json:"state,omitempty"`          // where things stand right now
	NextSteps []string `json:"next_steps,omitempty"`     // what to do next
	OpenQs    []string `json:"open_questions,omitempty"` // unresolved questions / blockers
	Files     []string `json:"files,omitempty"`          // files in play
	Branch    string   `json:"branch,omitempty"`         // git branch
}

// Empty reports whether the handoff carries no information.
func (h Handoff) Empty() bool {
	return h.Goal == "" && h.State == "" && h.Branch == "" &&
		len(h.NextSteps) == 0 && len(h.OpenQs) == 0 && len(h.Files) == 0
}

// ResumeBundle is everything an agent needs to pick up a project: the latest
// handoff plus the durable context around it, with a rendered brief.
type ResumeBundle struct {
	Project   string   `json:"project"`             // friendly project name
	ProjectID string   `json:"project_id"`          // canonical project key (path)
	Handoff   *Memory  `json:"handoff,omitempty"`   // latest handoff, if any
	Pinned    []Memory `json:"pinned,omitempty"`    // pinned facts/notes
	Decisions []Memory `json:"decisions,omitempty"` // recent decisions
	Todos     []Memory `json:"todos,omitempty"`     // open todos
	Relevant  []Memory `json:"relevant,omitempty"`  // query-relevant memories (if query given)
	Brief     string   `json:"brief"`               // human/agent-readable markdown summary
}

// Stats summarizes the store for `gnar status`/doctor.
type Stats struct {
	DBPath     string         `json:"db_path"`
	Total      int            `json:"total"`
	ByKind     map[Kind]int   `json:"by_kind"`
	Projects   int            `json:"projects"`
	Embedder   string         `json:"embedder"`
	StoredDim  int            `json:"stored_dim"`
	EmbedMatch bool           `json:"embedder_matches_store"`
	TopProj    []ProjectCount `json:"top_projects,omitempty"`
}

// ProjectCount is a per-project memory tally.
type ProjectCount struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
}
