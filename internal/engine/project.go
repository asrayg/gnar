package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Project identifies a workspace namespace.
type Project struct {
	ID   string // canonical key (absolute path of the git root or cwd)
	Name string // friendly name (basename)
}

// DetectProject resolves the project for a working directory.
//
// Precedence:
//  1. GNAR_PROJECT env (used verbatim as both id and name if it isn't a path)
//  2. the git top-level of dir
//  3. dir itself
//
// The id is canonicalized to an absolute path so switching IDEs inside the same
// repo maps to the same memory namespace.
func DetectProject(dir string) Project {
	if v := strings.TrimSpace(os.Getenv("GNAR_PROJECT")); v != "" {
		return projectFromHint(v)
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if root := gitRoot(dir); root != "" {
		dir = root
	}
	abs, err := filepath.Abs(dir)
	if err != nil || abs == "" {
		abs = dir
	}
	abs = filepath.Clean(abs)
	return Project{ID: abs, Name: filepath.Base(abs)}
}

// projectFromHint builds a Project from an explicit hint, which may be a path or
// a plain label.
func projectFromHint(v string) Project {
	if strings.ContainsAny(v, "/\\") || filepath.IsAbs(v) {
		abs, err := filepath.Abs(v)
		if err == nil && abs != "" {
			abs = filepath.Clean(abs)
			return Project{ID: abs, Name: filepath.Base(abs)}
		}
	}
	return Project{ID: v, Name: v}
}

// ResolveProject turns a caller-supplied project string (possibly empty) into a
// Project, falling back to detection from dir.
func ResolveProject(project, dir string) Project {
	if strings.TrimSpace(project) != "" {
		return projectFromHint(project)
	}
	return DetectProject(dir)
}

func gitRoot(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitBranch returns the current branch for dir, or "" if not a repo.
func gitBranch(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
