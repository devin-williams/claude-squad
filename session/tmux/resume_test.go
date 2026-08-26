package tmux

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The slug is one dash per non-alphanumeric character. Collapsing runs is the
// tempting mistake: "/.claude-squad" owes two dashes to the slash and the dot,
// and a collapsed slug points at a directory that does not exist.
func TestClaudeProjectSlug_OneDashPerCharacter(t *testing.T) {
	// Verbatim from a real ~/.claude/projects listing.
	assert.Equal(t,
		"-Users-devinwilliams--claude-squad-workspaces-e879902e88d5-worktrees-devinwilliams-add-logging-18ca8ae07726f460",
		claudeProjectSlug("/Users/devinwilliams/.claude-squad/workspaces/e879902e88d5/worktrees/devinwilliams/add-logging_18ca8ae07726f460"))
}

func TestResumeProgram_AppendsContinueOnlyWhenAConversationExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "worktrees", "some-session")

	assert.Equal(t, "claude", resumeProgram("claude", workDir),
		"with no stored conversation there is nothing to continue")

	dir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(workDir))
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "abc.jsonl"), []byte("{}\n"), 0644))

	assert.Equal(t, "claude --continue", resumeProgram("claude", workDir))
	assert.Equal(t, "/Users/x/.local/bin/claude --continue",
		resumeProgram("/Users/x/.local/bin/claude", workDir),
		"the harness is identified by basename, since stored programs are absolute paths")
}

// An empty project directory is the shape left behind by a session that never
// produced a transcript; --continue would fail there.
func TestResumeProgram_IgnoresEmptyProjectDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "wt")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "projects", claudeProjectSlug(workDir)), 0755))
	assert.Equal(t, "claude", resumeProgram("claude", workDir))
}

func TestResumeProgram_CursorKeysChatsByMD5OfWorkDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "wt")

	assert.Equal(t, "cursor-agent", resumeProgram("cursor-agent", workDir))

	// 5c1e... is md5(workDir); computed the same way the store does, since the
	// point of the test is the lookup, not the hash function.
	chat := filepath.Join(home, ".cursor", "chats", cursorChatDir(workDir), "chat-id")
	require.NoError(t, os.MkdirAll(chat, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(chat, "meta.json"), []byte("{}"), 0644))

	assert.Equal(t, "cursor-agent --continue", resumeProgram("cursor-agent", workDir))
}

func TestResumeProgram_LeavesUnknownHarnessesAndExplicitFlagsAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "wt")

	dir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(workDir))
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("{}\n"), 0644))

	assert.Equal(t, "claude --continue", resumeProgram("claude --continue", workDir),
		"a program that already continues must not be given a second flag")
	assert.Equal(t, "claude --resume", resumeProgram("claude --resume", workDir))
	assert.Equal(t, "some-other-cli", resumeProgram("some-other-cli", workDir),
		"an unknown harness has no conversation store to consult")
}
