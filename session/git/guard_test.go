package git

import (
	"claude-squad/log"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}

func withTempHome(t *testing.T) string {
	t.Helper()
	tempHome := t.TempDir()
	original := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", original)
	})
	return tempHome
}

func TestGuardDestructive(t *testing.T) {
	tempHome := withTempHome(t)

	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		t.Fatalf("getWorktreeDirectory: %v", err)
	}
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatalf("mkdir worktrees dir: %v", err)
	}

	legitChild := filepath.Join(worktreesDir, "feature-test")
	if err := os.MkdirAll(legitChild, 0755); err != nil {
		t.Fatalf("mkdir legit child: %v", err)
	}

	siblingLookalike := worktreesDir + "x"
	siblingChild := filepath.Join(siblingLookalike, "foo")
	if err := os.MkdirAll(siblingChild, 0755); err != nil {
		t.Fatalf("mkdir sibling lookalike: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"empty string", "", true},
		{"root", "/", true},
		{"home itself", tempHome, true},
		{"dotfiles under home", filepath.Join(tempHome, ".dotfiles"), true},
		{"worktrees dir itself", worktreesDir, true},
		{"legitimate child", legitChild, false},
		{"lookalike sibling naive-prefix trap", siblingChild, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := guardDestructive(tt.path)
			if tt.wantErr && err == nil {
				t.Fatalf("guardDestructive(%q) = nil, want error", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("guardDestructive(%q) = %v, want nil", tt.path, err)
			}
		})
	}
}

func TestGuardDestructive_ProtectsRealDirectory(t *testing.T) {
	withTempHome(t)

	sentinelDir := t.TempDir()
	sentinelFile := filepath.Join(sentinelDir, "sentinel.txt")
	if err := os.WriteFile(sentinelFile, []byte("do not delete\n"), 0644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	g := &GitWorktree{
		repoPath:     sentinelDir,
		worktreePath: sentinelDir,
	}

	if err := g.Remove(); err == nil {
		t.Fatal("Remove() = nil, want error for a worktree path outside any managed root")
	}

	if _, err := os.Stat(sentinelFile); err != nil {
		t.Fatalf("sentinel file should still exist, stat error: %v", err)
	}
}
