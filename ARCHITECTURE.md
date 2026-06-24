# Gnar — Architecture

**Gnar is shared memory for AI coding.** One persistent store that any agent, in any IDE,
across any context window, can read and write. When your context fills up or you switch
tools, you `handoff` your current state and the next agent `resume`s — seamlessly.

## The problem

A coding session's state lives in three fragile places:

1. **The agent's context window** — evaporates when it fills up and gets summarized.
2. **The IDE** — Cursor doesn't know what you told Claude Code in the terminal.
3. **Your head** — the decisions and dead-ends you'll have to re-explain tomorrow.

Switch any one of those and you start over. Gnar is the durable layer underneath all three.

## Shape

A single Go binary, `gnar`, that is two things at once:

```
        ┌─────────────── agents / IDEs ───────────────┐
        │  Claude Code   Cursor   VS Code   Zed  ...   │
        └───────────────────┬──────────────────────────┘
                            │ MCP (stdio)
                            ▼
   humans ── CLI ──►   ┌─────────────┐
                       │  gnar core  │  (engine: Remember / Recall / Handoff / Resume)
                       └──────┬──────┘
                              ▼
                       ┌─────────────┐
                       │   SQLite    │  ~/.gnar/gnar.db
                       │  + vectors  │  (embeddings as BLOBs, cosine KNN in Go)
                       └─────────────┘
```

- **MCP server** (`gnar serve`) — every agent/IDE that speaks MCP connects to the same store.
- **CLI** (`gnar remember`, `gnar recall`, `gnar resume`, …) — humans and shell scripts.

Both surfaces are thin shells over one **engine**, so they can never drift.

## Core concepts

- **Memory** — one remembered thing. Has a `kind` (`note`, `decision`, `fact`, `todo`,
  `snippet`, `handoff`), free text, tags, related files, and provenance (which `source`
  agent/IDE and `session` wrote it).
- **Project** — the namespace. Auto-detected from the git root (falling back to cwd), so
  switching IDEs inside the same repo shares one memory. Canonicalized to an absolute path;
  displayed by basename.
- **Handoff** — a structured snapshot of "where I am right now": goal, state, next steps,
  open questions, files, branch. This is the context-window bridge.
- **Resume** — the inverse: pull the latest handoff plus the pinned facts, recent decisions,
  and open todos for a project, rendered into one markdown brief an agent can drop into a
  fresh context window.

## Storage

SQLite via the pure-Go `ncruces/go-sqlite3` (WASM) driver — no cgo, single static binary,
cross-compiles anywhere. SQLite 3.53.

Semantic search is done **in Go**, not via a SQLite extension:

- Each memory's embedding is stored as a `BLOB` (little-endian float32) plus the id of the
  embedder that produced it.
- Recall loads the project's candidate memories, scores each by **keyword** (BM25-lite) and
  by **cosine** similarity to the query embedding, and fuses the two rankings with
  **Reciprocal Rank Fusion (RRF)**.

This trades unbounded scale for radical simplicity and zero extension/ABI headaches. For a
personal shared-memory store (thousands of entries, project-scoped) brute-force cosine is
sub-millisecond. The candidate scan is capped (configurable) and the cap is reported.

## Embeddings — pluggable, zero-config by default

An `Embedder` interface with three implementations:

| Provider | When | Notes |
|----------|------|-------|
| `hash`   | **default, no setup** | Deterministic feature-hashing (signed hashing trick). Cosine tracks keyword overlap, so semantic recall works out of the box, offline. |
| `openai` | API key set | OpenAI-compatible `/v1/embeddings` — also covers LM Studio, llama.cpp, vLLM, Ollama's `/v1`. |
| `ollama` | local Ollama | Native `/api/embed`, e.g. `nomic-embed-text` (768d). |

The embedder identity (`provider/model/dim`) is stamped in the DB. Switching embedders makes
old vectors incomparable, so a mismatch is detected and `gnar reindex` re-embeds everything.

## Packages

```
main.go                  thin entry → cli.Run
internal/model           domain types (Memory, Handoff, Kind, ResumeBundle) — no deps
internal/config          ~/.gnar paths, config.json load/save, env overrides
internal/embed           Embedder interface + hash/openai/ollama + factory
internal/store           SQLite: schema, migrations, CRUD, candidate loading, blob (de)serialize
internal/engine          orchestration: Remember/Recall/Handoff/Resume/Context + RRF search + brief rendering
internal/mcpserver       MCP tool registration over the engine
internal/cli             subcommand dispatch + command implementations
```

Dependency direction is strictly downward (`cli`/`mcpserver` → `engine` → `store`/`embed` →
`model`), so there are no import cycles and either front-end can be tested against the engine.

## Data model (SQLite)

```sql
CREATE TABLE memories (
  id         INTEGER PRIMARY KEY,
  project    TEXT NOT NULL,
  kind       TEXT NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  content    TEXT NOT NULL,
  tags       TEXT NOT NULL DEFAULT '[]',   -- JSON array
  files      TEXT NOT NULL DEFAULT '[]',   -- JSON array
  source     TEXT NOT NULL DEFAULT '',
  session    TEXT NOT NULL DEFAULT '',
  meta       TEXT NOT NULL DEFAULT '{}',   -- JSON object (handoff payload, etc.)
  pinned     INTEGER NOT NULL DEFAULT 0,
  archived   INTEGER NOT NULL DEFAULT 0,
  embedding  BLOB,                         -- little-endian float32, NULL if unembedded
  embed_id   TEXT NOT NULL DEFAULT '',     -- embedder identity that produced `embedding`
  created_at INTEGER NOT NULL,             -- unix seconds
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_mem_project  ON memories(project, archived);
CREATE INDEX idx_mem_kind     ON memories(project, kind, archived);
CREATE INDEX idx_mem_created  ON memories(project, created_at);

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);  -- schema_version, embed_id
```

## MCP tools

`gnar_remember`, `gnar_recall`, `gnar_handoff`, `gnar_resume`, `gnar_context`, `gnar_list`,
`gnar_get`, `gnar_update`, `gnar_forget`. Each maps 1:1 to an engine method. Tools accept an
optional `project` (else auto-detected from the server's working directory) and `source`
(the calling agent) so provenance is preserved across tools.
