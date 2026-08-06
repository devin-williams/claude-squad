package overlay

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// ItemKind classifies a WorkspaceItem for display and dispatch.
type ItemKind int

const (
	KindRegistered ItemKind = iota
	KindRepoRoot
	KindInsideRepo
	KindNonGit
	KindMissing
	KindNotDir
)

// WorkspaceItem is one row in the workspace picker.
type WorkspaceItem struct {
	Label    string
	Path     string
	Kind     ItemKind
	Note     string
	Existing bool
}

// PathClassifier classifies an arbitrary typed path for the synthetic row.
// The picker has no way to inspect the filesystem or git state itself (that
// would create an import cycle with session), so callers inject this.
type PathClassifier func(raw string) (kind ItemKind, note string)

// WorkspacePicker is an embeddable type-to-filter workspace picker. Unlike
// BranchPicker it filters synchronously against an in-memory slice, so it
// carries no async/staleness machinery.
type WorkspacePicker struct {
	items      []WorkspaceItem
	classifier PathClassifier

	filter  string
	cursor  int
	focused bool
	width   int

	synthetic       *WorkspaceItem
	syntheticFilter string
}

// NewWorkspacePicker creates a picker over the given registered workspaces,
// in the order the caller provides (callers sort by last-used so an empty
// filter + Enter picks the most recent). classifier may be nil, in which case
// the synthetic path row is shown with no kind/note annotation.
func NewWorkspacePicker(items []WorkspaceItem, classifier PathClassifier) *WorkspacePicker {
	return &WorkspacePicker{
		items:      items,
		classifier: classifier,
	}
}

func (wp *WorkspacePicker) SetWidth(w int) {
	wp.width = w
}

func (wp *WorkspacePicker) Focus() {
	wp.focused = true
}

func (wp *WorkspacePicker) Blur() {
	wp.focused = false
}

func (wp *WorkspacePicker) IsFocused() bool {
	return wp.focused
}

// Filter returns the current filter text.
func (wp *WorkspacePicker) Filter() string {
	return wp.filter
}

func isPathLikeFilter(s string) bool {
	return strings.Contains(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, ".")
}

func (wp *WorkspacePicker) filteredRegistered() []WorkspaceItem {
	if wp.filter == "" {
		return wp.items
	}
	lower := strings.ToLower(wp.filter)
	var out []WorkspaceItem
	for _, it := range wp.items {
		if strings.Contains(strings.ToLower(it.Label), lower) || strings.Contains(strings.ToLower(it.Path), lower) {
			out = append(out, it)
		}
	}
	return out
}

// updateSynthetic recomputes the synthetic path row, invoking the classifier
// at most once per distinct filter value.
func (wp *WorkspacePicker) updateSynthetic() {
	if wp.syntheticFilter == wp.filter {
		return
	}
	wp.syntheticFilter = wp.filter

	if wp.filter == "" || !isPathLikeFilter(wp.filter) || len(wp.filteredRegistered()) > 0 {
		wp.synthetic = nil
		return
	}

	absPath := expandTypedPath(wp.filter)
	item := WorkspaceItem{
		Label: homeShorten(absPath),
		Path:  absPath,
	}
	if wp.classifier != nil {
		item.Kind, item.Note = wp.classifier(wp.filter)
	}
	wp.synthetic = &item
}

func expandTypedPath(raw string) string {
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if raw == "~" {
				return home
			}
			return filepath.Join(home, raw[2:])
		}
	}
	if abs, err := filepath.Abs(raw); err == nil {
		return abs
	}
	return raw
}

func homeShorten(absPath string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return absPath
	}
	if absPath == home {
		return "~"
	}
	if strings.HasPrefix(absPath, home+string(filepath.Separator)) {
		return "~" + absPath[len(home):]
	}
	return absPath
}

func (wp *WorkspacePicker) visibleItems() []WorkspaceItem {
	items := wp.filteredRegistered()
	if wp.synthetic != nil {
		items = append(items, *wp.synthetic)
	}
	return items
}

