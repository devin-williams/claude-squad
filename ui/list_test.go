package ui

import (
	"claude-squad/session"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// newWorkspacedInstance builds a titled instance assigned to the given workspace.
func newWorkspacedInstance(t *testing.T, title, workspaceID string) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{Title: title, Path: ".", Program: "echo"})
	require.NoError(t, err)
	inst.WorkspaceID = workspaceID
	return inst
}

// renderedOrder returns the instances in l.items in the order their titles
// actually appear in renderBody's output, i.e. the true on-screen order.
// Items hidden by a collapsed group or the view filter are omitted.
func renderedOrder(l *List) []*session.Instance {
	lines, _ := l.renderBody()
	type entry struct {
		line int
		inst *session.Instance
	}
	var entries []entry
	for _, inst := range l.items {
		for i, line := range lines {
			if strings.Contains(line, inst.Title) {
				entries = append(entries, entry{i, inst})
				break
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].line < entries[j].line })
	out := make([]*session.Instance, len(entries))
	for i, e := range entries {
		out[i] = e.inst
	}
	return out
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

// TestReorderItems_InterleavedWorkspacesGroup verifies that instances added
// interleaved across workspaces end up grouped in l.items, in first-appearance
// group order, with insertion order preserved inside each group.
func TestReorderItems_InterleavedWorkspacesGroup(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)

	a1 := newWorkspacedInstance(t, "a1", "ws-a")
	b1 := newWorkspacedInstance(t, "b1", "ws-b")
	a2 := newWorkspacedInstance(t, "a2", "ws-a")
	b2 := newWorkspacedInstance(t, "b2", "ws-b")

	l.AddInstance(a1)
	l.AddInstance(b1)
	l.AddInstance(a2)
	l.AddInstance(b2)

	require.Equal(t, []*session.Instance{a1, a2, b1, b2}, l.items)
}

// TestDown_MatchesRenderedOrder verifies that repeatedly calling Down() from
// the top visits every instance in exactly the order renderBody renders them,
// not creation order.
func TestDown_MatchesRenderedOrder(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	l.SetSize(80, 40)

	titles := []struct{ title, ws string }{
		{"a1", "ws-a"}, {"b1", "ws-b"}, {"a2", "ws-a"}, {"b2", "ws-b"}, {"a3", "ws-a"},
	}
	for _, tt := range titles {
		l.AddInstance(newWorkspacedInstance(t, tt.title, tt.ws))
	}

	expected := renderedOrder(l)
	require.Len(t, expected, len(titles))

	l.SetSelectedInstance(0)
	// The list's index 0 after reordering may not be renderedOrder[0]'s
	// index; find it and start selection there.
	l.SelectInstance(expected[0])

	var visited []*session.Instance
	visited = append(visited, l.GetSelectedInstance())
	for i := 1; i < len(expected); i++ {
		l.Down()
		visited = append(visited, l.GetSelectedInstance())
	}

	require.Equal(t, expected, visited)
}

// TestUp_FromTopWrapsToLastRenderedRow verifies Up() from the first rendered
// row wraps around to the last rendered row.
func TestUp_FromTopWrapsToLastRenderedRow(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	l.SetSize(80, 40)

	for _, tt := range []struct{ title, ws string }{
		{"a1", "ws-a"}, {"b1", "ws-b"}, {"a2", "ws-a"}, {"b2", "ws-b"},
	} {
		l.AddInstance(newWorkspacedInstance(t, tt.title, tt.ws))
	}

	expected := renderedOrder(l)
	l.SelectInstance(expected[0])
	l.Up()
	require.Same(t, expected[len(expected)-1], l.GetSelectedInstance())
}

// TestReorderItems_PreservesSelection verifies the selected instance survives
// a reorder triggered by adding an instance to an earlier group, by pointer
// identity rather than by index.
func TestReorderItems_PreservesSelection(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)

	b1 := newWorkspacedInstance(t, "b1", "ws-b")
	a1 := newWorkspacedInstance(t, "a1", "ws-a")
	l.AddInstance(b1)
	l.AddInstance(a1)

	// ws-b appeared first, so items are [b1, a1]; select a1.
	l.SelectInstance(a1)
	require.Same(t, a1, l.GetSelectedInstance())

	// Adding another ws-b instance grows the earlier group and shifts a1's index.
	b2 := newWorkspacedInstance(t, "b2", "ws-b")
	l.AddInstance(b2)

	require.Equal(t, []*session.Instance{b1, b2, a1}, l.items)
	require.Same(t, a1, l.GetSelectedInstance())
}

