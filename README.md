# gnar

[![CI](https://github.com/asraygopa/gnar/actions/workflows/ci.yml/badge.svg)](https://github.com/asraygopa/gnar/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/asraygopa/gnar.svg)](https://pkg.go.dev/github.com/asraygopa/gnar)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Shared memory for switching IDEs, agents, and context windows.**

Your coding session's state lives in three fragile places: the agent's context
window (gone when it fills up), the IDE (Cursor can't see what you told Claude
Code), and your head (re-explained tomorrow). Switch any one and you start over.

`gnar` is the durable layer underneath all three — one local memory that every
agent and IDE reads and writes. When your context fills up or you switch tools,
you **hand off** your state and the next agent **resumes** seamlessly.

It's a single Go binary that is two things at once:

- an **MCP server** every agent/IDE can connect to (`gnar serve`), and
- a **CLI** for humans (`gnar remember`, `gnar recall`, `gnar resume`, …).

Both are thin shells over one engine backed by local SQLite with hybrid
**semantic + keyword** recall. No cloud, no account, no cgo — pure Go.

---

## Install

```bash
go build -o gnar .
# put it on your PATH, e.g.
mv gnar /usr/local/bin/
```

Requires Go 1.25+. The binary is fully static (pure-Go SQLite via WebAssembly).

Data lives in `~/.gnar/` (`gnar.db` + `config.json`). Override with `GNAR_HOME`.

---

## Quickstart (CLI)

```bash
# save things worth remembering — scoped to the current project automatically
gnar remember -k decision "Chose SQLite+WAL; KNN done in Go to stay cgo-free" --tag storage
gnar remember -k fact "Prod deploys run from the release branch only"
gnar remember -k todo "Add rate limiting to the /upload endpoint"

# find them later, by meaning and keywords
gnar recall sqlite storage choice
gnar recall -k todo            # filter by kind
gnar list -l                   # recent memories, with bodies

# before your context fills up or you switch tools:
gnar handoff -g "Wire up auth" -s "login works; refresh tokens TODO" \
  --next "add refresh rotation" --open "where to store the signing key?"

# in the next session / IDE / agent — pick up exactly where you left off:
gnar resume
```

`gnar resume` prints a markdown brief: where you left off (the latest handoff)
plus pinned facts, recent decisions, and open todos for the project.

Memories are namespaced by **project**, auto-detected from the git root — so
Claude Code in your terminal and Cursor in the same repo share one memory.

---

## Use it from agents & IDEs (MCP)

`gnar serve` runs an MCP server over stdio. Point any MCP client at it.

### Claude Code

```bash
claude mcp add gnar -- gnar serve
```

or add to `.mcp.json` in your project:

```json
{
  "mcpServers": {
    "gnar": { "command": "gnar", "args": ["serve"] }
  }
}
```

### Cursor

Add to `~/.cursor/mcp.json` (global) or `.cursor/mcp.json` (project):

```json
{
  "mcpServers": {
    "gnar": { "command": "gnar", "args": ["serve"] }
  }
}
```

### VS Code / Zed / any MCP client

Same shape — launch `gnar serve` as a stdio MCP server. The server detects the
project from its working directory; pass `--dir` or set `GNAR_PROJECT` to override.

### Tools exposed

| Tool | What it does |
|------|--------------|
| `gnar_resume`   | Load where work left off (latest handoff + pinned/decisions/todos). **Call at session start.** |
| `gnar_remember` | Save a decision, fact, todo, note, or snippet. |
| `gnar_recall`   | Semantic + keyword search of shared memory. |
| `gnar_handoff`  | Record goal/state/next-steps/open-questions before switching context. |
| `gnar_context`  | At-a-glance project overview. |
| `gnar_list` · `gnar_get` · `gnar_update` · `gnar_forget` | Browse and manage memories. |

It also exposes MCP **resources** and a **prompt** so clients can auto-load context
without an explicit tool call:

- `gnar://resume` — the resume brief as a readable resource
- `gnar://context` — the project overview as a resource
- the **`resume` prompt** — drops "here's where we left off" into a fresh chat

**Provenance is automatic:** memories written over MCP are tagged with the
connecting client's name (e.g. `claude-code`, `cursor`) as their `source`, so you
can always see which agent wrote what — no configuration needed.

A good agent prompt: *"At the start of a session call `gnar_resume`. Save
decisions and durable facts with `gnar_remember`. Before you run low on context,
call `gnar_handoff`."*

---

## Embeddings (semantic recall)

Recall fuses keyword scoring with vector similarity. By default gnar uses a
**zero-config hash embedder** — no API key, no network, works offline. Its cosine
similarity tracks lexical overlap, so recall is sensible out of the box.

For real semantic quality, point it at an embeddings provider:

```bash
# OpenAI (or any OpenAI-compatible endpoint: LM Studio, llama.cpp, vLLM)
export OPENAI_API_KEY=sk-...
gnar config set embed.provider openai
gnar config set embed.model text-embedding-3-small
gnar reindex     # re-embed existing memories with the new provider

# Local Ollama
gnar config set embed.provider ollama
gnar config set embed.model nomic-embed-text
gnar reindex
```

gnar stamps which embedder built the index and warns (via `gnar status`) if your
config later disagrees — run `gnar reindex` to rebuild.

---

## Configuration

`~/.gnar/config.json` (managed via `gnar config`):

```jsonc
{
  "default_source": "cli",          // label for CLI-written memories
  "embed": {
    "provider": "hash",             // hash | openai | ollama
    "model": "",
    "base_url": "",                 // override provider endpoint
    "api_key_env": "OPENAI_API_KEY",
    "dim": 256                      // hash dimension
  },
  "candidate_cap": 5000             // max memories scanned per recall
}
```

Environment overrides: `GNAR_HOME`, `GNAR_DB`, `GNAR_PROJECT`, `GNAR_SOURCE`,
`GNAR_EMBED_PROVIDER`, `GNAR_EMBED_MODEL`, `GNAR_EMBED_BASE_URL`,
`GNAR_EMBED_DIM`, `GNAR_EMBED_API_KEY`.

---

## How it works

See [ARCHITECTURE.md](./ARCHITECTURE.md). In short: SQLite via pure-Go
`ncruces/go-sqlite3`; embeddings stored as BLOBs; recall loads the project's
candidates and fuses a BM25-lite keyword rank with a cosine-similarity rank via
Reciprocal Rank Fusion. Brute-force KNN in Go is sub-millisecond at personal
scale and avoids any native extension.

---

## Commands

```
gnar remember <text>     save a memory            (r, add)
gnar recall <query>      search                   (search, find)
gnar list                recent memories          (ls)
gnar get <id>            show one                 (show)
gnar edit <id>           edit a memory's fields
gnar forget <id>         archive (--hard deletes) (rm)
gnar pin <id>            pin/unpin (--off)
gnar handoff             record where you are     (ho)
gnar resume              load where you left off
gnar context             project overview         (ctx)
gnar serve               run the MCP server       (mcp)
gnar status              store + embedder health  (doctor)
gnar reindex             re-embed all memories
gnar export [-o file]    export all memories as JSONL
gnar import [file]       import memories (idempotent)
gnar config [...]        manage configuration
```

## Backup & portability

Memories are plain rows in `~/.gnar/gnar.db`. To move them between machines or
back them up, use the embedder-independent JSONL export (vectors are recomputed
on import, so it's portable across embedding providers):

```bash
gnar export -o memories.jsonl     # back up everything
gnar import memories.jsonl         # restore / merge — re-importing is idempotent
```

Import is **atomic** (all-or-nothing): a malformed record aborts the whole import
and leaves the store untouched.

## Contributing

Issues and PRs welcome. The codebase is small and layered
(`model → config/embed/store → engine → mcpserver/cli`); both front-ends are thin
shells over one engine. Before sending a PR:

```bash
make fmt          # gofmt
make vet          # go vet
make race         # tests with the race detector
```

CI runs the same checks plus a build and smoke test on every push.

## License

[MIT](LICENSE) © Asray Gopa
