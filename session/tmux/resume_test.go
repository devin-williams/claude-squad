package tmux

import (
	"fmt"
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

// writeCursorChat creates one chat under workDir's chat directory.
func writeCursorChat(t *testing.T, home, workDir, id string, hasConversation bool, updatedAtMs int64) {
	t.Helper()
	dir := filepath.Join(home, ".cursor", "chats", cursorChatDir(workDir), id)
	require.NoError(t, os.MkdirAll(dir, 0755))
	meta := fmt.Sprintf(`{"schemaVersion":1,"hasConversation":%t,"updatedAtMs":%d,"cwd":%q}`,
		hasConversation, updatedAtMs, workDir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0644))
}

func TestResumeProgram_CursorNamesTheChatExplicitly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "wt")

	assert.Equal(t, "cursor-agent", resumeProgram("cursor-agent", workDir))

	writeCursorChat(t, home, workDir, "real-chat", true, 1_000)
	assert.Equal(t, "cursor-agent --resume real-chat", resumeProgram("cursor-agent", workDir))
}

// cursor-agent creates a chat directory the moment it starts, so a session
// started fresh leaves an empty chat that is NEWER than the real one. --continue
// takes the newest, which resumed that empty shell and looked exactly like the
// resume silently failing.
func TestResumeProgram_CursorSkipsTheEmptyChatAFreshStartLeaves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "wt")

	writeCursorChat(t, home, workDir, "real-chat", true, 1_000)
	writeCursorChat(t, home, workDir, "empty-shell", false, 9_999)

	assert.Equal(t, "cursor-agent --resume real-chat", resumeProgram("cursor-agent", workDir),
		"the newest chat is an empty shell; the newest chat with content is the one to reopen")
}

// Only empty shells means there is nothing to resume, and --resume with no id
// would open an interactive picker in a detached pane.
func TestResumeProgram_CursorWithOnlyEmptyChats(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "wt")

	writeCursorChat(t, home, workDir, "empty-shell", false, 9_999)
	assert.Equal(t, "cursor-agent", resumeProgram("cursor-agent", workDir))
}

// A blanked program would leave tmux running the default shell with no agent.
func TestResumeProgram_NeverReturnsAnEmptyProgram(t *testing.T) {
	t.Setenv("HOME", "")
	assert.NotEmpty(t, resumeProgram("claude", "/tmp/wt"))
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

func TestResumeProgram_CursorIgnoresChatIDsThatAreNotPlainIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "wt")

	writeCursorChat(t, home, workDir, "x; touch pwned", true, 9_000)
	writeCursorChat(t, home, workDir, "real-chat", true, 1_000)
	assert.Equal(t, "cursor-agent --resume real-chat", resumeProgram("cursor-agent", workDir))
}
