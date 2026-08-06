package session

import (
	"claude-squad/log"
	"claude-squad/session/git"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DiscoveredRepo struct {
	Name string
	Path string
}

// DiscoverOptions configures DiscoverRepos.
type DiscoverOptions struct {
	// Roots are the directories to scan.
	Roots []string
	// Depth is how many directory levels below each root to scan. Depth < 1
	// is treated as 1.
	Depth int
	// Skip is a list of directory base names (or path suffixes, e.g.
	// "go/pkg") to exclude from the scan.
	Skip []string
}

const maxDiscoveryVisits = 5000

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

// expandRoot expands a leading "~" to the user's home directory.
func expandRoot(root string) string {
	if root == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return root
	}
	if strings.HasPrefix(root, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, root[2:])
		}
	}
	return root
}

func shouldSkip(name, path string, skip []string) bool {
	for _, s := range skip {
		if s == "" {
			continue
		}
		if strings.ContainsRune(s, filepath.Separator) || strings.Contains(s, "/") {
			if strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(s)) {
				return true
			}
			continue
		}
		if name == s {
			return true
		}
	}
	return false
}

// DiscoverRepos lists git repository roots below each root, up to opts.Depth
// levels deep, so a bare name typed into the picker can match a repo that was
// never registered.
func DiscoverRepos(opts DiscoverOptions) []DiscoveredRepo {
	depth := opts.Depth
	if depth < 1 {
		depth = 1
	}

	seen := make(map[string]bool)
	var out []DiscoveredRepo
	visited := 0
	truncated := false

	var walk func(dir string, remaining int)
	walk = func(dir string, remaining int) {
		if truncated {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if truncated {
				return
			}
			visited++
			if visited > maxDiscoveryVisits {
				truncated = true
				log.WarningLog.Printf("discovery truncated after visiting %d directories", maxDiscoveryVisits)
				return
			}

			path := filepath.Join(dir, entry.Name())
			if shouldSkip(entry.Name(), path, opts.Skip) {
				continue
			}

			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				canonical = path
			}
			canonical = filepath.Clean(canonical)

			if seen[canonical] {
				continue
			}

			repoRoot, err := git.FindGitRepoRoot(canonical)
			if err == nil && filepath.Clean(repoRoot) == canonical {
				seen[canonical] = true
				out = append(out, DiscoveredRepo{Name: entry.Name(), Path: canonical})
				continue
			}

			if remaining > 1 {
				walk(path, remaining-1)
			}
		}
	}

	for _, root := range opts.Roots {
		if truncated {
			break
		}
		expanded := expandRoot(root)
		if _, err := os.Stat(expanded); err != nil {
			continue
		}
		walk(expanded, depth)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
