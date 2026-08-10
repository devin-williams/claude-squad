package ui

import (
	"claude-squad/session"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/require"
)

func newTestList(titles ...string) *List {
	s := spinner.New()
	l := NewList(&s, false)
	for _, t := range titles {
		inst, _ := session.NewInstance(session.InstanceOptions{
			Title:   t,
			Path:    ".",
			Program: "echo",
		})
		l.AddInstance(inst)
	}
	return l
}

func TestMoveUp(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveUp()
	require.True(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "b", l.items[0].Title)
	require.Equal(t, "a", l.items[1].Title)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveUp_AtTop(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(0)

	moved := l.MoveUp()
	require.False(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
}

func TestMoveDown(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveDown()
	require.True(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, "b", l.items[2].Title)
}

func TestMoveDown_AtBottom(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(2)

	moved := l.MoveDown()
	require.False(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveWithSingleItem(t *testing.T) {
	l := newTestList("only")
	l.SetSelectedInstance(0)

	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
}

// TestRender_InPlaceInstanceShowsHomeShortenedPath verifies that an in-place
// instance renders its home-shortened path with a distinct icon instead of a
// bare branch icon, since it has no branch to show.
func TestRender_InPlaceInstanceShowsHomeShortenedPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	inPlaceDir := filepath.Join(homeDir, "dotfiles")
	require.NoError(t, os.MkdirAll(inPlaceDir, 0o755))

	s := spinner.New()
	l := NewList(&s, false)
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   "dots",
		Path:    inPlaceDir,
		Program: "echo",
		InPlace: true,
	})
	require.NoError(t, err)
	l.AddInstance(inst)
	l.SetSize(80, 20)

	rendered := l.String()
	require.Contains(t, rendered, inPlaceIcon)
	require.Contains(t, rendered, "~/dotfiles")
	require.NotContains(t, rendered, branchIcon)
}

func TestWorkspaceIDs(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	for _, ws := range []string{"ws-b", "ws-a", "ws-b", "ws-c", "ws-a"} {
		inst, err := session.NewInstance(session.InstanceOptions{Title: ws + "-s", Path: ".", Program: "echo"})
		require.NoError(t, err)
		inst.WorkspaceID = ws
		l.AddInstance(inst)
	}

	// First-appearance order, duplicates collapsed — stable and matches what
	// the user sees rather than an alphabetical reshuffle.
	require.Equal(t, []string{"ws-b", "ws-a", "ws-c"}, l.WorkspaceIDs())
}

func TestWorkspaceIDsEmpty(t *testing.T) {
	s := spinner.New()
	require.Empty(t, NewList(&s, false).WorkspaceIDs())
}
