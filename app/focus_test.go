package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyMsgToBytes(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want []byte
	}{
		{
			name: "plain runes",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")},
			want: []byte("hello"),
		},
		{
			name: "umlaut runes",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("grüezi")},
			want: []byte("grüezi"),
		},
		{
			name: "space",
			msg:  tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")},
			want: []byte(" "),
		},
		{
			name: "enter",
			msg:  tea.KeyMsg{Type: tea.KeyEnter},
			want: []byte{0x0d},
		},
		{
			name: "escape",
			msg:  tea.KeyMsg{Type: tea.KeyEsc},
			want: []byte{0x1b},
		},
		{
			name: "backspace",
			msg:  tea.KeyMsg{Type: tea.KeyBackspace},
			want: []byte{0x7f},
		},
		{
			name: "ctrl+c",
			msg:  tea.KeyMsg{Type: tea.KeyCtrlC},
			want: []byte{0x03},
		},
		{
			name: "tab",
			msg:  tea.KeyMsg{Type: tea.KeyTab},
			want: []byte{0x09},
		},
		{
			name: "arrow up",
			msg:  tea.KeyMsg{Type: tea.KeyUp},
			want: []byte("\x1b[A"),
		},
		{
			name: "shift+tab",
			msg:  tea.KeyMsg{Type: tea.KeyShiftTab},
			want: []byte("\x1b[Z"),
		},
		{
			name: "alt+rune prefixes escape",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true},
			want: []byte("\x1bb"),
		},
		{
			name: "paste is bracketed",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1\nline2"), Paste: true},
			want: []byte("\x1b[200~line1\nline2\x1b[201~"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, keyMsgToBytes(tt.msg))
		})
	}
}

// fakeFocusTarget records the calls focus-mode routing makes, so tests can
// assert which concrete target it actually reached.
type fakeFocusTarget struct {
	alive        bool
	sentKeys     []string
	copyModeExit int
	sendErr      error
}

func (f *fakeFocusTarget) SendKeys(keys string) error {
	f.sentKeys = append(f.sentKeys, keys)
	return f.sendErr
}

func (f *fakeFocusTarget) ExitCopyMode() error {
	f.copyModeExit++
	return nil
}

func (f *fakeFocusTarget) Alive() bool { return f.alive }

// fakePtyFactory satisfies tmux.PtyFactory without spawning a real PTY, so
// tests can construct an Instance whose own tmux session never shells out.
// Start runs cmd through cmdExec first, mirroring MockPtyFactory in
// ui/preview_test.go, so a "new-session" command still flips the mock's
// session-exists tracking that Start()'s readiness poll waits on.
type fakePtyFactory struct {
	dir     string
	n       int
	cmdExec cmd_test.MockCmdExec
}

func (f *fakePtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	f.n++
	file, err := os.OpenFile(filepath.Join(f.dir, fmt.Sprintf("pty-%d", f.n)), os.O_CREATE|os.O_RDWR, 0644)
	if err == nil {
		_ = f.cmdExec.Run(cmd)
	}
	return file, err
}

func (f *fakePtyFactory) Close() {}

// newStartedInPlaceInstance builds an in-place instance whose own tmux session
// is fully mocked (never touches the real tmux binary), so it reports as
// alive and started for focus-mode routing tests. InPlace sidesteps the git
// worktree machinery that a normal instance's WorkingDir() would need.
func newStartedInPlaceInstance(t *testing.T, title string) *session.Instance {
	t.Helper()
	sessionCreated := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd.String()
			if strings.Contains(cmdStr, "has-session") {
				if sessionCreated {
					return nil
				}
				return fmt.Errorf("session does not exist")
			}
			if strings.Contains(cmdStr, "new-session") {
				sessionCreated = true
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) { return []byte(""), nil },
	}

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   title,
		Path:    t.TempDir(),
		Program: "bash",
		InPlace: true,
	})
	require.NoError(t, err)

	ts := tmux.NewTmuxSessionWithDeps(title, "bash", "", &fakePtyFactory{dir: t.TempDir(), cmdExec: cmdExec}, cmdExec)
	inst.SetTmuxSession(ts)
	require.NoError(t, inst.Start(true))
	return inst
}

func newFocusTestHome(t *testing.T) *home {
	t.Helper()
	spinnerModel := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return &home{
		ctx:          context.Background(),
		state:        stateDefault,
		list:         ui.NewList(&spinnerModel, false),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
	}
}

// TestEnterFocusMode_PreviewTabUsesInstanceTarget verifies that focus mode
// entered from the preview tab stores an instance-backed target.
func TestEnterFocusMode_PreviewTabUsesInstanceTarget(t *testing.T) {
	h := newFocusTestHome(t)
	inst := newStartedInPlaceInstance(t, "preview-target")
	defer func() { _ = inst.Kill() }()

	require.True(t, h.tabbedWindow.IsInPreviewTab())
	_, _ = h.enterFocusMode(inst)

	target, ok := h.focusTarget.(instanceFocusTarget)
	require.True(t, ok, "expected instanceFocusTarget, got %T", h.focusTarget)
	assert.Same(t, inst, target.inst)
}

// TestEnterFocusMode_TerminalTabUsesTerminalTarget verifies that focus mode
// entered from the terminal tab stores the terminal pane itself as the
// target, lazily creating its session first.
func TestEnterFocusMode_TerminalTabUsesTerminalTarget(t *testing.T) {
	h := newFocusTestHome(t)
	inst := newStartedInPlaceInstance(t, "terminal-target")
	defer func() { _ = inst.Kill() }()
	defer h.tabbedWindow.CleanupTerminalForInstance(inst.Title)

	h.tabbedWindow.Toggle() // Preview -> Diff
	h.tabbedWindow.Toggle() // Diff -> Terminal
	require.True(t, h.tabbedWindow.IsInTerminalTab())

	_, _ = h.enterFocusMode(inst)

	assert.Same(t, h.tabbedWindow.Terminal(), h.focusTarget)
}

