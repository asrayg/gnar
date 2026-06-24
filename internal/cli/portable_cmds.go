package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/asraygopa/gnar/internal/engine"
	"github.com/asraygopa/gnar/internal/model"
)

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("o", "", "output file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	n, err := eng.Export(w)
	if err != nil {
		return err
	}
	if *out != "" {
		fmt.Fprintf(os.Stderr, "exported %d memories to %s\n", n, *out)
	}
	return nil
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()

	r := os.Stdin
	if len(pos) > 0 && pos[0] != "-" {
		f, err := os.Open(pos[0])
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	res, err := eng.Import(context.Background(), r)
	if err != nil {
		// Import is atomic — on error nothing was written.
		return err
	}
	fmt.Printf("imported %d, skipped %d (duplicates)\n", res.Added, res.Skipped)
	return nil
}

func cmdEdit(args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	title := fs.String("title", "", "new title")
	fs.StringVar(title, "t", "", "title (shorthand)")
	content := fs.String("content", "", "new content (use - to read stdin)")
	fs.StringVar(content, "c", "", "content (shorthand)")
	kind := fs.String("kind", "", "new kind")
	fs.StringVar(kind, "k", "", "kind (shorthand)")
	tags := fs.String("tag", "", "comma-separated replacement tags")
	pin := fs.String("pin", "", "set pinned: true|false")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	id, err := parseID(pos)
	if err != nil {
		return err
	}

	patch := engine.UpdateInput{ID: id}
	fields := 0
	var visitErr error
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title", "t":
			v := *title
			patch.Title = &v
			fields++
		case "content", "c":
			v := *content
			if v == "-" {
				s, err := readStdin()
				if err != nil {
					visitErr = fmt.Errorf("reading stdin: %w", err)
					return
				}
				v = s
			}
			patch.Content = &v
			fields++
		case "kind", "k":
			k := model.Kind(strings.ToLower(*kind))
			patch.Kind = &k
			fields++
		case "tag":
			t := commaList(*tags)
			patch.Tags = &t
			fields++
		case "pin":
			b, err := strconv.ParseBool(*pin)
			if err != nil {
				visitErr = fmt.Errorf("invalid --pin value %q (use true or false)", *pin)
				return
			}
			patch.Pinned = &b
			fields++
		}
	})
	if visitErr != nil {
		return visitErr
	}
	if fields == 0 {
		return fmt.Errorf("nothing to edit: provide at least one of -t/-c/-k/--tag/--pin")
	}

	eng, _, err := openEngine()
	if err != nil {
		return err
	}
	defer eng.Close()
	m, err := eng.Update(context.Background(), patch)
	if err != nil {
		return err
	}
	fmt.Printf("updated #%d\n", m.ID)
	return nil
}