func (wp *WorkspacePicker) clampCursor() {
	items := wp.visibleItems()
	if wp.cursor >= len(items) {
		if len(items) > 0 {
			wp.cursor = len(items) - 1
		} else {
			wp.cursor = 0
		}
	}
	if wp.cursor < 0 {
		wp.cursor = 0
	}
}

// Selected returns the currently highlighted item.
func (wp *WorkspacePicker) Selected() (WorkspaceItem, bool) {
	items := wp.visibleItems()
	if wp.cursor < 0 || wp.cursor >= len(items) {
		return WorkspaceItem{}, false
	}
	return items[wp.cursor], true
}

// HandleKeyPress processes a key event. Enter and Esc are deliberately not
// consumed here — the parent state machine owns dismiss/confirm.
func (wp *WorkspacePicker) HandleKeyPress(msg tea.KeyMsg) (consumed bool) {
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		if wp.cursor > 0 {
			wp.cursor--
		}
		return true
	case tea.KeyDown, tea.KeyCtrlN:
		items := wp.visibleItems()
		if wp.cursor < len(items)-1 {
			wp.cursor++
		}
		return true
	case tea.KeyBackspace:
		if len(wp.filter) > 0 {
			runes := []rune(wp.filter)
			wp.filter = string(runes[:len(runes)-1])
			wp.updateSynthetic()
			wp.clampCursor()
		}
		return true
	case tea.KeyRunes:
		wp.filter += string(msg.Runes)
		wp.updateSynthetic()
		wp.clampCursor()
		return true
	case tea.KeySpace:
		wp.filter += " "
		wp.updateSynthetic()
		wp.clampCursor()
		return true
	}
	return false
}

var (
	wpFilterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))

	wpSelectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("0"))

	wpDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	wpNoteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

const wpMaxVisible = 5

// Render renders the filter line and the windowed item list.
func (wp *WorkspacePicker) Render() string {
	var s strings.Builder

	cursorGlyph := ""
	if wp.focused {
		cursorGlyph = "█"
	}
	s.WriteString(wpFilterStyle.Render("Filter: " + wp.filter + cursorGlyph))
	s.WriteString("\n\n")

	items := wp.visibleItems()
	if len(items) == 0 {
		s.WriteString(wpDimStyle.Render("  No matching workspaces"))
		return s.String()
	}

	start := 0
	if wp.cursor >= wpMaxVisible {
		start = wp.cursor - wpMaxVisible + 1
	}
	end := start + wpMaxVisible
	if end > len(items) {
		end = len(items)
	}

	if start > 0 {
		s.WriteString(wpDimStyle.Render("  ↑" + strconv.Itoa(start) + " more"))
		s.WriteString("\n")
	}

	innerWidth := wp.width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}

	for i := start; i < end; i++ {
		item := items[i]
		row := wpRenderRow(item, innerWidth)
		if i == wp.cursor && wp.focused {
			s.WriteString(wpSelectedStyle.Render("> " + row))
		} else if i == wp.cursor {
			s.WriteString("> " + row)
		} else {
			s.WriteString(wpDimStyle.Render("  " + row))
		}
		if i < end-1 {
			s.WriteString("\n")
		}
	}

	if end < len(items) {
		s.WriteString("\n")
		s.WriteString(wpDimStyle.Render("  ↓" + strconv.Itoa(len(items)-end) + " more"))
	}

	return s.String()
}

func wpRenderRow(item WorkspaceItem, width int) string {
	if item.Note == "" {
		return item.Label
	}
	note := wpNoteStyle.Render(item.Note)
	labelWidth := runewidth.StringWidth(item.Label)
	noteWidth := runewidth.StringWidth(item.Note)
	pad := width - labelWidth - noteWidth
	if pad < 1 {
		pad = 1
	}
	return item.Label + strings.Repeat(" ", pad) + note
}
