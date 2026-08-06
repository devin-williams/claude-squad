package git

import (
	"claude-squad/config"
	"fmt"
	"path/filepath"
	"strings"
)

// guardDestructive requires path to be a strict descendant of the global
// worktrees directory or of some registered workspace's worktree root, so a
// bad path (empty, "/", $HOME, or the root itself) can never reach a
// worktree-remove/RemoveAll call.
func guardDestructive(path string) error {
	if path == "" {
		return fmt.Errorf("refusing destructive operation on empty path")
	}

	resolved, err := resolveForGuard(path)
	if err != nil {
		return err
	}

	roots, err := destructiveRoots()
	if err != nil {
		return err
	}

	for _, root := range roots {
		resolvedRoot, err := resolveForGuard(root)
		if err != nil {
			continue
		}
		if isStrictDescendant(resolvedRoot, resolved) {
			return nil
		}
	}

	return fmt.Errorf("refusing destructive operation on %q: not inside a managed worktree root", path)
}

// resolveForGuard resolves path to an absolute, symlink-free form. If path
// itself doesn't exist yet (e.g. a worktree not yet created), it resolves the
// deepest existing ancestor and rejoins the missing suffix, so a not-yet-existing
// child still compares correctly against an already-resolved root.
func resolveForGuard(path string) (string, error) {
	cleaned := filepath.Clean(path)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path for %q: %w", path, err)
	}

	var suffix []string
	current := abs
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Join(append([]string{resolved}, suffix...)...), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}

func isStrictDescendant(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func destructiveRoots() ([]string, error) {
	var roots []string

	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree directory: %w", err)
	}
	roots = append(roots, worktreesDir)

	reg := config.LoadWorkspaceRegistry()
	for _, ws := range reg.Workspaces {
		root, err := ws.WorktreeRoot()
		if err != nil {
			continue
		}
		roots = append(roots, root)
	}

	return roots, nil
}

// GuardDestructive is the exported entry point for other packages (e.g.
// session) that need to validate a path before a destructive filesystem
// operation, without duplicating the boundary-check logic here.
func GuardDestructive(path string) error {
	return guardDestructive(path)
}
