package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func typeRunes(wp *WorkspacePicker, s string) {
	for _, r := range s {
		wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func backspace(wp *WorkspacePicker) {
	wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyBackspace})
}

func testItems() []WorkspaceItem {
	return []WorkspaceItem{
		{Label: "pi-toolsmith", Path: "/Users/dev/repos/pi-toolsmith", Kind: KindRegistered, Existing: true},
		{Label: "remote-agents", Path: "/Users/dev/repos/remote-agents", Kind: KindRegistered, Existing: true},
		{Label: "argonath", Path: "/Users/dev/repos/argonath", Kind: KindRegistered, Existing: true},
	}
}

func TestFilterMatchesLabelCaseInsensitive(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	typeRunes(wp, "REMOTE")
	items := wp.visibleItems()
	require.Len(t, items, 1)
	require.Equal(t, "remote-agents", items[0].Label)
}

func TestFilterMatchesPathCaseInsensitive(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	typeRunes(wp, "REPOS/ARGO")
	items := wp.visibleItems()
	require.Len(t, items, 1)
	require.Equal(t, "argonath", items[0].Label)
}

func TestOrderPreservedOnEmptyFilter(t *testing.T) {
	src := testItems()
	wp := NewWorkspacePicker(src, nil)
	items := wp.visibleItems()
	require.Len(t, items, len(src))
	for i, it := range items {
		require.Equal(t, src[i].Label, it.Label)
	}
}

func TestCursorMovementClamping(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)

	// clamp at top
	require.True(t, wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyUp}))
	sel, ok := wp.Selected()
	require.True(t, ok)
	require.Equal(t, "pi-toolsmith", sel.Label)

	// move down to the bottom and past it
	require.True(t, wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown}))
	require.True(t, wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown}))
	require.True(t, wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown}))
	sel, ok = wp.Selected()
	require.True(t, ok)
	require.Equal(t, "argonath", sel.Label)
}

func TestCursorClampedAfterFilterNarrows(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	sel, ok := wp.Selected()
	require.True(t, ok)
	require.Equal(t, "argonath", sel.Label)

	typeRunes(wp, "pi")
	items := wp.visibleItems()
	require.Len(t, items, 1)
	sel, ok = wp.Selected()
	require.True(t, ok)
	require.Equal(t, "pi-toolsmith", sel.Label)
}

func TestSyntheticRowAppearsForPathLikeFilterWithNoMatches(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	typeRunes(wp, "~/.dotfiles")
	items := wp.visibleItems()
	require.Len(t, items, 1)
	require.False(t, items[0].Existing)
}

func TestSyntheticRowNotShownForPlainWordWithNoMatches(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	typeRunes(wp, "zzz")
	items := wp.visibleItems()
	require.Len(t, items, 0)
}

func TestSyntheticRowNotShownWhenRegisteredMatchExists(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	typeRunes(wp, "pi-tool")
	items := wp.visibleItems()
	require.Len(t, items, 1)
	require.True(t, items[0].Existing)
}

func TestClassifierInvokedForSyntheticRowOnly(t *testing.T) {
	calls := 0
	classifier := func(raw string) (ItemKind, string) {
		calls++
		return KindNonGit, "no .git — edits in place"
	}
	wp := NewWorkspacePicker(testItems(), classifier)

	wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("~/.dotfiles")})
	require.Equal(t, 1, calls)

	items := wp.visibleItems()
	require.Len(t, items, 1)
	require.Equal(t, KindNonGit, items[0].Kind)
	require.Equal(t, "no .git — edits in place", items[0].Note)

	for i := 0; i < 10; i++ {
		wp.Render()
	}
	require.Equal(t, 1, calls)
}

func TestClassifierNotInvokedWhenFilterUnchanged(t *testing.T) {
	calls := 0
	classifier := func(raw string) (ItemKind, string) {
		calls++
		return KindNonGit, ""
	}
	wp := NewWorkspacePicker(testItems(), classifier)
	wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("~/x")})
	require.Equal(t, 1, calls)

	// Cursor movement should not re-trigger classification.
	wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyUp})
	wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, 1, calls)
}

func TestZeroMatchRendersExplicitMessage(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	typeRunes(wp, "zzz")
	out := wp.Render()
	require.Contains(t, out, "No matching workspaces")
}

func TestRenderDoesNotPanicAtNarrowWidth(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	wp.SetWidth(20)
	require.NotPanics(t, func() {
		wp.Render()
	})
}

func TestRenderDoesNotPanicWithEmptyItems(t *testing.T) {
	wp := NewWorkspacePicker(nil, nil)
	require.NotPanics(t, func() {
		wp.Render()
	})
}

func TestRenderDoesNotPanicWithSyntheticRowAtNarrowWidth(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), func(raw string) (ItemKind, string) {
		return KindNonGit, "no .git — edits in place"
	})
	wp.SetWidth(20)
	typeRunes(wp, "~/.dotfiles")
	require.NotPanics(t, func() {
		wp.Render()
	})
}

func TestBackspaceEditsFilter(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	typeRunes(wp, "argo")
	backspace(wp)
	require.Equal(t, "arg", wp.Filter())
}

func TestEnterAndEscNotConsumed(t *testing.T) {
	wp := NewWorkspacePicker(testItems(), nil)
	require.False(t, wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEnter}))
	require.False(t, wp.HandleKeyPress(tea.KeyMsg{Type: tea.KeyEsc}))
}
