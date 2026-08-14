package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func focusedPicker(t *testing.T, branches ...string) *BranchPicker {
	t.Helper()
	bp := NewBranchPicker()
	bp.Focus()
	bp.SetResults(branches, bp.GetFilterVersion())
	return bp
}

// The two getters drive different git paths — checkout versus cut-a-new-branch —
// so a pick that answered both would be ambiguous at the call site.
func TestBranchPicker_ModesAreMutuallyExclusive(t *testing.T) {
	bp := focusedPicker(t, "develop", "feature/x")

	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown}) // off "New branch", onto develop
	require.Equal(t, "develop", bp.GetSelectedBranch())
	require.Empty(t, bp.GetBranchBase(), "checkout mode names no base")

	consumed, filterChanged := bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	require.True(t, consumed, "right must not fall through to the overlay")
	require.False(t, filterChanged)

	require.True(t, bp.IsBranchFrom())
	require.Equal(t, "develop", bp.GetBranchBase())
	require.Empty(t, bp.GetSelectedBranch(),
		"branch-from mode must not report a checkout target, or the session commits onto the base")

	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyLeft})
	require.False(t, bp.IsBranchFrom())
	require.Equal(t, "develop", bp.GetSelectedBranch())
	require.Empty(t, bp.GetBranchBase())
}

// Toggling drops the "New branch" pseudo-row, so a cursor index held across the
// switch would silently move the highlight to the neighbouring branch.
func TestBranchPicker_ToggleKeepsHighlightOnSameBranch(t *testing.T) {
	bp := focusedPicker(t, "develop", "feature/x", "release")

	// Row 0 is the "New branch" pseudo-row, so two steps reach the middle branch —
	// one with a neighbour either side, which an off-by-one would land on.
	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, "feature/x", bp.GetSelectedBranch())

	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, "feature/x", bp.GetBranchBase(), "the highlight must survive the row shift")

	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, "feature/x", bp.GetSelectedBranch())
}

// In branch-from mode the pseudo-row would mean "new branch from the configured
// base", which is what the mode already does with no base picked.
func TestBranchPicker_BranchFromHidesNewBranchRow(t *testing.T) {
	bp := focusedPicker(t, "develop")

	require.Contains(t, bp.Render(), newBranchOption)

	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	out := bp.Render()
	assert.NotContains(t, out, newBranchOption)
	assert.Contains(t, out, "New branch from", "the mode must be visible, not just felt")
	assert.Contains(t, out, "develop")
}

// With no branches to base off, the mode has nothing to name and must degrade to
// the configured base rather than reporting a bogus one.
func TestBranchPicker_BranchFromWithNoResults(t *testing.T) {
	bp := focusedPicker(t)

	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	assert.Empty(t, bp.GetBranchBase())
	assert.Empty(t, bp.GetSelectedBranch())
}

// The filter takes every printable key, so the mode toggle cannot be a rune.
func TestBranchPicker_ArrowsDoNotEnterTheFilter(t *testing.T) {
	bp := focusedPicker(t, "develop")

	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRight})
	bp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Empty(t, bp.GetFilter())
	assert.Zero(t, bp.GetFilterVersion(), "a mode change is not a new search")
}

// The overlay owns tab/enter; the picker must see the arrows while focused, and
// must not swallow them once focus has moved to the enter button.
func TestTextInputOverlay_BranchFromReachesTheOverlayGetter(t *testing.T) {
	o := NewTextInputOverlayWithBranchPicker("Enter prompt", "")
	o.SetSize(80, 20)
	o.SetBranchResults([]string{"develop"}, o.BranchFilterVersion())

	o.HandleKeyPress(tea.KeyMsg{Type: tea.KeyTab}) // focus the picker
	require.True(t, o.isBranchPicker())
	o.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	o.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRight})

	assert.Equal(t, "develop", o.GetBranchBase())
	assert.Empty(t, o.GetSelectedBranch())

	o.HandleKeyPress(tea.KeyMsg{Type: tea.KeyTab}) // enter button
	require.True(t, o.isEnterButton())
	o.HandleKeyPress(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, "develop", o.GetBranchBase(), "arrows must not toggle the mode from another focus stop")
}

// The label claimed HEAD while the code resolves the configured base, preferring
// origin's default branch — a promise the picker cannot keep.
func TestBranchPicker_NewBranchRowDoesNotClaimHEAD(t *testing.T) {
	assert.NotContains(t, strings.ToUpper(newBranchOption), "HEAD")
}
