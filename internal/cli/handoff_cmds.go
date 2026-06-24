package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/asraygopa/gnar/internal/engine"
	"github.com/asraygopa/gnar/internal/model"
)

func cmdHandoff(args []string) error {
	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	goal := fs.String("goal", "", "what you're trying to accomplish")
	fs.StringVar(goal, "g", "", "goal (shorthand)")
	state := fs.String("state", "", "where things stand now")
	fs.StringVar(state, "s", "", "state (shorthand)")
	branch := fs.String("branch", "", "git branch (auto-detected if omitted)")
	project := projectFlag(fs)
	source := fs.String("source", "", "source agent/IDE (default from config)")
	var next, open, files stringSlice
	fs.Var(&next, "next", "a next step (repeatable)")
	fs.Var(&open, "open", "an open question/blocker (repeatable)")
	fs.Var(&files, "file", "a file in play (repeatable)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	// Any trailing words become the state if --state not given.
	if *state == "" {
		if extra := strings.TrimSpace(strings.Join(pos, " ")); extra != "" {
			*state = extra
		}
	}

	eng, cfg, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	src := *source
	if src == "" {
		src = cfg.DefaultSource
	}
	m, err := eng.Handoff(context.Background(), engine.HandoffInput{
		Project: *project,
		Dir:     cwd(),
		Source:  src,
		Handoff: model.Handoff{
			Goal:      *goal,
			State:     *state,
			NextSteps: next,
			OpenQs:    open,
			Files:     files,
			Branch:    *branch,
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("handoff #%d recorded. Next session: gnar resume\n", m.ID)
	return nil
}

func cmdResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	project := projectFlag(fs)
	asJSON := jsonFlag(fs)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(pos, " "))
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	b, err := eng.Resume(context.Background(), engine.ResumeInput{Project: *project, Dir: cwd(), Query: query})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(b)
	}
	fmt.Print(b.Brief)
	return nil
}

func cmdContext(args []string) error {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	project := projectFlag(fs)
	asJSON := jsonFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	b, err := eng.Context(context.Background(), *project, cwd())
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(b)
	}
	fmt.Print(b.Brief)
	return nil
}
