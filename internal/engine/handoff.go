package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/asraygopa/gnar/internal/model"
	"github.com/asraygopa/gnar/internal/store"
)

// HandoffInput describes a context-window handoff to record.
type HandoffInput struct {
	Project string
	Dir     string
	Source  string
	Session string
	Handoff model.Handoff
}

// Handoff records a structured snapshot of the current working state so the next
// agent/context can resume. It is stored as a KindHandoff memory whose content
// is a rendered brief and whose Meta holds the structured fields.
func (e *Engine) Handoff(ctx context.Context, in HandoffInput) (model.Memory, error) {
	if in.Handoff.Empty() {
		return model.Memory{}, fmt.Errorf("empty handoff: provide at least a goal, state, or next step")
	}
	proj := ResolveProject(in.Project, in.Dir)
	h := in.Handoff
	if h.Branch == "" {
		h.Branch = gitBranch(firstNonEmpty(in.Dir, proj.ID))
	}
	content := renderHandoff(proj.Name, h, e.now())
	title := "Handoff"
	if h.Goal != "" {
		title = "Handoff: " + truncateLine(h.Goal, 60)
	}
	return e.Remember(ctx, RememberInput{
		Project: proj.ID,
		Kind:    model.KindHandoff,
		Title:   title,
		Content: content,
		Files:   h.Files,
		Source:  in.Source,
		Session: in.Session,
		Meta:    handoffMeta(h),
	})
}

func handoffMeta(h model.Handoff) map[string]any {
	m := map[string]any{}
	if h.Goal != "" {
		m["goal"] = h.Goal
	}
	if h.State != "" {
		m["state"] = h.State
	}
	if len(h.NextSteps) > 0 {
		m["next_steps"] = toAnySlice(h.NextSteps)
	}
	if len(h.OpenQs) > 0 {
		m["open_questions"] = toAnySlice(h.OpenQs)
	}
	if len(h.Files) > 0 {
		m["files"] = toAnySlice(h.Files)
	}
	if h.Branch != "" {
		m["branch"] = h.Branch
	}
	return m
}

// handoffFromMeta reconstructs a Handoff from a stored memory's Meta.
func handoffFromMeta(meta map[string]any) model.Handoff {
	return model.Handoff{
		Goal:      asString(meta["goal"]),
		State:     asString(meta["state"]),
		NextSteps: asStringSlice(meta["next_steps"]),
		OpenQs:    asStringSlice(meta["open_questions"]),
		Files:     asStringSlice(meta["files"]),
		Branch:    asString(meta["branch"]),
	}
}

// --- Resume ---

// ResumeInput describes a resume request.
type ResumeInput struct {
	Project string
	Dir     string
	Query   string // optional: also pull query-relevant memories
	Limit   int    // per-section cap (default 5)
}

// Resume gathers the latest handoff plus durable context for a project and
// renders a brief an agent can drop into a fresh context window.
func (e *Engine) Resume(ctx context.Context, in ResumeInput) (model.ResumeBundle, error) {
	proj := ResolveProject(in.Project, in.Dir)
	per := in.Limit
	if per <= 0 {
		per = 5
	}
	b := model.ResumeBundle{Project: proj.Name, ProjectID: proj.ID}

	latest, err := e.store.List(store.Query{Project: proj.ID, Kinds: []model.Kind{model.KindHandoff}, Limit: 1})
	if err != nil {
		return b, err
	}
	if len(latest) > 0 {
		b.Handoff = &latest[0]
	}
	if b.Pinned, err = e.store.List(store.Query{Project: proj.ID, PinnedOnly: true, Limit: per}); err != nil {
		return b, err
	}
	if b.Decisions, err = e.store.List(store.Query{Project: proj.ID, Kinds: []model.Kind{model.KindDecision}, Limit: per}); err != nil {
		return b, err
	}
	if b.Todos, err = e.store.List(store.Query{Project: proj.ID, Kinds: []model.Kind{model.KindTodo}, Limit: per}); err != nil {
		return b, err
	}
	if strings.TrimSpace(in.Query) != "" {
		rel, err := e.Recall(ctx, RecallInput{Project: proj.ID, Query: in.Query, Limit: per})
		if err != nil {
			return b, err
		}
		b.Relevant = rel
	}
	b.Brief = renderResume(b)
	return b, nil
}

// Context returns a project overview: counts and recent activity. It reuses the
// ResumeBundle shape but renders an overview-style brief.
func (e *Engine) Context(ctx context.Context, project, dir string) (model.ResumeBundle, error) {
	bundle, err := e.Resume(ctx, ResumeInput{Project: project, Dir: dir, Limit: 5})
	if err != nil {
		return bundle, err
	}
	// Recent notes/facts/snippets for an at-a-glance overview.
	recent, err := e.store.List(store.Query{
		Project: bundle.ProjectID,
		Kinds:   []model.Kind{model.KindNote, model.KindFact, model.KindSnippet},
		Limit:   5,
	})
	if err != nil {
		return bundle, err
	}
	bundle.Relevant = recent
	bundle.Brief = renderContext(bundle, recent)
	return bundle, nil
}

// --- small conversion helpers ---

func toAnySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateLine(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func fmtTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}
