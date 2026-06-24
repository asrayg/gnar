package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/asraygopa/gnar/internal/config"
	"github.com/asraygopa/gnar/internal/mcpserver"
	"github.com/asraygopa/gnar/internal/model"
)

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := fs.String("dir", "", "working directory for project detection (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()

	workdir := *dir
	if workdir == "" {
		workdir = cwd()
	}
	// stdout is reserved for the JSON-RPC stream; log to stderr only.
	fmt.Fprintf(os.Stderr, "gnar mcp server (v%s) ready on stdio — project dir %s\n", mcpserver.Version, workdir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := mcpserver.New(eng, workdir)
	if err := srv.Run(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func cmdReindex(args []string) error {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	eng, cfg, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	fmt.Fprintf(os.Stderr, "re-embedding with %s ...\n", eng.Embedder().ID())
	_ = cfg
	n, err := eng.Reindex(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("re-embedded %d memories\n", n)
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := jsonFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	st, err := eng.Stats()
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(st)
	}
	fmt.Printf("gnar %s\n", Version)
	fmt.Printf("db:       %s\n", st.DBPath)
	fmt.Printf("memories: %d across %d project(s)\n", st.Total, st.Projects)
	fmt.Printf("embedder: %s (dim %d)\n", st.Embedder, st.StoredDim)
	if !st.EmbedMatch {
		fmt.Println("⚠ embedder differs from the one used to build the index — run `gnar reindex`")
	}
	if len(st.ByKind) > 0 {
		fmt.Println("by kind:")
		for _, k := range model.AllKinds {
			if c := st.ByKind[k]; c > 0 {
				fmt.Printf("  %-9s %d\n", k, c)
			}
		}
	}
	if len(st.TopProj) > 0 {
		fmt.Println("top projects:")
		for _, p := range st.TopProj {
			fmt.Printf("  %-4d %s\n", p.Count, p.Name)
		}
	}
	return nil
}

func cmdConfig(args []string) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "path":
		fmt.Println(config.ConfigPath())
		return nil
	case "show", "get":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(rest) == 1 {
			// `config get <key>`
			v, ok := configValue(cfg, rest[0])
			if !ok {
				return fmt.Errorf("unknown config key %q", rest[0])
			}
			fmt.Println(v)
			return nil
		}
		return printJSON(cfg)
	case "set":
		return configSet(rest)
	default:
		return fmt.Errorf("usage: gnar config [show|path|get <key>|set <key> <value>]")
	}
}

func configValue(cfg *config.Config, key string) (string, bool) {
	switch key {
	case "default_source":
		return cfg.DefaultSource, true
	case "embed.provider":
		return cfg.Embed.Provider, true
	case "embed.model":
		return cfg.Embed.Model, true
	case "embed.base_url":
		return cfg.Embed.BaseURL, true
	case "embed.dim":
		return fmt.Sprint(cfg.Embed.Dim), true
	case "candidate_cap":
		return fmt.Sprint(cfg.CandidateCap), true
	default:
		return "", false
	}
}

func configSet(rest []string) error {
	if len(rest) != 2 {
		return fmt.Errorf("usage: gnar config set <key> <value>")
	}
	key, val := rest[0], rest[1]
	// Load without env overlay so an active GNAR_* override is not persisted.
	cfg, err := config.LoadRaw()
	if err != nil {
		return err
	}
	switch key {
	case "default_source":
		cfg.DefaultSource = val
	case "embed.provider":
		cfg.Embed.Provider = val
	case "embed.model":
		cfg.Embed.Model = val
	case "embed.base_url":
		cfg.Embed.BaseURL = val
	case "embed.dim":
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err != nil || n <= 0 {
			return fmt.Errorf("embed.dim must be a positive integer")
		}
		cfg.Embed.Dim = n
	case "candidate_cap":
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err != nil || n <= 0 {
			return fmt.Errorf("candidate_cap must be a positive integer")
		}
		cfg.CandidateCap = n
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("set %s = %s\n", key, val)
	if strings.HasPrefix(key, "embed.") {
		fmt.Println("note: changing the embedder may require `gnar reindex`")
	}
	return nil
}
