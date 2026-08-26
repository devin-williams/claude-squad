package tmux

import (
	cmd2 "claude-squad/cmd"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-squad/cmd/cmd_test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockPtyFactory struct {
	t *testing.T

	// Array of commands and the corresponding file handles representing PTYs.
	cmds  []*exec.Cmd
	files []*os.File
}

func (pt *MockPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	filePath := filepath.Join(pt.t.TempDir(), fmt.Sprintf("pty-%s-%d", pt.t.Name(), rand.Int31()))
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err == nil {
		pt.cmds = append(pt.cmds, cmd)
		pt.files = append(pt.files, f)
	}
	return f, err
}

func (pt *MockPtyFactory) Close() {}

func NewMockPtyFactory(t *testing.T) *MockPtyFactory {
	return &MockPtyFactory{
		t: t,
	}
}

func TestSanitizeName(t *testing.T) {
	session := NewTmuxSession("asdf", "program", "")
	require.Equal(t, TmuxPrefix+"asdf", session.sanitizedName)

	session = NewTmuxSession("a sd f . . asdf", "program", "")
	require.Equal(t, TmuxPrefix+"asdf__asdf", session.sanitizedName)

	// With workspace id, name is prefixed with the (truncated) workspace id.
	session = NewTmuxSession("asdf", "program", "abcdef0123456789")
	require.Equal(t, TmuxPrefix+"abcdef01_asdf", session.sanitizedName)
}

func TestStartTmuxSession(t *testing.T) {
	ptyFactory := NewMockPtyFactory(t)

	created := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") && !created {
				created = true
				return fmt.Errorf("session already exists")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	workdir := t.TempDir()
	session := newTmuxSession("test-session", "claude", "", ptyFactory, cmdExec)

	err := session.Start(workdir, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(ptyFactory.cmds))
	require.Equal(t, fmt.Sprintf("tmux new-session -d -s claudesquad_test-session -c %s claude", workdir),
		cmd2.ToString(ptyFactory.cmds[0]))
	require.Equal(t, "tmux attach-session -t claudesquad_test-session",
		cmd2.ToString(ptyFactory.cmds[1]))

	require.Equal(t, 2, len(ptyFactory.files))

	// File should be closed.
	_, err = ptyFactory.files[0].Stat()
	require.Error(t, err)
	// File should be open
	_, err = ptyFactory.files[1].Stat()
	require.NoError(t, err)
}

// attach-session runs inside the PTY, so its failure never reaches
// ptyFactory.Start — that only reports whether the process could be spawned.
// Restore therefore has to check for itself, or a session that is gone restores
// "successfully" and every later capture fails instead.
func TestRestore_FailsWhenSessionIsGone(t *testing.T) {
	ptyFactory := NewMockPtyFactory(t)
	session := newTmuxSession("gone", "claude", "", ptyFactory, cmd_test.MockCmdExec{
		RunFunc:    func(*exec.Cmd) error { return fmt.Errorf("no such session") },
		OutputFunc: func(*exec.Cmd) ([]byte, error) { return []byte(""), nil },
	})

	err := session.Restore()
	require.Error(t, err)
	assert.Contains(t, err.Error(), session.sanitizedName, "the error must name the session")
	assert.Empty(t, ptyFactory.cmds, "no PTY should be spawned for a session that is not there")
}

// "error capturing pane content: exit status 1" says nothing about which session
// failed or why, which is undiagnosable across a list of sessions.
func TestCapturePaneContent_ErrorNamesSessionAndQuotesStderr(t *testing.T) {
	session := newTmuxSession("noisy", "claude", "", NewMockPtyFactory(t), cmd_test.MockCmdExec{
		RunFunc: func(*exec.Cmd) error { return nil },
		OutputFunc: func(*exec.Cmd) ([]byte, error) {
			// Shape of what exec.Cmd.Output returns for a failed tmux call.
			return nil, &exec.ExitError{
				ProcessState: &os.ProcessState{},
				Stderr:       []byte("can't find pane: claudesquad_noisy\n"),
			}
		},
	})

	_, err := session.CapturePaneContent()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claudesquad_noisy")
	assert.Contains(t, err.Error(), "can't find pane", "tmux's own stderr is the diagnosis")
}
