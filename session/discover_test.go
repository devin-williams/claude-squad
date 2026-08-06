package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", path, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", path, err, out)
	}
}

func names(repos []DiscoveredRepo) []string {
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		out = append(out, r.Name)
	}
	return out
}

func TestDiscoverRepos(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "argonath"))
	initRepo(t, filepath.Join(parent, "remote-agents"))
	if err := os.MkdirAll(filepath.Join(parent, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "loose.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A nested repo must not surface: only one level below a root is scanned.
	initRepo(t, filepath.Join(parent, "argonath", "vendored"))

	got := names(DiscoverRepos([]string{parent}))
	want := []string{"argonath", "remote-agents"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (sorted by name)", got, want)
		}
	}
}

func TestDiscoverReposDedupes(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "argonath"))

	got := DiscoverRepos([]string{parent, parent})
	if len(got) != 1 {
		t.Fatalf("expected dedupe to one entry, got %v", names(got))
	}
}

func TestDiscoveryRootsIncludesParentWhenInsideRepo(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "argonath")
	initRepo(t, repo)
	initRepo(t, filepath.Join(parent, "remote-agents"))

	// Launching from inside a repo should still find its siblings.
	if got := names(DiscoverRepos(DiscoveryRoots(repo))); len(got) != 2 {
		t.Fatalf("expected sibling discovery from inside a repo, got %v", got)
	}

	// Launching from a plain parent scans only its own children.
	roots := DiscoveryRoots(parent)
	if len(roots) != 1 {
		t.Fatalf("expected a single root for a non-repo launch dir, got %v", roots)
	}
}

func TestDiscoveryRootsEmptyLaunchDir(t *testing.T) {
	if roots := DiscoveryRoots(""); roots != nil {
		t.Fatalf("expected nil roots, got %v", roots)
	}
}
