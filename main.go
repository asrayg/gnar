// Command gnar is shared memory for switching IDEs, agents, and context windows.
//
// It is two things in one binary: an MCP server (`gnar serve`) that any agent or
// IDE can connect to, and a CLI for humans. Both are thin shells over one engine
// backed by a local SQLite database with semantic + keyword recall.
package main

import (
	"os"

	"github.com/asraygopa/gnar/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
