package session

import (
	"claude-squad/session/git"
	"os"
	"path/filepath"
	"sort"
)

type DiscoveredRepo struct {
	Name string
	Path string
}

// DiscoveryRoots returns the directories whose children are worth scanning: the
// launch dir, plus its parent when the launch dir is itself a repo, so launching
// from inside a project still finds its siblings.
func DiscoveryRoots(launchDir string) []string {
	if launchDir == "" {
		return nil
	}
	abs, err := filepath.Abs(launchDir)
	if err != nil {
		return nil
	}
	// git reports resolved paths, so compare canonically or the repo check below never matches.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	roots := []string{abs}
	if root, err := git.FindGitRepoRoot(abs); err == nil && filepath.Clean(root) == abs {
		if parent := filepath.Dir(abs); parent != abs {
			roots = append(roots, parent)
		}
	}
	return roots
}

// DiscoverRepos lists git repository roots one level below each root, so a bare
// name typed into the picker can match a repo that was never registered.
func DiscoverRepos(roots []string) []DiscoveredRepo {
	seen := make(map[string]bool)
	var out []DiscoveredRepo
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				canonical = path
			}
			if seen[canonical] {
				continue
			}
			repoRoot, err := git.FindGitRepoRoot(canonical)
			if err != nil || filepath.Clean(repoRoot) != filepath.Clean(canonical) {
				continue
			}
			seen[canonical] = true
			out = append(out, DiscoveredRepo{Name: entry.Name(), Path: canonical})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
