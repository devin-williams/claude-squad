package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-squad/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInPlaceInstance_StorageRoundTrip verifies that an in-place instance
// survives ToInstanceData -> FromInstanceData with gitWorktree still nil.
// If NewGitWorktreeFromStorage were ever called unconditionally here, it
// would hand back a non-nil struct built from empty fields, silently
// re-arming every nil-based safety check after a restart.
func TestInPlaceInstance_StorageRoundTrip(t *testing.T) {
	log.Initialize(false)
	t.Cleanup(log.Close)

	original := &Instance{
		Title:     "in-place-test",
		Path:      t.TempDir(),
		Status:    Paused,
		InPlace:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	data := original.ToInstanceData()
	assert.True(t, data.InPlace)
	assert.Equal(t, GitWorktreeData{}, data.Worktree)

	restored, err := FromInstanceData(data)
	require.NoError(t, err)

	assert.True(t, restored.InPlace)
	assert.Nil(t, restored.gitWorktree)
}

// TestInPlaceInstance_SafetyGuards is the headline acceptance test: an
// in-place instance's safety comes entirely from gitWorktree staying nil, so
// Kill, Pause, and Resume must never touch the real directory it points at.
func TestInPlaceInstance_SafetyGuards(t *testing.T) {
	log.Initialize(false)
	t.Cleanup(log.Close)

	sentinelDir := t.TempDir()
	sentinelFile := filepath.Join(sentinelDir, "sentinel.txt")
	require.NoError(t, os.WriteFile(sentinelFile, []byte("do not delete\n"), 0644))

	inst := &Instance{
		Title:     "in-place-safety",
		Path:      sentinelDir,
		Status:    Running,
		InPlace:   true,
		started:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	require.NoError(t, inst.Kill())
	requireSentinelExists(t, sentinelFile)

	require.Error(t, inst.Pause())
	requireSentinelExists(t, sentinelFile)

	inst.Status = Paused
	require.Error(t, inst.Resume())
	requireSentinelExists(t, sentinelFile)
}

func requireSentinelExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.NoError(t, err, "sentinel file should still exist")
}

// TestInPlaceInstance_RepoName verifies RepoName falls back to the directory's
// base name instead of dereferencing the nil gitWorktree.
func TestInPlaceInstance_RepoName(t *testing.T) {
	dir := t.TempDir()
	inst := &Instance{
		Title:   "in-place-reponame",
		Path:    dir,
		InPlace: true,
		started: true,
	}

	name, err := inst.RepoName()
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(dir), name)
}

// TestInPlaceInstance_DiffNoOps verifies the diff-stat paths are no-ops for an
// in-place instance rather than dereferencing the nil gitWorktree — UpdateDiffStats
// in particular runs from the daemon, so an unguarded nil there is a headless crash.
func TestInPlaceInstance_DiffNoOps(t *testing.T) {
	inst := &Instance{
		Title:   "in-place-diff",
		Path:    t.TempDir(),
		InPlace: true,
		started: true,
		Status:  Running,
	}

	require.NoError(t, inst.UpdateDiffStats())
	assert.Nil(t, inst.GetDiffStats())
	assert.Nil(t, inst.ComputeDiff())
	assert.Nil(t, inst.ComputeDiffNumstat())
}

// TestInPlaceInstance_GetGitWorktreeErrors verifies GetGitWorktree returns an
// explicit error for an in-place instance instead of the (nil, nil) that would
// let a caller like PushChanges nil-deref.
func TestInPlaceInstance_GetGitWorktreeErrors(t *testing.T) {
	inst := &Instance{
		Title:   "in-place-worktree",
		Path:    t.TempDir(),
		InPlace: true,
		started: true,
	}

	wt, err := inst.GetGitWorktree()
	require.Error(t, err)
	assert.Nil(t, wt)
}
