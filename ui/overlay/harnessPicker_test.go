package overlay

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"claude-squad/harness"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOptions() []harness.Option {
	return []harness.Option{
		{Label: "claude", Program: "claude"},
		{Label: "cursor-agent", Program: "cursor-agent"},
		{Label: "aider", Program: "aider"},
	}
}

func TestHarnessPicker_JAndKMoveCursor(t *testing.T) {
	hp := NewHarnessPicker(testOptions())
	hp.Focus()

	assert.True(t, hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}))
	sel, ok := hp.Selected()
	require.True(t, ok)
	assert.Equal(t, "cursor-agent", sel.Program)

	assert.True(t, hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}))
	sel, ok = hp.Selected()
	require.True(t, ok)
	assert.Equal(t, "claude", sel.Program)
}

func TestHarnessPicker_ArrowsMoveCursor(t *testing.T) {
	hp := NewHarnessPicker(testOptions())
	hp.Focus()

	assert.True(t, hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown}))
	sel, ok := hp.Selected()
	require.True(t, ok)
	assert.Equal(t, "cursor-agent", sel.Program)

	assert.True(t, hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyUp}))
	sel, ok = hp.Selected()
	require.True(t, ok)
	assert.Equal(t, "claude", sel.Program)
}

func TestHarnessPicker_EnterAndEscNotConsumed(t *testing.T) {
	hp := NewHarnessPicker(testOptions())
	hp.Focus()

	assert.False(t, hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEnter}))
	assert.False(t, hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEsc}))
}

func TestHarnessPicker_OtherRunesNotConsumed(t *testing.T) {
	hp := NewHarnessPicker(testOptions())
	hp.Focus()

	assert.False(t, hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}))
}

func TestHarnessPickerOverlay_SelectedMirrorsPickerAPI(t *testing.T) {
	overlay := NewHarnessPickerOverlay(testOptions())
	overlay.SetSize(80, 24)

	sel, ok := overlay.Selected()
	require.True(t, ok)
	assert.Equal(t, "claude", sel.Program)

	assert.True(t, overlay.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown}))
	sel, ok = overlay.Selected()
	require.True(t, ok)
	assert.Equal(t, "cursor-agent", sel.Program)
}

// The dot is colored per harness, so it must be styled against the row's base
// style rather than wrapped by it: a pre-rendered dot ends in an ANSI reset,
// which clears the selected row's background for everything after it.
func TestHarnessPicker_SelectedRowHasNoBackgroundHole(t *testing.T) {
	// lipgloss strips all styling when it sees no TTY, so without a forced
	// profile this test would assert against plain text and pass regardless.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	hp := NewHarnessPicker([]harness.Option{
		{Label: "claude", Program: "claude"},
		{Label: "cursor-agent", Program: "cursor-agent"},
	})
	hp.Focus()
	hp.SetWidth(40)

	row := strings.Split(hp.Render(), "\n")[0]
	labelAt := strings.Index(row, "claude")
	require.GreaterOrEqual(t, labelAt, 0)

	// The last escape sequence before the label must re-establish the row's
	// background. A pre-rendered dot leaves a bare reset there instead, so the
	// highlight stops at the dot and the label sits in a hole.
	seqs := regexp.MustCompile(`\x1b\[[0-9;]*m`).FindAllStringIndex(row[:labelAt], -1)
	require.NotEmpty(t, seqs, "the label should be preceded by styling")
	lastSeq := row[seqs[len(seqs)-1][0]:seqs[len(seqs)-1][1]]
	assert.NotEqual(t, "\x1b[0m", lastSeq, "the label is preceded by a reset: background hole")
	assert.Contains(t, lastSeq, "48;", "the sequence before the label must set a background")
}

// Nothing bounds how many profiles a workspace declares, so the list windows
// rather than rendering past the bottom of the terminal.
func TestHarnessPicker_WindowsLongOptionLists(t *testing.T) {
	var opts []harness.Option
	for i := 0; i < 20; i++ {
		opts = append(opts, harness.Option{Label: fmt.Sprintf("agent-%d", i), Program: "claude"})
	}
	hp := NewHarnessPicker(opts)
	hp.Focus()
	hp.SetWidth(40)
	hp.SetMaxVisible(5)

	lines := strings.Split(hp.Render(), "\n")
	assert.LessOrEqual(t, len(lines), 7, "5 rows plus at most two overflow markers")

	// The cursor stays visible after moving past the initial window.
	for i := 0; i < 12; i++ {
		hp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	sel, ok := hp.Selected()
	require.True(t, ok)
	assert.Contains(t, hp.Render(), sel.Label, "the selected option must stay within the window")
}
