package tmux

import (
	"path/filepath"
	"strings"
)

// harness describes one agent CLI's prompt-detection strings and how to
// dismiss its first-run trust screen. Adding a new agent CLI is a matter of
// adding an entry to harnesses, not editing matching logic in multiple places.
type harness struct {
	name string
	// confirmPrompts: any match in the pane content means the agent is
	// waiting on a yes/no confirmation.
	confirmPrompts []string
	// trustPrompts: any match in the pane content means the first-run
	// trust/permission screen is showing.
	trustPrompts []string
	// dismissTrust sends whatever keystroke(s) dismiss the trust screen.
	dismissTrust func(*TmuxSession) error
}

var harnesses = []*harness{
	{
		name:           ProgramClaude,
		confirmPrompts: []string{"No, and tell Claude what to do differently"},
		trustPrompts:   []string{"Do you trust the files in this folder?", "new MCP server"},
		dismissTrust:   (*TmuxSession).TapEnter,
	},
	{
		name:           ProgramAider,
		confirmPrompts: []string{"(Y)es/(N)o/(D)on't ask again"},
		trustPrompts:   []string{"Open documentation url for more info"},
		dismissTrust:   (*TmuxSession).TapDAndEnter,
	},
	{
		name:           ProgramGemini,
		confirmPrompts: []string{"Yes, allow once"},
		trustPrompts:   []string{"Open documentation url for more info"},
		dismissTrust:   (*TmuxSession).TapDAndEnter,
	},
	{
		// Strings transcribed from a live cursor-agent pane, not from its docs.
		// Only non-allowlisted shell commands prompt; file edits apply without
		// asking, so there is no edit-approval string to match.
		name:           ProgramCursor,
		confirmPrompts: []string{"Run this command?", "Waiting for approval"},
		trustPrompts:   []string{"Workspace Trust Required", "Do you trust the contents of this directory?"},
		dismissTrust:   (*TmuxSession).TapEnter,
	},
}

// HarnessName normalizes a stored program string to the token that
// identifies its agent CLI: the first whitespace-separated field (dropping
// any arguments), then the path basename (dropping any directory). Stored
// program strings are frequently absolute paths and/or carry arguments, e.g.
// "/Users/x/.local/bin/claude" or "aider --model gpt-4".
func HarnessName(program string) string {
	fields := strings.Fields(program)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

// harnessFor returns the registry entry matching program, or nil if the
// program is not a known agent CLI.
func harnessFor(program string) *harness {
	name := HarnessName(program)
	for _, h := range harnesses {
		if h.name == name {
			return h
		}
	}
	return nil
}

// KnownHarness reports whether program identifies a registered agent CLI.
func KnownHarness(program string) bool {
	return harnessFor(program) != nil
}

// KnownHarnessNames returns the registry's names in declaration order.
func KnownHarnessNames() []string {
	names := make([]string, len(harnesses))
	for i, h := range harnesses {
		names[i] = h.name
	}
	return names
}

func containsAny(content string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(content, needle) {
			return true
		}
	}
	return false
}