// TestHandleFocusState_ForwardsToStoredTarget verifies that regular keys are
// routed through whatever focusTarget is currently stored, independent of
// which tab put it there.
func TestHandleFocusState_ForwardsToStoredTarget(t *testing.T) {
	h := newFocusTestHome(t)
	inst := newStartedInPlaceInstance(t, "route-target")
	defer func() { _ = inst.Kill() }()
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	h.state = stateFocus
	fake := &fakeFocusTarget{alive: true}
	h.focusTarget = fake

	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}
	_, _ = h.handleFocusState(keyMsg)

	require.Len(t, fake.sentKeys, 1)
	assert.Equal(t, "x", fake.sentKeys[0])
	assert.Equal(t, stateFocus, h.state, "regular keys must not exit focus mode")
}

// TestHandleFocusState_ExitKeysClearTarget verifies each exit key leaves focus
// mode and clears the stored target, while esc is forwarded rather than
// exiting (agents and shells use it to interrupt).
func TestHandleFocusState_ExitKeysClearTarget(t *testing.T) {
	h := newFocusTestHome(t)
	inst := newStartedInPlaceInstance(t, "exit-target")
	defer func() { _ = inst.Kill() }()
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	exitKeys := []tea.KeyMsg{
		{Type: tea.KeyCtrlQ},
		{Type: tea.KeyCtrlAt},
		{Type: tea.KeyCtrlF},
	}

	for _, keyMsg := range exitKeys {
		t.Run(keyMsg.String(), func(t *testing.T) {
			h.state = stateFocus
			h.focusTarget = &fakeFocusTarget{alive: true}

			_, _ = h.handleFocusState(keyMsg)

			assert.Equal(t, stateDefault, h.state)
			assert.Nil(t, h.focusTarget)
		})
	}

	t.Run("esc is forwarded, not an exit key", func(t *testing.T) {
		h.state = stateFocus
		fake := &fakeFocusTarget{alive: true}
		h.focusTarget = fake

		_, _ = h.handleFocusState(tea.KeyMsg{Type: tea.KeyEsc})

		assert.Equal(t, stateFocus, h.state)
		assert.NotNil(t, h.focusTarget)
		require.Len(t, fake.sentKeys, 1)
		assert.Equal(t, []byte{0x1b}, []byte(fake.sentKeys[0]))
	})
}

// TestHandleFocusState_WheelScrollsPaneNotSession pins the scroll fix. The
// wheel must drive the pane's own snapshot scroll, NOT be forwarded to the
// session: tmux only passes a wheel through to a program that enabled mouse
// tracking, and these agents do not (verified: mouse_any_flag=0), so a
// forwarded wheel is captured by tmux as copy-mode, after which focus mode
// cannot type at all. An earlier version of this test asserted the forwarding
// behaviour, i.e. it locked the bug in.
func TestHandleFocusState_WheelScrollsPaneNotSession(t *testing.T) {
	h := newFocusTestHome(t)
	inst := newStartedInPlaceInstance(t, "wheel-target")
	defer func() { _ = inst.Kill() }()
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)
	h.instanceChanged() // prime the pane with the selected instance

	h.state = stateFocus
	fake := &fakeFocusTarget{alive: true}
	h.focusTarget = fake

	mouseMsg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	_, _ = h.Update(mouseMsg)

	assert.Empty(t, fake.sentKeys, "the wheel must never be written to the session")
	assert.True(t, h.tabbedWindow.IsPreviewInScrollMode(), "the wheel must drive the pane's own scroll mode")
}

// TestHandleFocusState_ShiftArrowsScrollThenTypingSnapsBack is the keyboard
// half of the same rule, plus the follow-on: typing must return to the live
// view so keystrokes are not sent while scrolled-back output is on screen.
func TestHandleFocusState_ShiftArrowsScrollThenTypingSnapsBack(t *testing.T) {
	h := newFocusTestHome(t)
	inst := newStartedInPlaceInstance(t, "shift-scroll-target")
	defer func() { _ = inst.Kill() }()
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)
	h.instanceChanged()

	h.state = stateFocus
	fake := &fakeFocusTarget{alive: true}
	h.focusTarget = fake

	_, _ = h.handleFocusState(tea.KeyMsg{Type: tea.KeyShiftUp})

	assert.Empty(t, fake.sentKeys, "shift+up must not be forwarded as keys or as a wheel event")
	assert.True(t, h.tabbedWindow.IsPreviewInScrollMode())
	assert.Equal(t, stateFocus, h.state)

	_, _ = h.handleFocusState(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.False(t, h.tabbedWindow.IsPreviewInScrollMode(), "typing must snap back to the live view")
	require.Len(t, fake.sentKeys, 1)
	assert.Equal(t, "x", fake.sentKeys[0])
}

// Entering focus mode must release a pane tmux left in copy-mode, or every
// keystroke is consumed as a copy-mode binding and focus mode looks dead.
func TestEnterFocusMode_ReleasesCopyMode(t *testing.T) {
	h := newFocusTestHome(t)
	inst := newStartedInPlaceInstance(t, "copymode-target")
	defer func() { _ = inst.Kill() }()
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	_, _ = h.enterFocusMode(inst)
	require.Equal(t, stateFocus, h.state)

	target, ok := h.focusTarget.(instanceFocusTarget)
	require.True(t, ok, "the preview tab focuses the instance itself")
	assert.Same(t, inst, target.inst)
}
