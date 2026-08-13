package overlay

import (
	"strings"

	"claude-squad/harness"
	"claude-squad/session/tmux"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// HarnessPicker lists the available agent CLIs. Unlike WorkspacePicker there is
// no filtering, so j/k are free to mean cursor motion instead of filter text.
type HarnessPicker struct {
	options    []harness.Option
	cursor     int
	focused    bool
	width      int
	maxVisible int
}

// hpDefaultMaxVisible bounds the row count before SetSize reports the real
// height. Nothing caps how many profiles a workspace declares, so the list has
// to window rather than trust that it fits.
const hpDefaultMaxVisible = 6

func NewHarnessPicker(options []harness.Option) *HarnessPicker {
	return &HarnessPicker{options: options}
}

func (hp *HarnessPicker) SetWidth(w int) {
	hp.width = w
}

func (hp *HarnessPicker) SetMaxVisible(n int) {
	hp.maxVisible = n
}

func (hp *HarnessPicker) Focus() {
	hp.focused = true
}

func (hp *HarnessPicker) Blur() {
	hp.focused = false
}

func (hp *HarnessPicker) Selected() (harness.Option, bool) {
	if hp.cursor < 0 || hp.cursor >= len(hp.options) {
		return harness.Option{}, false
	}
	return hp.options[hp.cursor], true
}

// HandleKeyPress consumes up/down motion (arrows, or "j"/"k" runes) only.
// Enter and Esc are deliberately not consumed — mirroring WorkspacePicker's
// contract, the parent state machine owns confirm/cancel.
func (hp *HarnessPicker) HandleKeyPress(msg tea.KeyMsg) (consumed bool) {
	switch msg.Type {
	case tea.KeyUp:
		if hp.cursor > 0 {
			hp.cursor--
		}
		return true
	case tea.KeyDown:
		if hp.cursor < len(hp.options)-1 {
			hp.cursor++
		}
		return true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return false
		}
		switch msg.Runes[0] {
		case 'k':
			if hp.cursor > 0 {
				hp.cursor--
			}
			return true
		case 'j':
			if hp.cursor < len(hp.options)-1 {
				hp.cursor++
			}
			return true
		}
	}
	return false
}

var (
	hpSelectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("0"))

	hpDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

// renderRow styles each piece against base rather than wrapping an
// already-rendered dot: the dot's trailing ANSI reset would clear base's
// background, leaving a hole across the rest of a selected row.
func (hp *HarnessPicker) renderRow(opt harness.Option, marker string, base lipgloss.Style) string {
	name := tmux.HarnessName(opt.Program)
	label := opt.Label
	if avail := hp.width - runewidth.StringWidth(marker) - runewidth.StringWidth(harness.Badge(name)) - 1; avail > 3 && runewidth.StringWidth(label) > avail {
		label = runewidth.Truncate(label, avail, "...")
	}
	dot := base.Copy().Foreground(harness.DotColor(name)).Render(harness.Badge(name))
	return base.Render(marker) + dot + base.Render(" "+label)
}

func (hp *HarnessPicker) visibleRange() (start, end int) {
	max := hp.maxVisible
	if max <= 0 {
		max = hpDefaultMaxVisible
	}
	if len(hp.options) <= max {
		return 0, len(hp.options)
	}
	start = hp.cursor - max/2
	if start < 0 {
		start = 0
	}
	if start+max > len(hp.options) {
		start = len(hp.options) - max
	}
	return start, start + max
}

func (hp *HarnessPicker) Render() string {
	start, end := hp.visibleRange()

	var rows []string
	if start > 0 {
		rows = append(rows, hpDimStyle.Render("  ..."))
	}
	for i := start; i < end; i++ {
		switch {
		case i == hp.cursor && hp.focused:
			rows = append(rows, hp.renderRow(hp.options[i], "> ", hpSelectedStyle))
		case i == hp.cursor:
			rows = append(rows, hp.renderRow(hp.options[i], "> ", lipgloss.NewStyle()))
		default:
			rows = append(rows, hp.renderRow(hp.options[i], "  ", hpDimStyle))
		}
	}
	if end < len(hp.options) {
		rows = append(rows, hpDimStyle.Render("  ..."))
	}
	return strings.Join(rows, "\n")
}

var (
	hpoTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginBottom(1)

	hpoFooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

type HarnessPickerOverlay struct {
	picker *HarnessPicker
	width  int
	height int
}

func NewHarnessPickerOverlay(options []harness.Option) *HarnessPickerOverlay {
	picker := NewHarnessPicker(options)
	picker.Focus()
	return &HarnessPickerOverlay{
		picker: picker,
		width:  50,
	}
}

func (o *HarnessPickerOverlay) SetWidth(width int) {
	o.width = width
	o.picker.SetWidth(width - 6)
}

func (o *HarnessPickerOverlay) SetSize(width, height int) {
	o.height = height
	// Title, blank, footer, padding and border; the rest is rows.
	o.picker.SetMaxVisible(height - hpChromeRows)
	o.SetWidth(width)
}

// hpChromeRows is everything Render emits besides option rows.
const hpChromeRows = 8

func (o *HarnessPickerOverlay) HandleKeyPress(msg tea.KeyMsg) (consumed bool) {
	return o.picker.HandleKeyPress(msg)
}

func (o *HarnessPickerOverlay) Selected() (harness.Option, bool) {
	return o.picker.Selected()
}

func (o *HarnessPickerOverlay) Render() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(o.width)

	content := hpoTitleStyle.Render("Select agent") + "\n"
	content += o.picker.Render() + "\n\n"
	content += hpoFooterStyle.Render("↑↓/jk move · enter select · esc cancel")

	return style.Render(content)
}
