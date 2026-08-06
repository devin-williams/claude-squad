package session

import (
	"claude-squad/session/git"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type TargetMode int

const (
	ModeWorktree TargetMode = iota
	ModeInPlace
)

// SessionTarget is the resolved, canonical destination for a typed path.
type SessionTarget struct {
	Mode     TargetMode
	Dir      string // canonical abs path: the REPO ROOT for ModeWorktree, the dir itself for ModeInPlace
	SubDir   string // for ModeWorktree: path of the typed dir RELATIVE to Dir; "" when the typed path IS the repo root
	Existing bool   // true if an entry for this target is already in the workspace registry
}

func expandPath(raw, launchDir string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = os.ExpandEnv(raw)
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		raw = filepath.Join(home, raw[1:])
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(launchDir, raw)
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return abs, nil
}

// ResolveSessionTarget resolves a raw, user-typed path (relative or absolute,
// possibly containing "~" or env vars) into a canonical SessionTarget.
// launchDir is used to resolve relative paths and must be supplied by the
// caller (never os.Getwd() here) so this stays testable.
func ResolveSessionTarget(raw, launchDir string) (SessionTarget, error) {
	abs, err := expandPath(raw, launchDir)
	if err != nil {
		return SessionTarget{}, err
	}

	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		return SessionTarget{}, fmt.Errorf("%s: %w", abs, os.ErrNotExist)
	}
	if err != nil {
		return SessionTarget{}, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return SessionTarget{}, fmt.Errorf("%s is not a directory", abs)
	}

	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return SessionTarget{}, fmt.Errorf("resolve symlinks for %s: %w", abs, err)
	}

	if !git.IsGitRepo(canonical) {
		return SessionTarget{Mode: ModeInPlace, Dir: canonical}, nil
	}

	root, err := git.FindGitRepoRoot(canonical)
	if err != nil {
		return SessionTarget{}, fmt.Errorf("find git repo root for %s: %w", canonical, err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return SessionTarget{}, fmt.Errorf("resolve symlinks for %s: %w", root, err)
	}

	if root == canonical {
		return SessionTarget{Mode: ModeWorktree, Dir: root}, nil
	}

	rel, err := filepath.Rel(root, canonical)
	if err != nil {
		return SessionTarget{}, fmt.Errorf("relativize %s against %s: %w", canonical, root, err)
	}
	return SessionTarget{Mode: ModeWorktree, Dir: root, SubDir: rel}, nil
}

type TargetKind int

const (
	KindRepoRoot TargetKind = iota
	KindInsideRepo
	KindNonGit
	KindMissing
	KindNotDir
)

// ClassifyPath is a lightweight, error-free classifier for annotating rows in
// a picker UI without running the full resolve-and-error path.
func ClassifyPath(raw, launchDir string) (TargetKind, string) {
	abs, err := expandPath(raw, launchDir)
	if err != nil {
		return KindMissing, "does not exist"
	}

	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		return KindMissing, "does not exist"
	}
	if err != nil {
		return KindMissing, "does not exist"
	}
	if !info.IsDir() {
		return KindNotDir, "not a directory"
	}

	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		canonical = abs
	}

	if !git.IsGitRepo(canonical) {
		return KindNonGit, "no .git — edits in place"
	}

	root, err := git.FindGitRepoRoot(canonical)
	if err != nil {
		return KindNonGit, "no .git — edits in place"
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}

	if root == canonical {
		return KindRepoRoot, "git repo"
	}
	return KindInsideRepo, fmt.Sprintf("inside repo %s → worktree", filepath.Base(root))
}
