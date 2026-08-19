package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"claude-squad/config"
	"claude-squad/harness"
	"claude-squad/session"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
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

// widthOfEveryLine returns the lipgloss display width of every line in s.
func widthOfEveryLine(s string) []int {
	lines := strings.Split(s, "\n")
	widths := make([]int, len(lines))
	for i, line := range lines {
		widths[i] = lipgloss.Width(line)
	}
	return widths
}

func newSingleInstanceList(t *testing.T, program string) *List {
	t.Helper()
	s := spinner.New()
	l := NewList(&s, false)
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   "sess",
		Path:    ".",
		Program: program,
	})
	require.NoError(t, err)
	inst.Branch = "feature/some-branch"
	l.AddInstance(inst)
	return l
}

// TestRender_BadgeDoesNotWidenRow verifies the harness badge is subtracted
// from the row's available width rather than appended on top of it — every
// rendered line must be exactly as wide with a badge as without one.
func TestRender_BadgeDoesNotWidenRow(t *testing.T) {
	withBadge := newSingleInstanceList(t, "claude")
	withBadge.SetSize(80, 20)

	withoutBadge := newSingleInstanceList(t, "")
	withoutBadge.SetSize(80, 20)

	require.Equal(t, widthOfEveryLine(withoutBadge.String()), widthOfEveryLine(withBadge.String()))
}

// TestRender_BadgeDoesNotWidenRow_CJKBranch is the same assertion with a
// 2-cell-per-rune branch name, to catch a len() vs runewidth.StringWidth
// slip in the badge-width accounting.
func TestRender_BadgeDoesNotWidenRow_CJKBranch(t *testing.T) {
	withBadge := newSingleInstanceList(t, "claude")
	withBadge.items[0].Branch = "功能/新分支名称测试"
	withBadge.SetSize(80, 20)

	withoutBadge := newSingleInstanceList(t, "")
	withoutBadge.items[0].Branch = "功能/新分支名称测试"
	withoutBadge.SetSize(80, 20)

	require.Equal(t, widthOfEveryLine(withoutBadge.String()), widthOfEveryLine(withBadge.String()))
}

// TestRender_NarrowWidthDropsBadgeNotBranch verifies that at a width too
// narrow to fit both, the badge is sacrificed and the branch — the row's
// identity — survives. 16 is inside the drop threshold (the badge reappears at
// 18); the badge is only a few cells now that it is the glyph alone.
func TestRender_NarrowWidthDropsBadgeNotBranch(t *testing.T) {
	l := newSingleInstanceList(t, "claude")
	l.SetSize(16, 20)

	rendered := l.String()
	// Assert on the glyph: asserting on the harness name would pass whether or
	// not the badge rendered, since the row no longer prints the name.
	require.NotContains(t, rendered, harness.Badge("claude"), "badge must be dropped at narrow widths")
	require.Contains(t, rendered, branchIcon, "branch must survive even when the badge is dropped")
}

// The badge must never push the branch line past the pane at any width. Below
// 14 the branch line already overflows without a badge at all (a pre-existing
// upstream issue at absurd widths), so the sweep starts there.
func TestRender_BadgeNeverOverflowsAtAnyWidth(t *testing.T) {
	for w := 14; w <= 60; w++ {
		l := newSingleInstanceList(t, "claude")
		l.SetSize(w, 20)
		for _, line := range strings.Split(l.String(), "\n") {
			if strings.Contains(line, branchIcon) {
				require.LessOrEqual(t, lipgloss.Width(line), w,
					"branch line overflows at width %d", w)
			}
		}
	}
}

// The workspace keys step through this list, so it has to be the order the tab
// strip renders — registry order — and not the order sessions happened to be
// created in. The two differ here, which is the normal case.
func TestWorkspaceTabIDs_FollowsRegistryOrderNotFirstAppearance(t *testing.T) {
	seedRegistry(t, "ws-a", "ws-b", "ws-c")

	s := spinner.New()
	l := NewList(&s, false)
	for _, ws := range []string{"ws-b", "ws-c", "ws-b", "ws-a"} {
		l.AddInstance(newWorkspacedInstance(t, ws+"-s", ws))
	}

	require.Equal(t, []string{"ws-a", "ws-b", "ws-c"}, l.WorkspaceTabIDs())
	require.Equal(t, l.WorkspaceTabIDs(), tabStripIDs(l),
		"the cycle order and the rendered strip must be the same list")
}

// An orphan (its workspace deleted out from under live sessions) has no registry
// entry to order it by, and still needs a reachable stop.
func TestWorkspaceTabIDs_OrphansFollowRegisteredTabs(t *testing.T) {
	seedRegistry(t, "ws-a")

	s := spinner.New()
	l := NewList(&s, false)
	l.AddInstance(newWorkspacedInstance(t, "orphan-s", "ws-gone"))
	l.AddInstance(newWorkspacedInstance(t, "a-s", "ws-a"))

	require.Equal(t, []string{"ws-a", "ws-gone"}, l.WorkspaceTabIDs())
	require.Equal(t, l.WorkspaceTabIDs(), tabStripIDs(l))
}

