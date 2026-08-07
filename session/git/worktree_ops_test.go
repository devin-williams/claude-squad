package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupFromExistingBranch_RemovesOrphanedDirectory(t *testing.T) {
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	defer func() {
		_ = os.Setenv("HOME", originalHome)
	}()

	repoPath := filepath.Join(t.TempDir(), "repo")
	mustRunGit(t, "", "init", repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	mustRunGit(t, repoPath, "add", "README.md")
	mustRunGit(t, repoPath, "commit", "-m", "initial")
	mustRunGit(t, repoPath, "branch", "feature/test")

	worktreePath := filepath.Join(tempHome, ".claude-squad", "worktrees", "feature-test")
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("mkdir orphaned worktree: %v", err)
	}

	junkPath := filepath.Join(worktreePath, "orphan.txt")
	if err := os.WriteFile(junkPath, []byte("orphaned\n"), 0644); err != nil {
		t.Fatalf("write orphan marker: %v", err)
	}

	g := &GitWorktree{
		repoPath:         repoPath,
		worktreePath:     worktreePath,
		branchName:       "feature/test",
		isExistingBranch: true,
	}

	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if _, err := os.Stat(junkPath); !os.IsNotExist(err) {
		t.Fatalf("orphan marker still exists after Setup, err = %v", err)
	}

	if valid, err := g.IsValidWorktree(); err != nil {
		t.Fatalf("IsValidWorktree() error = %v", err)
	} else if !valid {
		t.Fatal("expected Setup() to recreate a valid worktree")
	}

	currentBranch := mustRunGit(t, worktreePath, "branch", "--show-current")
	if currentBranch != "feature/test\n" {
		t.Fatalf("current branch = %q, want %q", currentBranch, "feature/test\n")
	}
}

func TestSetupFromExistingBranch_RecordsBaseCommit(t *testing.T) {
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	defer func() {
		_ = os.Setenv("HOME", originalHome)
	}()

	repoPath := filepath.Join(t.TempDir(), "repo")
	mustRunGit(t, "", "init", repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	mustRunGit(t, repoPath, "add", "README.md")
	mustRunGit(t, repoPath, "commit", "-m", "initial")
	mustRunGit(t, repoPath, "branch", "feature/test")

	g := &GitWorktree{
		repoPath:         repoPath,
		worktreePath:     filepath.Join(tempHome, ".claude-squad", "worktrees", "feature-test"),
		branchName:       "feature/test",
		isExistingBranch: true,
	}

	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	want := strings.TrimSpace(mustRunGit(t, repoPath, "rev-parse", "feature/test"))
	if got := g.GetBaseCommitSHA(); got != want {
		t.Fatalf("GetBaseCommitSHA() = %q, want %q", got, want)
	}
}

func setupTempHome(t *testing.T) string {
	t.Helper()
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", originalHome)
	})
	return tempHome
}

// initRepoWithCommit creates a repo at repoPath with one commit on branch
// initialBranch and returns that commit's SHA.
func initRepoWithCommit(t *testing.T, repoPath, initialBranch, fileName, content, message string) string {
	t.Helper()
	mustRunGit(t, "", "init", "-b", initialBranch, repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")

	filePath := filepath.Join(repoPath, fileName)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", fileName, err)
	}
	mustRunGit(t, repoPath, "add", fileName)
	mustRunGit(t, repoPath, "commit", "-m", message)
	return strings.TrimSpace(mustRunGit(t, repoPath, "rev-parse", "HEAD"))
}

func TestSetupNewWorktree_UsesOriginDefaultBranchNotFeatureBranch(t *testing.T) {
	tempHome := setupTempHome(t)

	remotePath := filepath.Join(t.TempDir(), "remote")
	mustRunGit(t, "", "init", "--bare", "-b", "main", remotePath)

	seedPath := filepath.Join(t.TempDir(), "seed")
	initRepoWithCommit(t, seedPath, "main", "README.md", "hello\n", "initial")
	mustRunGit(t, seedPath, "remote", "add", "origin", remotePath)
	mustRunGit(t, seedPath, "push", "origin", "main")

	repoPath := filepath.Join(t.TempDir(), "repo")
	mustRunGit(t, "", "clone", remotePath, repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")
	mustRunGit(t, repoPath, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repoPath, "feature.txt"), []byte("wip\n"), 0644); err != nil {
		t.Fatalf("write feature.txt: %v", err)
	}
	mustRunGit(t, repoPath, "add", "feature.txt")
	mustRunGit(t, repoPath, "commit", "-m", "feature work")

	// Advance the remote's main past the point the local clone knows about.
	remoteWorktree := filepath.Join(t.TempDir(), "remote-checkout")
	mustRunGit(t, "", "clone", remotePath, remoteWorktree)
	mustRunGit(t, remoteWorktree, "config", "user.name", "Test User")
	mustRunGit(t, remoteWorktree, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(remoteWorktree, "upstream.txt"), []byte("ahead\n"), 0644); err != nil {
		t.Fatalf("write upstream.txt: %v", err)
	}
	mustRunGit(t, remoteWorktree, "add", "upstream.txt")
	mustRunGit(t, remoteWorktree, "commit", "-m", "upstream progress")
	mustRunGit(t, remoteWorktree, "push", "origin", "main")
	wantSHA := strings.TrimSpace(mustRunGit(t, remoteWorktree, "rev-parse", "main"))

	g := &GitWorktree{
		repoPath:     repoPath,
		worktreePath: filepath.Join(tempHome, ".claude-squad", "worktrees", "new-session"),
		branchName:   "session/new",
		baseFetch:    true,
	}

	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got := g.GetBaseCommitSHA(); got != wantSHA {
		t.Fatalf("GetBaseCommitSHA() = %q, want origin/main SHA %q", got, wantSHA)
	}

	worktreeHead := strings.TrimSpace(mustRunGit(t, g.worktreePath, "rev-parse", "HEAD"))
	if worktreeHead != wantSHA {
		t.Fatalf("worktree HEAD = %q, want %q", worktreeHead, wantSHA)
	}

	featureSHA := strings.TrimSpace(mustRunGit(t, repoPath, "rev-parse", "feature"))
	if worktreeHead == featureSHA {
		t.Fatal("worktree HEAD should not equal the checked-out feature branch's SHA")
	}
}

