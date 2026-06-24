// Package cli implements the `gnar` command-line interface — the human-facing
// front-end over the engine. It mirrors the MCP tools so the two never diverge.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/asraygopa/gnar/internal/config"
	"github.com/asraygopa/gnar/internal/engine"
)

// Version is the CLI/build version. Overridable at build time via:
//
//	-ldflags "-X github.com/asraygopa/gnar/internal/cli.Version=v1.2.3"
var Version = "0.1.0"

// Run dispatches a subcommand and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage(os.Stdout)
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "remember", "r", "add":
		return run(cmdRemember, rest)
	case "recall", "search", "find":
		return run(cmdRecall, rest)
	case "handoff", "ho":
		return run(cmdHandoff, rest)
	case "resume":
		return run(cmdResume, rest)
	case "context", "ctx":
		return run(cmdContext, rest)
	case "list", "ls":
		return run(cmdList, rest)
	case "get", "show":
		return run(cmdGet, rest)
	case "forget", "rm":
		return run(cmdForget, rest)
	case "pin":
		return run(cmdPin, rest)
	case "edit":
		return run(cmdEdit, rest)
	case "export":
		return run(cmdExport, rest)
	case "import":
		return run(cmdImport, rest)
	case "serve", "mcp":
		return run(cmdServe, rest)
	case "reindex":
		return run(cmdReindex, rest)
	case "status", "doctor", "stats":
		return run(cmdStatus, rest)
	case "config":
		return run(cmdConfig, rest)
	case "version", "--version", "-v":
		fmt.Println("gnar", Version)
		return 0
	case "help", "--help", "-h":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "gnar: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

// run executes a command func and maps errors to an exit code.
func run(fn func([]string) error, args []string) int {
	if err := fn(args); err != nil {
		fmt.Fprintln(os.Stderr, "gnar:", err)
		return 1
	}
	return 0
}

// openEngine loads config and opens the engine.
func openEngine() (*engine.Engine, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	eng, err := engine.Open(cfg)
	if err != nil {
		return nil, nil, err
	}
	return eng, cfg, nil
}

// cwd returns the current working directory (best effort).
func cwd() string {
	d, _ := os.Getwd()
	return d
}

// parseInterspersed parses flags that may appear before, after, or between
// positional arguments — Go's flag package stops at the first positional, which
// is surprising for `gnar remember "text" --tag x`. It returns the positionals
// in order. Repeatable (stringSlice) flags accumulate across the re-parses.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	return positional, nil
}

// stringSlice is a repeatable string flag (e.g. --next a --next b).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// commaList splits a comma-separated flag value into trimmed, non-empty parts.
func commaList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// readStdin reads all of stdin (used when content is "-").
func readStdin() (string, error) {
	b, err := io.ReadAll(os.Stdin)
	return string(b), err
}

func usage(w io.Writer) {
	fmt.Fprint(w, `gnar — shared memory for switching IDEs, agents, and context windows

USAGE
  gnar <command> [flags]

MEMORY
  remember <text>        Save a memory            (aliases: r, add)
  recall <query>         Search memory            (aliases: search, find)
  list                   List recent memories     (alias: ls)
  get <id>               Show one memory          (alias: show)
  edit <id>              Edit a memory's fields
  forget <id>            Archive a memory         (alias: rm)
  pin <id>               Pin/unpin a memory

CONTEXT HANDOFF
  handoff                Record where you are     (alias: ho)
  resume                 Load where you left off
  context                Project overview         (alias: ctx)

SERVER & ADMIN
  serve                  Run the MCP server (stdio)   (alias: mcp)
  status                 Store + embedder health      (aliases: doctor, stats)
  reindex                Re-embed all memories
  export [-o file]       Export all memories as JSONL
  import [file]          Import memories from JSONL (idempotent)
  config [path|get|set]  Manage configuration
  version                Print version

Run "gnar <command> -h" for command flags.
Memories are scoped to the current project (auto-detected from the git root).
`)
}
