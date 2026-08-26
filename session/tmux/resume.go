package tmux

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// Both CLIs keep their conversations on disk, outside tmux, keyed by the
// directory the agent ran in — so a session whose tmux server died can pick its
// conversation back up as long as it is revived in the same worktree.
//
// The layouts below were read off a live machine, not from docs, and are the
// kind of thing that changes without notice: hasConversation returning false
// only costs a fresh conversation, so a layout change degrades to today's
// behaviour rather than breaking a revive.
type conversationStore struct {
	// resumeArg continues the most recent conversation for the working directory.
	resumeArg string
	// hasConversation reports whether a stored conversation exists for workDir.
	hasConversation func(home, workDir string) bool
}

var conversationStores = map[string]conversationStore{
	ProgramClaude: {
		resumeArg: "--continue",
		hasConversation: func(home, workDir string) bool {
			dir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(workDir))
			entries, err := os.ReadDir(dir)
			if err != nil {
				return false
			}
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".jsonl") {
					return true
				}
			}
			return false
		},
	},
	ProgramCursor: {
		resumeArg: "--continue",
		hasConversation: func(home, workDir string) bool {
			entries, err := os.ReadDir(filepath.Join(home, ".cursor", "chats", cursorChatDir(workDir)))
			return err == nil && len(entries) > 0
		},
	},
}

// cursorChatDir is the directory cursor-agent stores a working directory's chats
// under: the md5 of the absolute path. Verified against a live ~/.cursor/chats.
func cursorChatDir(workDir string) string {
	sum := md5.Sum([]byte(workDir))
	return hex.EncodeToString(sum[:])
}

// claudeProjectSlug mirrors how Claude Code names a project directory: every
// character that is not alphanumeric becomes a dash, one for one. Collapsing
// runs would be wrong — "/.claude-squad" contributes two dashes for the slash
// and the dot.
func claudeProjectSlug(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// resumeProgram returns program with its harness's continue flag appended, when
// that harness has a stored conversation for workDir. An unknown harness, an
// absent conversation, or a program that already asks to resume is returned
// unchanged.
func resumeProgram(program, workDir string) string {
	store, ok := conversationStores[HarnessName(program)]
	if !ok {
		return program
	}
	for _, existing := range strings.Fields(program) {
		if existing == "--continue" || existing == "--resume" || existing == "-c" {
			return program
		}
	}

	home, err := os.UserHomeDir()
	if err != nil || !store.hasConversation(home, workDir) {
		return program
	}
	return program + " " + store.resumeArg
}
