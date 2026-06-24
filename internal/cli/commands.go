package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/asraygopa/gnar/internal/engine"
	"github.com/asraygopa/gnar/internal/model"
)

func cmdRemember(args []string) error {
	fs := flag.NewFlagSet("remember", flag.ContinueOnError)
	kind := fs.String("kind", "note", "kind: note|decision|fact|todo|snippet")
	fs.StringVar(kind, "k", "note", "kind (shorthand)")
	title := fs.String("title", "", "short title")
	fs.StringVar(title, "t", "", "title (shorthand)")
	project := projectFlag(fs)
	var tags, files stringSlice
	fs.Var(&tags, "tag", "tag (repeatable)")
	fs.Var(&files, "file", "related file (repeatable)")
	pin := fs.Bool("pin", false, "pin this memory")
	source := fs.String("source", "", "source agent/IDE (default from config)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	content := strings.TrimSpace(strings.Join(pos, " "))
	if content == "-" {
		s, err := readStdin()
		if err != nil {
			return err
		}
		content = s
	}
	if content == "" && *title == "" {
		return fmt.Errorf("nothing to remember (provide text, or - to read stdin)")
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
	m, err := eng.Remember(context.Background(), engine.RememberInput{
		Project: *project,
		Dir:     cwd(),
		Kind:    model.Kind(strings.ToLower(*kind)),
		Title:   *title,
		Content: content,
		Tags:    tags,
		Files:   files,
		Source:  src,
		Pinned:  *pin,
	})
	if err != nil {
		return err
	}
	fmt.Printf("remembered #%d (%s)\n", m.ID, m.Kind)
	return nil
}

func cmdRecall(args []string) error {
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	limit := fs.Int("n", 10, "max results")
	kinds := fs.String("kind", "", "comma-separated kinds to include")
	fs.StringVar(kinds, "k", "", "kinds (shorthand)")
	tags := fs.String("tag", "", "comma-separated tags to require")
	project := projectFlag(fs)
	all := fs.Bool("all", false, "search across all projects")
	asJSON := jsonFlag(fs)
	verbose := fs.Bool("l", false, "long output (show content)")
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
	mems, err := eng.Recall(context.Background(), engine.RecallInput{
		Project:     *project,
		Dir:         cwd(),
		AllProjects: *all,
		Query:       query,
		Kinds:       parseKinds(*kinds),
		Tags:        commaList(*tags),
		Limit:       *limit,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(mems)
	}
	printMemList(mems, *verbose)
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	limit := fs.Int("n", 20, "max results")
	kinds := fs.String("kind", "", "comma-separated kinds to include")
	fs.StringVar(kinds, "k", "", "kinds (shorthand)")
	project := projectFlag(fs)
	all := fs.Bool("all", false, "list across all projects")
	asJSON := jsonFlag(fs)
	verbose := fs.Bool("l", false, "long output (show content)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	mems, err := eng.List(engine.ListInput{
		Project:     *project,
		Dir:         cwd(),
		AllProjects: *all,
		Kinds:       parseKinds(*kinds),
		Limit:       *limit,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(mems)
	}
	printMemList(mems, *verbose)
	return nil
}

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	asJSON := jsonFlag(fs)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	id, err := parseID(pos)
	if err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	m, ok, err := eng.Get(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("memory %d not found", id)
	}
	if *asJSON {
		return printJSON(m)
	}
	printMemFull(m)
	return nil
}

func cmdForget(args []string) error {
	fs := flag.NewFlagSet("forget", flag.ContinueOnError)
	hard := fs.Bool("hard", false, "permanently delete instead of archiving")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	id, err := parseID(pos)
	if err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	ok, err := eng.Forget(id, *hard)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("memory %d not found", id)
	}
	verb := "archived"
	if *hard {
		verb = "deleted"
	}
	fmt.Printf("%s #%d\n", verb, id)
	return nil
}

func cmdPin(args []string) error {
	fs := flag.NewFlagSet("pin", flag.ContinueOnError)
	unpin := fs.Bool("off", false, "unpin instead of pin")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	id, err := parseID(pos)
	if err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	pinned := !*unpin
	if _, err := eng.Update(context.Background(), engine.UpdateInput{ID: id, Pinned: &pinned}); err != nil {
		return err
	}
	state := "pinned"
	if *unpin {
		state = "unpinned"
	}
	fmt.Printf("%s #%d\n", state, id)
	return nil
}

// --- shared flag/print helpers ---

func projectFlag(fs *flag.FlagSet) *string {
	p := fs.String("project", "", "project namespace (default: auto-detect)")
	fs.StringVar(p, "p", "", "project (shorthand)")
	return p
}

func jsonFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("json", false, "output JSON")
}

func parseKinds(s string) []model.Kind {
	var out []model.Kind
	for _, k := range commaList(s) {
		kind := model.Kind(strings.ToLower(k))
		if model.ValidKind(kind) {
			out = append(out, kind)
		}
	}
	return out
}

func parseID(args []string) (int64, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("missing memory id")
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q", args[0])
	}
	return id, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printMemList(mems []model.Memory, verbose bool) {
	if len(mems) == 0 {
		fmt.Println("(no memories)")
		return
	}
	for _, m := range mems {
		line := m.Title
		if line == "" {
			line = firstLine(m.Content, 90)
		}
		fmt.Printf("#%-5d %-9s %s", m.ID, m.Kind, line)
		if m.Pinned {
			fmt.Print("  📌")
		}
		if len(m.Tags) > 0 {
			fmt.Printf("  {%s}", strings.Join(m.Tags, ", "))
		}
		fmt.Println()
		if verbose && m.Content != "" && m.Content != line {
			for _, ln := range strings.Split(strings.TrimRight(m.Content, "\n"), "\n") {
				fmt.Printf("      %s\n", ln)
			}
		}
	}
}

func printMemFull(m model.Memory) {
	fmt.Printf("#%d  %s\n", m.ID, m.Kind)
	if m.Title != "" {
		fmt.Printf("title:   %s\n", m.Title)
	}
	fmt.Printf("project: %s\n", m.Project)
	if len(m.Tags) > 0 {
		fmt.Printf("tags:    %s\n", strings.Join(m.Tags, ", "))
	}
	if len(m.Files) > 0 {
		fmt.Printf("files:   %s\n", strings.Join(m.Files, ", "))
	}
	if m.Source != "" {
		fmt.Printf("source:  %s\n", m.Source)
	}
	fmt.Printf("created: %s\n", m.CreatedAt.Format("2006-01-02 15:04"))
	if m.Pinned {
		fmt.Println("pinned:  yes")
	}
	fmt.Printf("\n%s\n", strings.TrimRight(m.Content, "\n"))
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