func TestSetupNewWorktree_ConfiguredBranchResolvesOrigin(t *testing.T) {
	tempHome := setupTempHome(t)

	remotePath := filepath.Join(t.TempDir(), "remote")
	mustRunGit(t, "", "init", "-b", "main", remotePath)
	mustRunGit(t, remotePath, "config", "user.name", "Test User")
	mustRunGit(t, remotePath, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(remotePath, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRunGit(t, remotePath, "add", "README.md")
	mustRunGit(t, remotePath, "commit", "-m", "initial")
	mustRunGit(t, remotePath, "checkout", "-b", "develop")
	if err := os.WriteFile(filepath.Join(remotePath, "develop.txt"), []byte("dev\n"), 0644); err != nil {
		t.Fatalf("write develop.txt: %v", err)
	}
	mustRunGit(t, remotePath, "add", "develop.txt")
	mustRunGit(t, remotePath, "commit", "-m", "develop work")
	wantSHA := strings.TrimSpace(mustRunGit(t, remotePath, "rev-parse", "develop"))
	mustRunGit(t, remotePath, "checkout", "main")

	repoPath := filepath.Join(t.TempDir(), "repo")
	mustRunGit(t, "", "clone", remotePath, repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")

	g := &GitWorktree{
		repoPath:     repoPath,
		worktreePath: filepath.Join(tempHome, ".claude-squad", "worktrees", "new-session"),
		branchName:   "session/new",
		baseFetch:    true,
		baseBranch:   "develop",
	}

	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got := g.GetBaseCommitSHA(); got != wantSHA {
		t.Fatalf("GetBaseCommitSHA() = %q, want origin/develop SHA %q", got, wantSHA)
	}
}

func TestSetupNewWorktree_DefaultBranchMaster(t *testing.T) {
	tempHome := setupTempHome(t)

	remotePath := filepath.Join(t.TempDir(), "remote")
	mustRunGit(t, "", "init", "-b", "master", remotePath)
	mustRunGit(t, remotePath, "config", "user.name", "Test User")
	mustRunGit(t, remotePath, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(remotePath, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRunGit(t, remotePath, "add", "README.md")
	mustRunGit(t, remotePath, "commit", "-m", "initial")
	wantSHA := strings.TrimSpace(mustRunGit(t, remotePath, "rev-parse", "master"))

	repoPath := filepath.Join(t.TempDir(), "repo")
	mustRunGit(t, "", "clone", remotePath, repoPath)
	mustRunGit(t, repoPath, "config", "user.name", "Test User")
	mustRunGit(t, repoPath, "config", "user.email", "test@example.com")
	mustRunGit(t, repoPath, "checkout", "-b", "feature")

	g := &GitWorktree{
		repoPath:     repoPath,
		worktreePath: filepath.Join(tempHome, ".claude-squad", "worktrees", "new-session"),
		branchName:   "session/new",
		baseFetch:    true,
	}

	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got := g.GetBaseCommitSHA(); got != wantSHA {
		t.Fatalf("GetBaseCommitSHA() = %q, want master SHA %q", got, wantSHA)
	}
}

func TestSetupNewWorktree_NoOriginFallsBackToLocalHEAD(t *testing.T) {
	tempHome := setupTempHome(t)

	repoPath := filepath.Join(t.TempDir(), "repo")
	wantSHA := initRepoWithCommit(t, repoPath, "main", "README.md", "hello\n", "initial")

	g := &GitWorktree{
		repoPath:     repoPath,
		worktreePath: filepath.Join(tempHome, ".claude-squad", "worktrees", "new-session"),
		branchName:   "session/new",
		baseFetch:    true,
	}

	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got := g.GetBaseCommitSHA(); got != wantSHA {
		t.Fatalf("GetBaseCommitSHA() = %q, want local HEAD SHA %q", got, wantSHA)
	}

	if valid, err := g.IsValidWorktree(); err != nil || !valid {
		t.Fatalf("expected a valid worktree, valid=%v err=%v", valid, err)
	}
}

func TestSetupNewWorktree_FetchFailureWarnsAndFallsBack(t *testing.T) {
	tempHome := setupTempHome(t)

	repoPath := filepath.Join(t.TempDir(), "repo")
	wantSHA := initRepoWithCommit(t, repoPath, "main", "README.md", "hello\n", "initial")

	// Point origin at a path that doesn't exist so fetch fails.
	mustRunGit(t, repoPath, "remote", "add", "origin", filepath.Join(t.TempDir(), "does-not-exist"))

	g := &GitWorktree{
		repoPath:     repoPath,
		worktreePath: filepath.Join(tempHome, ".claude-squad", "worktrees", "new-session"),
		branchName:   "session/new",
		baseFetch:    true,
	}

	if err := g.Setup(); err != nil {
		t.Fatalf("Setup() error = %v, want fetch failure to be non-fatal", err)
	}

	if got := g.GetBaseCommitSHA(); got != wantSHA {
		t.Fatalf("GetBaseCommitSHA() = %q, want local HEAD SHA %q", got, wantSHA)
	}
}

func mustRunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmdArgs := args
	if dir != "" {
		cmdArgs = append([]string{"-C", dir}, args...)
	}

	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}
