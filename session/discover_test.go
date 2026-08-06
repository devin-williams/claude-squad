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

	got := names(DiscoverRepos(DiscoverOptions{Roots: []string{parent}, Depth: 1}))
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

	got := DiscoverRepos(DiscoverOptions{Roots: []string{parent, parent}, Depth: 1})
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
	roots := DiscoveryRoots(repo)
	if got := names(DiscoverRepos(DiscoverOptions{Roots: roots, Depth: 1})); len(got) != 2 {
		t.Fatalf("expected sibling discovery from inside a repo, got %v", got)
	}

	// Launching from a plain parent scans only its own children.
	plainRoots := DiscoveryRoots(parent)
	if len(plainRoots) != 1 {
		t.Fatalf("expected a single root for a non-repo launch dir, got %v", plainRoots)
	}
}

func TestDiscoveryRootsEmptyLaunchDir(t *testing.T) {
	if roots := DiscoveryRoots(""); roots != nil {
		t.Fatalf("expected nil roots, got %v", roots)
	}
}

func TestDiscoverReposDepth2FindsGrandchildren(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "group", "argonath"))

	got := names(DiscoverRepos(DiscoverOptions{Roots: []string{parent}, Depth: 1}))
	if len(got) != 0 {
		t.Fatalf("expected no repos at depth 1, got %v", got)
	}

	got = names(DiscoverRepos(DiscoverOptions{Roots: []string{parent}, Depth: 2}))
	want := []string{"argonath"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverReposSkipListExcludesDirectory(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "keep"))
	initRepo(t, filepath.Join(parent, "node_modules", "skipped"))

	got := names(DiscoverRepos(DiscoverOptions{
		Roots: []string{parent},
		Depth: 2,
		Skip:  []string{"node_modules"},
	}))
	want := []string{"keep"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverReposDoesNotDescendIntoRepoAtDepth2(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "argonath"))
	initRepo(t, filepath.Join(parent, "argonath", "vendored"))

	got := names(DiscoverRepos(DiscoverOptions{Roots: []string{parent}, Depth: 2}))
	want := []string{"argonath"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected nested repo to be excluded, got %v", got)
	}
}

func TestDiscoverReposExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	tmpRepo, err := os.MkdirTemp(home, "cs-discover-tilde-*")
	if err != nil {
		t.Skip("cannot create temp dir under home")
	}
	defer os.RemoveAll(tmpRepo)
	initRepo(t, filepath.Join(tmpRepo, "argonath"))

	rel := "~/" + filepath.Base(tmpRepo)
	got := names(DiscoverRepos(DiscoverOptions{Roots: []string{rel}, Depth: 1}))
	want := []string{"argonath"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverReposSkipsNonexistentRoot(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "argonath"))
	missing := filepath.Join(parent, "does-not-exist")

	got := names(DiscoverRepos(DiscoverOptions{Roots: []string{parent, missing}, Depth: 1}))
	want := []string{"argonath"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverAllAppliesSkipToLaunchDirPass(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "keep"))
	initRepo(t, filepath.Join(parent, ".nvm"))

	// The launch-dir pass is always run; a skipped directory must not reappear
	// just because cs happened to be launched from its parent.
	got := names(DiscoverAll(parent, DiscoverOptions{Skip: []string{".nvm"}}))
	if len(got) != 1 || got[0] != "keep" {
		t.Fatalf("expected skip to apply to the launch-dir pass, got %v", got)
	}
}

func TestDiscoverAllDedupesAcrossPasses(t *testing.T) {
	parent := t.TempDir()
	initRepo(t, filepath.Join(parent, "argonath"))

	// Same directory reachable from both the launch dir and a configured root.
	got := DiscoverAll(parent, DiscoverOptions{Roots: []string{parent}, Depth: 1})
	if len(got) != 1 {
		t.Fatalf("expected one entry after dedupe, got %v", names(got))
	}
}
