package overlay

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Not "from HEAD": a new branch is cut from the configured new-session base
// (workspace BaseBranch, else config, else origin's default branch), which is
// usually not the current checkout.
const newBranchOption = "New branch (from default base)"

// BranchPicker is an embeddable component for selecting a branch.
// It does not hold the full branch list — results are provided asynchronously
// via SetResults after each debounced search.
type BranchPicker struct {
	results       []string // current search results (from git)
	filter        string   // current filter text
	filterVersion uint64   // incremented on each filter change
	cursor        int      // index into visibleItems()
	focused       bool
	width         int
	showNewBranch bool // whether to show the "New branch" option
	// branchFrom switches what picking a branch means: check it out and commit
	// onto it (false), or cut a new session branch based on it (true).
	branchFrom bool
}

// NewBranchPicker creates a new empty branch picker.
func NewBranchPicker() *BranchPicker {
	return &BranchPicker{
		showNewBranch: true,
	}
}

// SetWidth sets the width of the branch picker.
func (bp *BranchPicker) SetWidth(w int) {
	bp.width = w
}

// Focus gives the branch picker focus.
func (bp *BranchPicker) Focus() {
	bp.focused = true
}

// Blur removes focus from the branch picker.
func (bp *BranchPicker) Blur() {
	bp.focused = false
}

// IsFocused returns whether the branch picker is focused.
func (bp *BranchPicker) IsFocused() bool {
	return bp.focused
}

// GetFilter returns the current filter text.
func (bp *BranchPicker) GetFilter() string {
	return bp.filter
}

// GetFilterVersion returns a monotonically increasing version that changes on every filter edit.
func (bp *BranchPicker) GetFilterVersion() uint64 {
	return bp.filterVersion
}

// HandleKeyPress processes a key event. Returns (consumed, filterChanged).
func (bp *BranchPicker) HandleKeyPress(msg tea.KeyMsg) (consumed bool, filterChanged bool) {
	switch msg.Type {
	case tea.KeyUp:
		if bp.cursor > 0 {
			bp.cursor--
		}
		return true, false
	case tea.KeyDown:
		items := bp.visibleItems()
		if bp.cursor < len(items)-1 {
			bp.cursor++
		}
		return true, false
	case tea.KeyRight, tea.KeyLeft:
		// Arrows rather than a rune or a ctrl chord: every printable key is filter
		// text here, and ctrl+b is tmux's prefix on most setups.
		bp.setBranchFrom(msg.Type == tea.KeyRight)
		return true, false
	case tea.KeyBackspace:
		if len(bp.filter) > 0 {
			runes := []rune(bp.filter)
			bp.filter = string(runes[:len(runes)-1])
			bp.filterVersion++
			return true, true
		}
		return true, false
	case tea.KeyRunes:
		bp.filter += string(msg.Runes)
		bp.filterVersion++
		return true, true
	case tea.KeySpace:
		bp.filter += " "
		bp.filterVersion++
		return true, true
	}
	return false, false
}

// SetResults updates the branch list with search results.
// version must match filterVersion for the results to be accepted (prevents stale updates).
func (bp *BranchPicker) SetResults(branches []string, version uint64) {
	if version != bp.filterVersion {
		return // stale results
	}
	bp.results = branches

	// Hide "New branch" when filter exactly matches a branch name
	bp.showNewBranch = true
	if bp.filter != "" {
		lower := strings.ToLower(bp.filter)
		for _, b := range branches {
			if strings.ToLower(b) == lower {
				bp.showNewBranch = false
				break
			}
		}
	}

	// Clamp cursor
	items := bp.visibleItems()
	if bp.cursor >= len(items) {
		if len(items) > 0 {
			bp.cursor = len(items) - 1
		} else {
			bp.cursor = 0
		}
	}
}

// visibleItems returns the list of items to display.
func (bp *BranchPicker) visibleItems() []string {
	var items []string
	// The pseudo-row is "new branch from the configured base", so in branch-from
	// mode it would mean the same thing as the mode itself with no base picked.
	if bp.showNewBranch && !bp.branchFrom {
		items = append(items, newBranchOption)
	}
	items = append(items, bp.results...)
	return items
}

// setBranchFrom switches mode, keeping the highlight on the same branch. Toggling
// adds or removes the pseudo-row, so holding the cursor index would silently
// shift the highlight by one.
func (bp *BranchPicker) setBranchFrom(on bool) {
	if bp.branchFrom == on {
		return
	}
	prev := bp.selectedRow()
	bp.branchFrom = on
	bp.cursor = 0
	for i, item := range bp.visibleItems() {
		if item == prev {
			bp.cursor = i
			break
		}
	}
}

// IsBranchFrom reports whether the picked branch is the base for a new session
// branch rather than the branch to work on directly.
func (bp *BranchPicker) IsBranchFrom() bool {
	return bp.branchFrom
}

func (bp *BranchPicker) selectedRow() string {
	items := bp.visibleItems()
	if bp.cursor < 0 || bp.cursor >= len(items) {
		return ""
	}
	return items[bp.cursor]
}

// GetSelectedBranch returns the branch to check out and commit onto, or empty
// string when a new branch should be created instead — which covers both the
// "New branch" row and branch-from mode, where the pick is a base and not a
// checkout target.
func (bp *BranchPicker) GetSelectedBranch() string {
	if bp.branchFrom {
		return ""
	}
	if row := bp.selectedRow(); row != newBranchOption {
		return row
	}
	return ""
}

// GetBranchBase returns the branch a new session branch should be cut from, or
// empty string to use the configured new-session base.
func (bp *BranchPicker) GetBranchBase() string {
	if !bp.branchFrom {
		return ""
	}
	if row := bp.selectedRow(); row != newBranchOption {
		return row
	}
	return ""
}

var (
	bpLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true)

	bpFilterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))

	bpSelectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("0"))

	bpDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// Render renders the branch picker.
func (bp *BranchPicker) Render() string {
	var s strings.Builder
	label := "Branch"
	if bp.branchFrom {
		label = "New branch from"
	}
	s.WriteString(bpLabelStyle.Render(label))
	if bp.focused {
		cursor := bp.filter + "█"
		s.WriteString(bpFilterStyle.Render(" (filter: " + cursor + ")"))
	} else if bp.filter != "" {
		s.WriteString(bpDimStyle.Render(" (filter: " + bp.filter + ")"))
	}
	if bp.focused {
		hint := " → branch off it"
		if bp.branchFrom {
			hint = " ← work on it directly"
		}
		s.WriteString(bpDimStyle.Render(hint))
	}
	s.WriteString("\n\n")

	items := bp.visibleItems()
	if len(items) == 0 {
		s.WriteString(bpDimStyle.Render("  No matching branches"))
		return s.String()
	}

	// Show max 5 visible items, windowed around cursor
	maxVisible := 5
	start := 0
	if bp.cursor >= maxVisible {
		start = bp.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		prefix := "  "
		label := items[i]
		if i == bp.cursor && bp.focused {
			prefix = "> "
			s.WriteString(bpSelectedStyle.Render(prefix + label))
		} else if i == bp.cursor {
			s.WriteString(prefix + label)
		} else {
			s.WriteString(bpDimStyle.Render(prefix + label))
		}
		if i < end-1 {
			s.WriteString("\n")
		}
	}

	return s.String()
}
