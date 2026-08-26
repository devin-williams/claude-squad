package tmux

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Both CLIs keep their conversations on disk, outside tmux, keyed by the
// directory the agent ran in — so a session whose tmux server died can pick its
// conversation back up as long as it is revived in the same worktree.
//
// The layouts below were read off a live machine, not from docs, and are the
// kind of thing that changes without notice. Returning "" only costs a fresh
// conversation, so a layout change degrades to a plain start rather than
// breaking a revive.
type conversationStore struct {
	// resumeArgs returns the arguments that reopen the stored conversation for
	// workDir, or "" when there is nothing worth resuming.
	resumeArgs func(home, workDir string) string
}

var conversationStores = map[string]conversationStore{
	ProgramClaude: {resumeArgs: claudeResumeArgs},
	ProgramCursor: {resumeArgs: cursorResumeArgs},
}

// claudeResumeArgs: Claude Code writes one transcript per session under
// ~/.claude/projects/<slug of cwd>/, and only once the conversation has
// content, so the mere presence of a transcript means --continue has something
// to open.
func claudeResumeArgs(home, workDir string) string {
	entries, err := os.ReadDir(filepath.Join(home, ".claude", "projects", claudeProjectSlug(workDir)))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			return "--continue"
		}
	}
	return ""
}

// cursorResumeArgs names the chat explicitly rather than using --continue.
// cursor-agent creates a chat directory the moment it starts, so a session that
// was started fresh leaves an empty chat behind that is *newer* than the real
// one — and --continue takes the newest. Resuming that empty shell looks exactly
// like the resume having silently failed.
func cursorResumeArgs(home, workDir string) string {
	dir := filepath.Join(home, ".cursor", "chats", cursorChatDir(workDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var newestID string
	var newestAt int64 = -1
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var meta struct {
			HasConversation bool  `json:"hasConversation"`
			UpdatedAtMs     int64 `json:"updatedAtMs"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil || !meta.HasConversation {
			continue
		}
		if meta.UpdatedAtMs > newestAt {
			newestAt, newestID = meta.UpdatedAtMs, e.Name()
		}
	}
	if newestID == "" {
		return ""
	}
	return "--resume " + newestID
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

// resumeProgram returns program with its harness's resume arguments appended,
// when that harness has a conversation stored for workDir. An unknown harness,
// an absent conversation, or a program that already asks to resume is returned
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
	if err != nil {
		return program
	}
	args := store.resumeArgs(home, workDir)
	if args == "" {
		return program
	}
	return program + " " + args
}
