package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"claude-squad/cmd/cmd_test"

	"github.com/stretchr/testify/require"
)

func TestHarnessFor(t *testing.T) {
	tests := []struct {
		name    string
		program string
		want    string // expected harness name, "" for nil
	}{
		{"bare claude", "claude", ProgramClaude},
		{"absolute path claude", "/Users/x/.local/bin/claude", ProgramClaude},
		{"aider with args", "aider --model gpt-4", ProgramAider},
		{"absolute path aider with args", "/usr/local/bin/aider --model gpt-4", ProgramAider},
		{"bare gemini", "gemini", ProgramGemini},
		{"unknown program", "bash", ""},
		{"empty program", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := harnessFor(tt.program)
			if tt.want == "" {
				require.Nil(t, h)
				return
			}
			require.NotNil(t, h)
			require.Equal(t, tt.want, h.name)
		})
	}
}

// TestHarnessForOldMatchingBug pins the exact bug that harnessFor fixes: a
// naive `program == ProgramClaude` comparison fails for the common case of an
// absolute path or a program string carrying arguments.
func TestHarnessForOldMatchingBug(t *testing.T) {
	program := "/Users/x/.local/bin/claude"
	require.NotEqual(t, ProgramClaude, program, "sanity check: path must not equal bare name")
	require.NotNil(t, harnessFor(program))
}

func newHasUpdatedTestSession(t *testing.T, program string, paneContent string) *TmuxSession {
	t.Helper()
	cmdExec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte(paneContent), nil
		},
	}
	session := newTmuxSession("test-session", program, "", NewMockPtyFactory(t), cmdExec)
	session.monitor = newStatusMonitor()
	return session
}

func TestHasUpdatedAbsolutePathClaudePrompt(t *testing.T) {
	session := newHasUpdatedTestSession(t, "/Users/x/.local/bin/claude",
		"some output\nNo, and tell Claude what to do differently\n")
	_, hasPrompt := session.HasUpdated()
	require.True(t, hasPrompt)
}

func TestHasUpdatedAbsolutePathClaudeNoPrompt(t *testing.T) {
	session := newHasUpdatedTestSession(t, "/Users/x/.local/bin/claude", "some regular output\n")
	_, hasPrompt := session.HasUpdated()
	require.False(t, hasPrompt)
}

func newTrustPromptTestSession(t *testing.T, program string, paneContent string) (*TmuxSession, *os.File) {
	t.Helper()
	cmdExec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte(paneContent), nil
		},
	}
	session := newTmuxSession("test-session", program, "", NewMockPtyFactory(t), cmdExec)

	f, err := os.CreateTemp(t.TempDir(), fmt.Sprintf("ptmx-%s", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	session.ptmx = f
	return session, f
}

func readAllFromStart(t *testing.T, f *os.File) []byte {
	t.Helper()
	_, err := f.Seek(0, 0)
	require.NoError(t, err)
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	return buf[:n]
}

func TestCheckAndHandleTrustPromptClaudeTapsEnter(t *testing.T) {
	session, ptmx := newTrustPromptTestSession(t, "/Users/x/.local/bin/claude",
		"Do you trust the files in this folder?\n")
	handled := session.CheckAndHandleTrustPrompt()
	require.True(t, handled)
	require.Equal(t, []byte{0x0D}, readAllFromStart(t, ptmx))
}

func TestCheckAndHandleTrustPromptAiderTapsDAndEnter(t *testing.T) {
	session, ptmx := newTrustPromptTestSession(t, "aider --model gpt-4",
		"Open documentation url for more info\n")
	handled := session.CheckAndHandleTrustPrompt()
	require.True(t, handled)
	require.Equal(t, []byte{0x44, 0x0D}, readAllFromStart(t, ptmx))
}

func TestCheckAndHandleTrustPromptUnknownProgramReturnsFalse(t *testing.T) {
	session, ptmx := newTrustPromptTestSession(t, "bash",
		"Open documentation url for more info\n")
	handled := session.CheckAndHandleTrustPrompt()
	require.False(t, handled)
	require.Empty(t, readAllFromStart(t, ptmx))
}