// The active workspace keeps a tab while empty, so cycling must be able to land
// on it — otherwise the strip shows a stop the keys skip over.
func TestWorkspaceTabIDs_IncludesEmptyActiveWorkspace(t *testing.T) {
	seedRegistry(t, "ws-a", "ws-b")

	s := spinner.New()
	l := NewList(&s, false)
	l.AddInstance(newWorkspacedInstance(t, "a-s", "ws-a"))
	l.SetActiveWorkspaceID("ws-b")

	require.Equal(t, []string{"ws-a", "ws-b"}, l.WorkspaceTabIDs())
}

func TestWorkspaceTabIDsEmpty(t *testing.T) {
	s := spinner.New()
	require.Empty(t, NewList(&s, false).WorkspaceTabIDs())
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
	require.Same(t, a1, l.GetSelectedInstance(),
		"collapsing must park the selection on the group so the same key can unfold it")

	order := renderedOrder(l)
	require.Equal(t, []*session.Instance{b1}, order)

	l.SelectInstance(b1)
	l.Down()
	require.Same(t, a1, l.GetSelectedInstance(),
		"Down must stop once on the collapsed ws-a group, not skip past it")
	l.Down()
	require.Same(t, b1, l.GetSelectedInstance(), "Down from the collapsed group skips its hidden members")

	l.SelectInstance(a2)
	l.Down()
	require.Same(t, b1, l.GetSelectedInstance(), "a hidden member is not itself a stop")

	l.SelectInstance(b1)
	l.ToggleCollapseCurrent() // b1 is in ws-b; this toggles ws-b, collapsing everything
	require.Empty(t, renderedOrder(l))

	// With every group collapsed the headers are the only stops left, and both
	// must remain reachable — otherwise a fully-folded list is a dead end.
	l.SelectInstance(a1)
	l.Down()
	require.Same(t, b1, l.GetSelectedInstance())
	l.Up()
	require.Same(t, a1, l.GetSelectedInstance())
	l.ToggleCollapseCurrent()
	require.Equal(t, []*session.Instance{a1, a2}, renderedOrder(l), "z must unfold the group it folded")

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

// A collapsed group renders no item rows, so if its header is not highlighted the
// cursor is parked somewhere with nothing on screen to show it.
func TestRenderBody_CollapsedGroupHeaderCarriesTheCursor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	seedRegistry(t, "ws-a", "ws-b")

	s := spinner.New()
	l := NewList(&s, false)
	l.SetSize(80, 40)

	l.AddInstance(newWorkspacedInstance(t, "a1", "ws-a"))
	l.AddInstance(newWorkspacedInstance(t, "b1", "ws-b"))

	lines, anchor := l.renderBody()
	require.GreaterOrEqual(t, anchor, 0)
	require.NotContains(t, lines[anchor], "ws-a", "with ws-a expanded the anchor is an item row, not the header")

	l.ToggleCollapseCurrent()

	lines, anchor = l.renderBody()
	require.GreaterOrEqual(t, anchor, 0, "the collapsed group's header must be the scroll anchor")
	require.Contains(t, lines[anchor], "ws-a")

	cells, backgrounds := cellBackgrounds(lines[anchor])
	for i, c := range cells {
		if c == '▸' {
			require.NotEmpty(t, backgrounds[i],
				"the header the cursor sits on must render with the selected background")
			return
		}
	}
	t.Fatal("collapsed marker not found in the anchored header line")
}

// seedRegistry points $CLAUDE_SQUAD_HOME at a fresh tmp dir and writes a
// registry holding the given workspace ids (display name == id).
func seedRegistry(t *testing.T, ids ...string) {
	t.Helper()
	dir := t.TempDir()
	prev := os.Getenv(config.ConfigHomeEnvVar)
	require.NoError(t, os.Setenv(config.ConfigHomeEnvVar, dir))
	t.Cleanup(func() {
		if prev == "" {
			_ = os.Unsetenv(config.ConfigHomeEnvVar)
		} else {
			_ = os.Setenv(config.ConfigHomeEnvVar, prev)
		}
	})

	reg := &config.WorkspaceRegistry{}
	for _, id := range ids {
		require.NoError(t, reg.Upsert(config.Workspace{ID: id, DisplayName: id, RepoPath: filepath.Join(dir, id)}))
	}
	require.NoError(t, reg.Save())
}

// The registry accumulates every repo ever used, so a tab per registered
// workspace clutters the strip with destinations that can only show an empty
// list.
func TestWorkspaceTabs_OmitWorkspacesWithoutSessions(t *testing.T) {
	seedRegistry(t, "ws-a", "ws-b", "ws-c")

	s := spinner.New()
	l := NewList(&s, false)
	l.AddInstance(newWorkspacedInstance(t, "a1", "ws-a"))
	l.AddInstance(newWorkspacedInstance(t, "c1", "ws-c"))

	require.Equal(t, []string{"ws-a", "ws-c"}, l.WorkspaceTabIDs(), "ws-b holds no sessions and must not get a tab")
	require.Contains(t, l.renderCountSummary(l.workspaceTabDescriptors(l.groupedItems())), "2 workspaces")
}

