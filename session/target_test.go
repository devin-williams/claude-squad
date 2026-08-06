package session

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, exec.Command("git", "-C", dir, "init", "-q").Run())
	require.NoError(t, exec.Command("git", "-C", dir, "commit", "-q", "--allow-empty", "-m", "init").Run())
}

func canon(t *testing.T, path string) string {
	t.Helper()
	c, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return c
}

func TestResolveSessionTarget(t *testing.T) {
	repoRoot := t.TempDir()
	initGitRepo(t, repoRoot)
	canonicalRoot := canon(t, repoRoot)

	subDir := filepath.Join(repoRoot, "pkg", "inner")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	nonGitDir := t.TempDir()

	launchDir := t.TempDir()

	t.Run("repo root", func(t *testing.T) {
		target, err := ResolveSessionTarget(repoRoot, launchDir)
		require.NoError(t, err)
		assert.Equal(t, ModeWorktree, target.Mode)
		assert.Equal(t, canonicalRoot, target.Dir)
		assert.Empty(t, target.SubDir)
	})

	t.Run("subdirectory inside repo", func(t *testing.T) {
		target, err := ResolveSessionTarget(subDir, launchDir)
		require.NoError(t, err)
		assert.Equal(t, ModeWorktree, target.Mode)
		assert.Equal(t, canonicalRoot, target.Dir)
		assert.Equal(t, filepath.Join("pkg", "inner"), target.SubDir)
	})

	t.Run("non-git directory", func(t *testing.T) {
		target, err := ResolveSessionTarget(nonGitDir, launchDir)
		require.NoError(t, err)
		assert.Equal(t, ModeInPlace, target.Mode)
		assert.Equal(t, canon(t, nonGitDir), target.Dir)
		assert.Empty(t, target.SubDir)
	})

	t.Run("missing path", func(t *testing.T) {
		missing := filepath.Join(launchDir, "does-not-exist")
		_, err := ResolveSessionTarget(missing, launchDir)
		require.Error(t, err)
		assert.True(t, errors.Is(err, os.ErrNotExist))
	})

	t.Run("file rather than directory", func(t *testing.T) {
		file := filepath.Join(launchDir, "a-file.txt")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0644))
		_, err := ResolveSessionTarget(file, launchDir)
		require.Error(t, err)
	})

	t.Run("tilde expansion", func(t *testing.T) {
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		tmpHomeSub, err := os.MkdirTemp(home, "cs-target-test-")
		require.NoError(t, err)
		defer os.RemoveAll(tmpHomeSub)

		rel := "~/" + filepath.Base(tmpHomeSub)
		target, err := ResolveSessionTarget(rel, launchDir)
		require.NoError(t, err)
		assert.Equal(t, ModeInPlace, target.Mode)
		assert.Equal(t, canon(t, tmpHomeSub), target.Dir)
	})

	t.Run("relative path resolved against launchDir", func(t *testing.T) {
		target, err := ResolveSessionTarget(filepath.Base(nonGitDir), filepath.Dir(nonGitDir))
		require.NoError(t, err)
		assert.Equal(t, ModeInPlace, target.Mode)
		assert.Equal(t, canon(t, nonGitDir), target.Dir)
	})

	t.Run("symlinked path", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks not reliably available on windows CI")
		}
		linkPath := filepath.Join(launchDir, "repo-link")
		require.NoError(t, os.Symlink(repoRoot, linkPath))
		target, err := ResolveSessionTarget(linkPath, launchDir)
		require.NoError(t, err)
		assert.Equal(t, ModeWorktree, target.Mode)
		assert.Equal(t, canonicalRoot, target.Dir)
	})
}

func TestClassifyPath(t *testing.T) {
	repoRoot := t.TempDir()
	initGitRepo(t, repoRoot)

	subDir := filepath.Join(repoRoot, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	nonGitDir := t.TempDir()
	launchDir := t.TempDir()

	kind, label := ClassifyPath(repoRoot, launchDir)
	assert.Equal(t, KindRepoRoot, kind)
	assert.Equal(t, "git repo", label)

	kind, label = ClassifyPath(subDir, launchDir)
	assert.Equal(t, KindInsideRepo, kind)
	assert.Contains(t, label, "worktree")

	kind, label = ClassifyPath(nonGitDir, launchDir)
	assert.Equal(t, KindNonGit, kind)
	assert.Equal(t, "no .git — edits in place", label)

	kind, label = ClassifyPath(filepath.Join(launchDir, "missing"), launchDir)
	assert.Equal(t, KindMissing, kind)
	assert.Equal(t, "does not exist", label)

	file := filepath.Join(launchDir, "a-file.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0644))
	kind, label = ClassifyPath(file, launchDir)
	assert.Equal(t, KindNotDir, kind)
	assert.Equal(t, "not a directory", label)
}
