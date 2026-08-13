package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three focus stops and the isX() predicates were interlocked with a
// numStops of 4 while the overlay carried a profile picker. Removing it means
// every offset shifted, and an off-by-one is invisible: tab still appears to
// work, it just lands somewhere unexpected.
func TestPromptOverlayFocusCycle(t *testing.T) {
	o := NewTextInputOverlayWithBranchPicker("Enter prompt", "")
	o.SetSize(80, 20)

	require.True(t, o.isTextarea(), "focus must start on the textarea")
	require.False(t, o.isBranchPicker())
	require.False(t, o.isEnterButton())

	tab := tea.KeyMsg{Type: tea.KeyTab}

	o.HandleKeyPress(tab)
	assert.True(t, o.isBranchPicker(), "tab from the textarea must reach the branch picker")
	assert.False(t, o.isTextarea())
	assert.False(t, o.isEnterButton())

	o.HandleKeyPress(tab)
	assert.True(t, o.isEnterButton(), "tab from the branch picker must reach the enter button")
	assert.False(t, o.isBranchPicker())

	o.HandleKeyPress(tab)
	assert.True(t, o.isTextarea(), "tab must wrap back to the textarea after three stops")

	// shift+tab walks the same ring backwards.
	o.HandleKeyPress(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.True(t, o.isEnterButton(), "shift+tab from the textarea must wrap to the enter button")
}

func TestPromptOverlayRenderHasNoProfileSection(t *testing.T) {
	o := NewTextInputOverlayWithBranchPicker("Enter prompt", "")
	o.SetSize(80, 20)

	out := o.Render()
	assert.NotContains(t, strings.ToLower(out), "profile",
		"the program picker moved to its own step before naming; it must not linger here")
}