// The active workspace is where new sessions land, so it stays visible even
// while empty — otherwise the V toggle has no tab to highlight.
func TestWorkspaceTabs_KeepEmptyActiveWorkspace(t *testing.T) {
	seedRegistry(t, "ws-a", "ws-b")

	s := spinner.New()
	l := NewList(&s, false)
	l.AddInstance(newWorkspacedInstance(t, "a1", "ws-a"))
	l.SetActiveWorkspaceID("ws-b")

	require.Equal(t, []string{"ws-a", "ws-b"}, l.WorkspaceTabIDs())
}

// A tab is still owed to instances whose workspace was removed from the
// registry, or they become unreachable by filter.
func TestWorkspaceTabs_KeepOrphanWorkspaces(t *testing.T) {
	seedRegistry(t, "ws-a")

	s := spinner.New()
	l := NewList(&s, false)
	l.AddInstance(newWorkspacedInstance(t, "a1", "ws-a"))
	l.AddInstance(newWorkspacedInstance(t, "gone", "ws-deleted"))

	require.Equal(t, []string{"ws-a", "ws-deleted"}, l.WorkspaceTabIDs())
}

// Filtering must not hide the other tabs, or there is no way to filter back.
func TestWorkspaceTabs_SurviveViewFilter(t *testing.T) {
	seedRegistry(t, "ws-a", "ws-b")

	s := spinner.New()
	l := NewList(&s, false)
	l.AddInstance(newWorkspacedInstance(t, "a1", "ws-a"))
	l.AddInstance(newWorkspacedInstance(t, "b1", "ws-b"))
	l.SetViewFilter("ws-a")

	require.Equal(t, []string{"ws-a", "ws-b"}, l.WorkspaceTabIDs())
}

// cellBackgrounds returns the active background SGR for each visible cell in a
// rendered line, so a test can assert two adjacent cells are painted the same.
func cellBackgrounds(line string) (cells []rune, backgrounds []string) {
	seq := regexp.MustCompile(`^\x1b\[([0-9;]*)m`)
	bg := ""
	for i := 0; i < len(line); {
		if m := seq.FindStringSubmatch(line[i:]); m != nil {
			params := m[1]
			switch {
			case params == "" || params == "0":
				bg = ""
			case strings.Contains(params, "48;"):
				bg = params[strings.Index(params, "48;"):]
			}
			i += len(m[0])
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		cells = append(cells, r)
		backgrounds = append(backgrounds, bg)
		i += size
	}
	return cells, backgrounds
}

// The badge's padding must carry the row background. Rendered as bare spaces
// between pre-rendered segments it lands after their ANSI reset, which on a
// selected row draws black gaps either side of the glyph.
func TestRender_SelectedBadgeHasNoUnstyledPadding(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	l := newSingleInstanceList(t, "claude")
	l.SetSize(80, 20)
	l.SetSelectedInstance(0)

	glyph := []rune(harness.Badge("claude"))[0]
	var line string
	for _, ln := range strings.Split(l.String(), "\n") {
		if strings.ContainsRune(ln, glyph) {
			line = ln
			break
		}
	}
	require.NotEmpty(t, line, "the badge should be rendered on the selected row")

	cells, backgrounds := cellBackgrounds(line)
	idx := -1
	for i, r := range cells {
		if r == glyph {
			idx = i
			break
		}
	}
	require.Greater(t, idx, 0, "glyph must have a cell before it")
	require.Less(t, idx, len(cells)-1, "glyph must have a cell after it")

	require.NotEmpty(t, backgrounds[idx], "the glyph itself must sit on the row background")
	assert.Equal(t, backgrounds[idx], backgrounds[idx-1],
		"the padding before the badge is unstyled: a black gap on a selected row")
	assert.Equal(t, backgrounds[idx], backgrounds[idx+1],
		"the padding after the badge is unstyled: a black gap on a selected row")
}

// tabStripIDs recovers the tab order from the rendered strip by matching each
// cell's label back to a descriptor, so a test can compare the cycle order
// against what is actually on screen rather than against the same call again.
func tabStripIDs(l *List) []string {
	tabs := l.workspaceTabDescriptors(l.groupedItems())
	byLabel := make(map[string]string, len(tabs))
	for _, t := range tabs {
		byLabel[t.label] = t.id
	}

	// Positional rather than whitespace-split: an orphan's label is
	// "(unknown workspace)", which no field-splitting scheme can match back.
	strip := stripANSI(l.renderWorkspaceTabs(tabs))
	type placed struct {
		at int
		id string
	}
	var found []placed
	for label, id := range byLabel {
		at := strings.Index(strip, label)
		if at < 0 {
			continue
		}
		found = append(found, placed{at, id})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].at < found[j].at })

	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.id)
	}
	return out
}
