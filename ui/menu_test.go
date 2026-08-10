package ui

import (
	"claude-squad/session"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMenuFocusStateShowsHint(t *testing.T) {
	m := NewMenu()
	m.SetSize(120, 1)
	m.SetState(StateFocus)

	out := m.String()
	assert.Contains(t, out, "back to menu")
	assert.Contains(t, out, "keys go to the session")
	// The normal action keys must not leak into the focus hint.
	assert.NotContains(t, out, "push branch")
}

func TestMenuDefaultAdvertisesEnterAndAttach(t *testing.T) {
	m := NewMenu()
	m.SetSize(240, 1)
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   "menu-keys",
		Path:    ".",
		Program: "bash",
	})
	require.NoError(t, err)
	m.SetInstance(inst) // drives the default (non-empty) option layout

	out := stripANSI(m.String())
	// Enter types into the preview and o attaches, so the menu must not still
	// advertise f as the way to reach the session.
	assert.Contains(t, out, "↵ type")
	assert.Contains(t, out, "o attach")
	assert.NotContains(t, out, "f focus")
}

// stripANSI removes escape sequences so assertions match on visible text.
func stripANSI(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		if runes[i] == 0x1b {
			i = skipEscape(runes, i)
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

func TestMenuFitsCommonWidths(t *testing.T) {
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   "menu-width",
		Path:    ".",
		Program: "bash",
	})
	require.NoError(t, err)

	// 160 is the narrowest width the full menu is committed to fitting. It
	// overflows below that; shortening a label or dropping one from the bar
	// (they stay in the ? help) is the lever if that becomes a problem.
	for _, width := range []int{160, 200} {
		m := NewMenu()
		m.SetSize(width, 1)
		m.SetInstance(inst)
		line := strings.TrimSpace(stripANSI(m.String()))
		t.Logf("width %d (%d visible cols): %s", width, runewidth.StringWidth(line), line)
		assert.LessOrEqual(t, runewidth.StringWidth(line), width,
			"menu overflows at width %d — the workspace group is crowded, shorten a label", width)
	}
}
