package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	wpoTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginBottom(1)

	wpoFooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// WorkspacePickerOverlay is the bordered-box wrapper around WorkspacePicker,
// mirroring ConfirmationOverlay's construction and render shape.
type WorkspacePickerOverlay struct {
	picker *WorkspacePicker
	width  int
	height int
}

// NewWorkspacePickerOverlay wraps a WorkspacePicker in a bordered box.
func NewWorkspacePickerOverlay(items []WorkspaceItem, classifier PathClassifier) *WorkspacePickerOverlay {
	picker := NewWorkspacePicker(items, classifier)
	picker.Focus()
	return &WorkspacePickerOverlay{
		picker: picker,
		width:  50,
	}
}

func (o *WorkspacePickerOverlay) SetWidth(width int) {
	o.width = width
	o.picker.SetWidth(width - 6)
}

func (o *WorkspacePickerOverlay) SetSize(width, height int) {
	o.height = height
	o.SetWidth(width)
}

// HandleKeyPress delegates to the underlying picker.
func (o *WorkspacePickerOverlay) HandleKeyPress(msg tea.KeyMsg) (consumed bool) {
	return o.picker.HandleKeyPress(msg)
}

// Selected returns the currently highlighted item.
func (o *WorkspacePickerOverlay) Selected() (WorkspaceItem, bool) {
	return o.picker.Selected()
}

// Render renders the bordered picker box.
func (o *WorkspacePickerOverlay) Render() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(o.width)

	content := wpoTitleStyle.Render("Select workspace") + "\n"
	content += o.picker.Render() + "\n\n"
	content += wpoFooterStyle.Render("↑↓ move · enter select · esc cancel")

	return style.Render(content)
}
