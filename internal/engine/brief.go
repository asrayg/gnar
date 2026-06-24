package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/asrayg/gnar/internal/model"
)

// renderHandoff renders the markdown body stored as a handoff memory's content.
func renderHandoff(project string, h model.Handoff, at time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Handoff — %s\n", project)
	fmt.Fprintf(&b, "_recorded %s", fmtTime(at))
	if h.Branch != "" {
		fmt.Fprintf(&b, " · branch `%s`", h.Branch)
	}
	b.WriteString("_\n")

	if h.Goal != "" {
		fmt.Fprintf(&b, "\n**Goal:** %s\n", h.Goal)
	}
	if h.State != "" {
		fmt.Fprintf(&b, "\n**State:** %s\n", h.State)
	}
	writeList(&b, "Next steps", h.NextSteps)
	writeList(&b, "Open questions", h.OpenQs)
	if len(h.Files) > 0 {
		b.WriteString("\n**Files:**\n")
		for _, f := range h.Files {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// renderResume renders the resume brief from a bundle.
func renderResume(b model.ResumeBundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Resume — %s\n", b.Project)

	if b.Handoff != nil {
		h := handoffFromMeta(b.Handoff.Meta)
		fmt.Fprintf(&sb, "\n## Where you left off  _(%s)_\n", fmtTime(b.Handoff.CreatedAt))
		if h.Branch != "" {
			fmt.Fprintf(&sb, "_branch `%s`_\n", h.Branch)
		}
		if h.Goal != "" {
			fmt.Fprintf(&sb, "\n**Goal:** %s\n", h.Goal)
		}
		if h.State != "" {
			fmt.Fprintf(&sb, "\n**State:** %s\n", h.State)
		}
		writeList(&sb, "Next steps", h.NextSteps)
		writeList(&sb, "Open questions", h.OpenQs)
		if len(h.Files) > 0 {
			sb.WriteString("\n**Files in play:** ")
			sb.WriteString(joinCode(h.Files))
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("\n_No handoff recorded yet. Use `gnar handoff` before switching context._\n")
	}

	writeMemSection(&sb, "Pinned", b.Pinned)
	writeMemSection(&sb, "Recent decisions", b.Decisions)
	writeMemSection(&sb, "Open todos", b.Todos)
	if len(b.Relevant) > 0 {
		writeMemSection(&sb, "Relevant", b.Relevant)
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// renderContext renders the project overview brief.
func renderContext(b model.ResumeBundle, recent []model.Memory) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Context — %s\n", b.Project)
	if b.Handoff != nil {
		h := handoffFromMeta(b.Handoff.Meta)
		line := h.Goal
		if line == "" {
			line = truncateLine(b.Handoff.Content, 80)
		}
		fmt.Fprintf(&sb, "\n**Last handoff** _(%s)_: %s\n", fmtTime(b.Handoff.CreatedAt), line)
	}
	writeMemSection(&sb, "Pinned", b.Pinned)
	writeMemSection(&sb, "Recent decisions", b.Decisions)
	writeMemSection(&sb, "Open todos", b.Todos)
	writeMemSection(&sb, "Recent notes", recent)
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

func writeMemSection(b *strings.Builder, heading string, mems []model.Memory) {
	if len(mems) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", heading)
	for _, m := range mems {
		line := m.Title
		if line == "" {
			line = truncateLine(m.Content, 100)
		}
		fmt.Fprintf(b, "- [#%d] %s", m.ID, line)
		if len(m.Tags) > 0 {
			fmt.Fprintf(b, "  _(%s)_", strings.Join(m.Tags, ", "))
		}
		b.WriteString("\n")
	}
}

func writeList(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n**%s:**\n", heading)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
}

func joinCode(items []string) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = "`" + it + "`"
	}
	return strings.Join(out, ", ")
}
