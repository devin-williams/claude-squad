package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-squad/cmd/cmd_test"
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session/tmux"

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

// startedTestInstance boots an instance against a throwaway repo with a mocked
// tmux, i.e. the real session-create path, and returns it plus its repo path.
func startedTestInstance(t *testing.T, title string) (*Instance, string) {
	t.Helper()
	log.Initialize(false)
	t.Cleanup(log.Close)

	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())

	repoPath := initTestRepo(t)
	inst, err := NewInstance(InstanceOptions{Title: title, Path: repoPath, Program: "echo"})
	require.NoError(t, err)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "echo", "", &recordingPtyFactory{t: t}, newTmuxMockExec()))
	require.NoError(t, inst.Start(true))
	t.Cleanup(func() { _ = inst.Kill() })
	return inst, repoPath
}

// TestStart_RefusesFirstTimeSetupOnRunningInstance pins the destructive case.
// First-time setup overwrites i.gitWorktree with a fresh path and its failure
// path runs Kill, so on a live instance it tears down the running session and
// deletes its branch instead of creating anything. Nothing should call it that
// way, but a list-ordering bug did, and the damage was silent.
func TestStart_RefusesFirstTimeSetupOnRunningInstance(t *testing.T) {
	inst, repoPath := startedTestInstance(t, "already-running")

	wt, err := inst.GetGitWorktree()
	require.NoError(t, err)
	pathBefore := wt.GetWorktreePath()
	branchBefore := wt.GetBranchName()

	err = inst.Start(true)
	require.Error(t, err, "first-time setup on a running instance must be refused")
	assert.Contains(t, err.Error(), "already running")

	wtAfter, err := inst.GetGitWorktree()
	require.NoError(t, err)
	assert.Equal(t, pathBefore, wtAfter.GetWorktreePath(), "the live worktree must not be swapped out")
	_, statErr := os.Stat(pathBefore)
	assert.NoError(t, statErr, "the live worktree must still be on disk")

	out, err := exec.Command("git", "-C", repoPath, "show-ref", "--verify", "refs/heads/"+branchBefore).CombinedOutput()
	assert.NoError(t, err, "the live branch must not be deleted: %s", out)
}

// TestKill_SucceedsWhenTmuxSessionAlreadyGone covers the session whose tmux
// died underneath it. Reporting "failed to close tmux session" for a session
// that is already gone is what made such a row impossible to clear.
func TestKill_SucceedsWhenTmuxSessionAlreadyGone(t *testing.T) {
	inst, _ := startedTestInstance(t, "orphan")

	// has-session errors (gone) and kill-session errors (nothing to kill).
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps("orphan", "echo", "", &recordingPtyFactory{t: t},
		cmd_test.MockCmdExec{
			RunFunc:    func(*exec.Cmd) error { return fmt.Errorf("no such session") },
			OutputFunc: func(*exec.Cmd) ([]byte, error) { return nil, fmt.Errorf("no such session") },
		}))

	assert.NoError(t, inst.Kill(), "killing a session whose tmux is already gone must succeed")
}

// The converse: a live session that genuinely refuses to close must still error.
func TestKill_ReportsFailureWhenSessionIsAlive(t *testing.T) {
	inst, _ := startedTestInstance(t, "stubborn")

	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps("stubborn", "echo", "", &recordingPtyFactory{t: t},
		cmd_test.MockCmdExec{
			RunFunc: func(c *exec.Cmd) error {
				if strings.Contains(c.String(), "has-session") {
					return nil // alive
				}
				return fmt.Errorf("kill refused")
			},
			OutputFunc: func(*exec.Cmd) ([]byte, error) { return []byte(""), nil },
		}))

	err := inst.Kill()
	require.Error(t, err, "a live session that fails to close must still be reported")
	assert.Contains(t, err.Error(), "failed to close tmux session")
}

// deadSessionExec is a tmux double whose session is gone: has-session always
// fails, so both the existence check and any kill report "no such session".
func deadSessionExec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(c *exec.Cmd) error {
			if strings.Contains(c.String(), "has-session") {
				return fmt.Errorf("no such session")
			}
			return nil
		},
		OutputFunc: func(*exec.Cmd) ([]byte, error) { return []byte(""), nil },
	}
}