// TestReorderItems_Idempotent verifies sorting an already-sorted list is a no-op.
func TestReorderItems_Idempotent(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)

	for _, tt := range []struct{ title, ws string }{
		{"a1", "ws-a"}, {"b1", "ws-b"}, {"a2", "ws-a"}, {"c1", ""}, {"b2", "ws-b"},
	} {
		l.AddInstance(newWorkspacedInstance(t, tt.title, tt.ws))
	}

	before := append([]*session.Instance{}, l.items...)
	l.reorderItems()
	require.Equal(t, before, l.items)
}

// TestMoveUpDown_GroupBoundary verifies MoveUp/MoveDown refuse to cross a
// workspace group boundary, but still swap freely within a group.
func TestMoveUpDown_GroupBoundary(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)

	a1 := newWorkspacedInstance(t, "a1", "ws-a")
	a2 := newWorkspacedInstance(t, "a2", "ws-a")
	b1 := newWorkspacedInstance(t, "b1", "ws-b")
	b2 := newWorkspacedInstance(t, "b2", "ws-b")
	l.AddInstance(a1)
	l.AddInstance(a2)
	l.AddInstance(b1)
	l.AddInstance(b2)
	// items: [a1, a2, b1, b2]

	l.SelectInstance(b1)
	before := append([]*session.Instance{}, l.items...)
	require.False(t, l.MoveUp(), "MoveUp across ws-a/ws-b boundary must refuse")
	require.Equal(t, before, l.items)
	require.Same(t, b1, l.GetSelectedInstance())

	l.SelectInstance(a2)
	before = append([]*session.Instance{}, l.items...)
	require.False(t, l.MoveDown(), "MoveDown across ws-a/ws-b boundary must refuse")
	require.Equal(t, before, l.items)
	require.Same(t, a2, l.GetSelectedInstance())

	l.SelectInstance(a2)
	require.True(t, l.MoveUp(), "MoveUp within ws-a must succeed")
	require.Equal(t, []*session.Instance{a2, a1, b1, b2}, l.items)

	l.SelectInstance(b1)
	require.True(t, l.MoveDown(), "MoveDown within ws-b must succeed")
	require.Equal(t, []*session.Instance{a2, a1, b2, b1}, l.items)
}

// TestRenderBody_SkipsCollapsedAndFiltered verifies collapsed groups and an
// active view filter are skipped in the rendered order, and that Down() skips
// those same hidden rows.
func TestRenderBody_SkipsCollapsedAndFiltered(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	l.SetSize(80, 40)

	a1 := newWorkspacedInstance(t, "a1", "ws-a")
	a2 := newWorkspacedInstance(t, "a2", "ws-a")
	b1 := newWorkspacedInstance(t, "b1", "ws-b")
	l.AddInstance(a1)
	l.AddInstance(a2)
	l.AddInstance(b1)

	l.ToggleCollapseCurrent() // collapses ws-a, since a1 is selected by default
	require.Same(t, b1, l.GetSelectedInstance(), "collapsing selected group should move selection off it")

	order := renderedOrder(l)
	require.Equal(t, []*session.Instance{b1}, order)

	l.SelectInstance(b1)
	l.Down()
	require.Same(t, b1, l.GetSelectedInstance(), "Down should skip the collapsed ws-a group and wrap back to b1")

	l.ToggleCollapseCurrent() // b1 is in ws-b; this toggles ws-b, collapsing everything
	require.Empty(t, renderedOrder(l))

	// Un-collapse everything and instead exercise the view filter.
	l.collapsedWorkspaces = map[string]bool{}
	l.SetViewFilter("ws-a")
	order = renderedOrder(l)
	require.Equal(t, []*session.Instance{a1, a2}, order)

	l.SelectInstance(a1)
	l.Down()
	require.Same(t, a2, l.GetSelectedInstance())
	l.Down()
	require.Same(t, a1, l.GetSelectedInstance(), "Down should wrap within the filtered ws-a group, skipping b1")
}