// A tmux server that dies (reboot, crash, external kill) leaves the worktree and
// branch intact. Reattaching to nothing used to "succeed", leaving the instance
// marked Running while every capture failed for the rest of the process's life —
// which is what filled the error box with "exit status 1".
func TestStart_ReviveRecreatesSessionWhenTmuxIsGone(t *testing.T) {
	inst, _ := startedTestInstance(t, "revive-me")

	wt, err := inst.GetGitWorktree()
	require.NoError(t, err)
	worktreePath := wt.GetWorktreePath()

	h := &reviveHarness{t: t}
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps("revive-me", "echo", "", h, h.exec()))

	require.NoError(t, inst.Start(false), "restoring a session whose tmux is gone must recover, not fail")

	var issued []string
	for _, c := range h.cmds {
		issued = append(issued, c.String())
	}
	// tmux.Start creates the session and then attaches to it, so the attach that
	// follows is expected. What matters is that a new-session came first rather
	// than an attach to a session that is not there.
	require.NotEmpty(t, issued)
	assert.Contains(t, issued[0], "new-session", "a missing session has to be recreated")

	assert.Equal(t, Running, inst.Status)
	_, statErr := os.Stat(worktreePath)
	assert.NoError(t, statErr, "reviving must not disturb the worktree")
}

// The destructive case. Start's failure path calls Kill, which removes the
// worktree and deletes its branch — correct rollback for a session this call
// half-created, catastrophic for a restore, where the worktree holds work that
// predates the process.
func TestStart_FailedReviveLeavesWorktreeAndBranchIntact(t *testing.T) {
	inst, repoPath := startedTestInstance(t, "revive-fails")

	wt, err := inst.GetGitWorktree()
	require.NoError(t, err)
	worktreePath := wt.GetWorktreePath()
	branch := wt.GetBranchName()

	canary := filepath.Join(worktreePath, "uncommitted.txt")
	require.NoError(t, os.WriteFile(canary, []byte("work in progress\n"), 0644))

	// has-session fails (gone) and starting a replacement fails too.
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps("revive-fails", "echo", "",
		&failingPtyFactory{}, deadSessionExec()))

	require.Error(t, inst.Start(false))

	_, statErr := os.Stat(worktreePath)
	assert.NoError(t, statErr, "a failed revive must not remove the worktree")
	assert.FileExists(t, canary, "a failed revive must not discard uncommitted work")

	out, err := exec.Command("git", "-C", repoPath, "show-ref", "--verify", "refs/heads/"+branch).CombinedOutput()
	assert.NoError(t, err, "a failed revive must not delete the branch: %s", out)
}

// reviveHarness models a tmux server whose session is gone: has-session fails
// until a new-session is spawned through the PTY and succeeds afterwards, which
// is what tmux.Start's own readiness poll observes.
type reviveHarness struct {
	t       *testing.T
	started bool
	cmds    []*exec.Cmd
}

func (h *reviveHarness) Start(c *exec.Cmd) (*os.File, error) {
	h.cmds = append(h.cmds, c)
	if strings.Contains(c.String(), "new-session") {
		h.started = true
	}
	return os.CreateTemp(h.t.TempDir(), "pty")
}

func (h *reviveHarness) Close() {}

func (h *reviveHarness) exec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(c *exec.Cmd) error {
			if strings.Contains(c.String(), "has-session") && !h.started {
				return fmt.Errorf("no such session")
			}
			return nil
		},
		OutputFunc: func(*exec.Cmd) ([]byte, error) { return []byte(""), nil },
	}
}

type failingPtyFactory struct{}

func (failingPtyFactory) Start(*exec.Cmd) (*os.File, error) {
	return nil, fmt.Errorf("pty unavailable")
}
func (failingPtyFactory) Close() {}

// LoadInstances turns any FromInstanceData error into a fatal exit, so one
// unrecoverable session would leave no way to launch the app and delete the row.
func TestFromInstanceData_UnrecoverableSessionBecomesPausedNotFatal(t *testing.T) {
	log.Initialize(false)
	t.Cleanup(log.Close)
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())

	// A worktree path that was never created: neither attaching nor recreating
	// the session can work.
	missing := filepath.Join(t.TempDir(), "worktree-is-gone")

	inst, err := FromInstanceData(InstanceData{
		Title:   "gone",
		Path:    t.TempDir(),
		Branch:  "some/branch",
		Status:  Running,
		Program: "echo",
		Worktree: GitWorktreeData{
			RepoPath:     filepath.Join(t.TempDir(), "repo-is-gone"),
			WorktreePath: missing,
			SessionName:  "gone",
			BranchName:   "some/branch",
		},
	})

	require.NoError(t, err, "one broken session must not stop every session from loading")
	require.NotNil(t, inst)
	assert.Equal(t, Paused, inst.Status, "paused is the honest state: started, no live tmux")
	assert.True(t, inst.Started(), "r must be able to retry the recreate")
}
