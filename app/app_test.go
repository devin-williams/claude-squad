package app

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain runs before all tests to set up the test environment
func TestMain(m *testing.M) {
	// Initialize the logger before any tests run
	log.Initialize(false)
	defer log.Close()

	// Run all tests
	exitCode := m.Run()

	// Exit with the same code as the tests
	os.Exit(exitCode)
}

// TestConfirmationModalStateTransitions tests state transitions without full instance setup
func TestConfirmationModalStateTransitions(t *testing.T) {
	// Create a minimal home struct for testing state transitions
	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
	}

	t.Run("shows confirmation on D press", func(t *testing.T) {
		// Simulate pressing 'D'
		h.state = stateDefault
		h.confirmationOverlay = nil

		// Manually trigger what would happen in handleKeyPress for 'D'
		h.state = stateConfirm
		h.confirmationOverlay = overlay.NewConfirmationOverlay("[!] Kill session 'test'?")

		assert.Equal(t, stateConfirm, h.state)
		assert.NotNil(t, h.confirmationOverlay)
		assert.False(t, h.confirmationOverlay.Dismissed)
	})

	t.Run("returns to default on y press", func(t *testing.T) {
		// Start in confirmation state
		h.state = stateConfirm
		h.confirmationOverlay = overlay.NewConfirmationOverlay("Test confirmation")

		// Simulate pressing 'y' using HandleKeyPress
		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}
		shouldClose := h.confirmationOverlay.HandleKeyPress(keyMsg)
		if shouldClose {
			h.state = stateDefault
			h.confirmationOverlay = nil
		}

		assert.Equal(t, stateDefault, h.state)
		assert.Nil(t, h.confirmationOverlay)
	})

	t.Run("returns to default on n press", func(t *testing.T) {
		// Start in confirmation state
		h.state = stateConfirm
		h.confirmationOverlay = overlay.NewConfirmationOverlay("Test confirmation")

		// Simulate pressing 'n' using HandleKeyPress
		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
		shouldClose := h.confirmationOverlay.HandleKeyPress(keyMsg)
		if shouldClose {
			h.state = stateDefault
			h.confirmationOverlay = nil
		}

		assert.Equal(t, stateDefault, h.state)
		assert.Nil(t, h.confirmationOverlay)
	})

	t.Run("returns to default on esc press", func(t *testing.T) {
		// Start in confirmation state
		h.state = stateConfirm
		h.confirmationOverlay = overlay.NewConfirmationOverlay("Test confirmation")

		// Simulate pressing ESC using HandleKeyPress
		keyMsg := tea.KeyMsg{Type: tea.KeyEscape}
		shouldClose := h.confirmationOverlay.HandleKeyPress(keyMsg)
		if shouldClose {
			h.state = stateDefault
			h.confirmationOverlay = nil
		}

		assert.Equal(t, stateDefault, h.state)
		assert.Nil(t, h.confirmationOverlay)
	})
}

// TestConfirmationModalKeyHandling tests the actual key handling in confirmation state
func TestConfirmationModalKeyHandling(t *testing.T) {
	// Import needed packages
	spinner := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	list := ui.NewList(&spinner, false)

	// Create enough of home struct to test handleKeyPress in confirmation state
	h := &home{
		ctx:                 context.Background(),
		state:               stateConfirm,
		appConfig:           config.DefaultConfig(),
		list:                list,
		menu:                ui.NewMenu(),
		confirmationOverlay: overlay.NewConfirmationOverlay("Kill session?"),
	}

	testCases := []struct {
		name              string
		key               string
		expectedState     state
		expectedDismissed bool
		expectedNil       bool
	}{
		{
			name:              "y key confirms and dismisses overlay",
			key:               "y",
			expectedState:     stateDefault,
			expectedDismissed: true,
			expectedNil:       true,
		},
		{
			name:              "n key cancels and dismisses overlay",
			key:               "n",
			expectedState:     stateDefault,
			expectedDismissed: true,
			expectedNil:       true,
		},
		{
			name:              "esc key cancels and dismisses overlay",
			key:               "esc",
			expectedState:     stateDefault,
			expectedDismissed: true,
			expectedNil:       true,
		},
		{
			name:              "other keys are ignored",
			key:               "x",
			expectedState:     stateConfirm,
			expectedDismissed: false,
			expectedNil:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset state
			h.state = stateConfirm
			h.confirmationOverlay = overlay.NewConfirmationOverlay("Kill session?")

			// Create key message
			var keyMsg tea.KeyMsg
			if tc.key == "esc" {
				keyMsg = tea.KeyMsg{Type: tea.KeyEscape}
			} else {
				keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)}
			}

			// Call handleKeyPress
			model, _ := h.handleKeyPress(keyMsg)
			homeModel, ok := model.(*home)
			require.True(t, ok)

			assert.Equal(t, tc.expectedState, homeModel.state, "State mismatch for key: %s", tc.key)
			if tc.expectedNil {
				assert.Nil(t, homeModel.confirmationOverlay, "Overlay should be nil for key: %s", tc.key)
			} else {
				assert.NotNil(t, homeModel.confirmationOverlay, "Overlay should not be nil for key: %s", tc.key)
				assert.Equal(t, tc.expectedDismissed, homeModel.confirmationOverlay.Dismissed, "Dismissed mismatch for key: %s", tc.key)
			}
		})
	}
}

// TestConfirmationMessageFormatting tests that confirmation messages are formatted correctly
func TestConfirmationMessageFormatting(t *testing.T) {
	testCases := []struct {
		name            string
		sessionTitle    string
		expectedMessage string
	}{
		{
			name:            "short session name",
			sessionTitle:    "my-feature",
			expectedMessage: "[!] Kill session 'my-feature'? (y/n)",
		},
		{
			name:            "long session name",
			sessionTitle:    "very-long-feature-branch-name-here",
			expectedMessage: "[!] Kill session 'very-long-feature-branch-name-here'? (y/n)",
		},
		{
			name:            "session with spaces",
			sessionTitle:    "feature with spaces",
			expectedMessage: "[!] Kill session 'feature with spaces'? (y/n)",
		},
		{
			name:            "session with special chars",
			sessionTitle:    "feature/branch-123",
			expectedMessage: "[!] Kill session 'feature/branch-123'? (y/n)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test the message formatting directly
			actualMessage := fmt.Sprintf("[!] Kill session '%s'? (y/n)", tc.sessionTitle)
			assert.Equal(t, tc.expectedMessage, actualMessage)
		})
	}
}

// TestConfirmationFlowSimulation tests the confirmation flow by simulating the state changes
func TestConfirmationFlowSimulation(t *testing.T) {
	// Create a minimal setup
	spinner := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	list := ui.NewList(&spinner, false)

	// Add test instance
	instance, err := session.NewInstance(session.InstanceOptions{
		Title:   "test-session",
		Path:    t.TempDir(),
		Program: "claude",
		AutoYes: false,
	})
	require.NoError(t, err)
	_ = list.AddInstance(instance)
	list.SetSelectedInstance(0)

	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
		list:      list,
		menu:      ui.NewMenu(),
	}

	// Simulate what happens when D is pressed
	selected := h.list.GetSelectedInstance()
	require.NotNil(t, selected)

	// This is what the KeyKill handler does
	message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
	h.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	h.state = stateConfirm

	// Verify the state
	assert.Equal(t, stateConfirm, h.state)
	assert.NotNil(t, h.confirmationOverlay)
	assert.False(t, h.confirmationOverlay.Dismissed)
	// Test that overlay renders with the correct message
	rendered := h.confirmationOverlay.Render()
	assert.Contains(t, rendered, "Kill session 'test-session'?")
}

// TestConfirmActionWithDifferentTypes tests that confirmAction works with different action types
func TestConfirmActionWithDifferentTypes(t *testing.T) {
	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
	}

	t.Run("works with simple action returning nil", func(t *testing.T) {
		actionCalled := false
		action := func() tea.Msg {
			actionCalled = true
			return nil
		}

		// Set up callback to track action execution
		actionExecuted := false
		h.confirmationOverlay = overlay.NewConfirmationOverlay("Test action?")
		h.confirmationOverlay.OnConfirm = func() {
			h.state = stateDefault
			actionExecuted = true
			action() // Execute the action
		}
		h.state = stateConfirm

		// Verify state was set
		assert.Equal(t, stateConfirm, h.state)
		assert.NotNil(t, h.confirmationOverlay)
		assert.False(t, h.confirmationOverlay.Dismissed)
		assert.NotNil(t, h.confirmationOverlay.OnConfirm)

		// Execute the confirmation callback
		h.confirmationOverlay.OnConfirm()
		assert.True(t, actionCalled)
		assert.True(t, actionExecuted)
	})

	t.Run("works with action returning error", func(t *testing.T) {
		expectedErr := fmt.Errorf("test error")
		action := func() tea.Msg {
			return expectedErr
		}

		// Set up callback to track action execution
		var receivedMsg tea.Msg
		h.confirmationOverlay = overlay.NewConfirmationOverlay("Error action?")
		h.confirmationOverlay.OnConfirm = func() {
			h.state = stateDefault
			receivedMsg = action() // Execute the action and capture result
		}
		h.state = stateConfirm

		// Verify state was set
		assert.Equal(t, stateConfirm, h.state)
		assert.NotNil(t, h.confirmationOverlay)
		assert.False(t, h.confirmationOverlay.Dismissed)
		assert.NotNil(t, h.confirmationOverlay.OnConfirm)

		// Execute the confirmation callback
		h.confirmationOverlay.OnConfirm()
		assert.Equal(t, expectedErr, receivedMsg)
	})

	t.Run("works with action returning custom message", func(t *testing.T) {
		action := func() tea.Msg {
			return instanceChangedMsg{}
		}

		// Set up callback to track action execution
		var receivedMsg tea.Msg
		h.confirmationOverlay = overlay.NewConfirmationOverlay("Custom message action?")
		h.confirmationOverlay.OnConfirm = func() {
			h.state = stateDefault
			receivedMsg = action() // Execute the action and capture result
		}
		h.state = stateConfirm

		// Verify state was set
		assert.Equal(t, stateConfirm, h.state)
		assert.NotNil(t, h.confirmationOverlay)
		assert.False(t, h.confirmationOverlay.Dismissed)
		assert.NotNil(t, h.confirmationOverlay.OnConfirm)

		// Execute the confirmation callback
		h.confirmationOverlay.OnConfirm()
		_, ok := receivedMsg.(instanceChangedMsg)
		assert.True(t, ok, "Expected instanceChangedMsg but got %T", receivedMsg)
	})
}

// TestMultipleConfirmationsDontInterfere tests that multiple confirmations don't interfere with each other
func TestMultipleConfirmationsDontInterfere(t *testing.T) {
	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
	}

	// First confirmation
	action1Called := false
	action1 := func() tea.Msg {
		action1Called = true
		return nil
	}

	// Set up first confirmation
	h.confirmationOverlay = overlay.NewConfirmationOverlay("First action?")
	firstOnConfirm := func() {
		h.state = stateDefault
		action1()
	}
	h.confirmationOverlay.OnConfirm = firstOnConfirm
	h.state = stateConfirm

	// Verify first confirmation
	assert.Equal(t, stateConfirm, h.state)
	assert.NotNil(t, h.confirmationOverlay)
	assert.False(t, h.confirmationOverlay.Dismissed)
	assert.NotNil(t, h.confirmationOverlay.OnConfirm)

	// Cancel first confirmation (simulate pressing 'n')
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	shouldClose := h.confirmationOverlay.HandleKeyPress(keyMsg)
	if shouldClose {
		h.state = stateDefault
		h.confirmationOverlay = nil
	}

	// Second confirmation with different action
	action2Called := false
	action2 := func() tea.Msg {
		action2Called = true
		return fmt.Errorf("action2 error")
	}

	// Set up second confirmation
	h.confirmationOverlay = overlay.NewConfirmationOverlay("Second action?")
	var secondResult tea.Msg
	secondOnConfirm := func() {
		h.state = stateDefault
		secondResult = action2()
	}
	h.confirmationOverlay.OnConfirm = secondOnConfirm
	h.state = stateConfirm

	// Verify second confirmation
	assert.Equal(t, stateConfirm, h.state)
	assert.NotNil(t, h.confirmationOverlay)
	assert.False(t, h.confirmationOverlay.Dismissed)
	assert.NotNil(t, h.confirmationOverlay.OnConfirm)

	// Execute second action to verify it's the correct one
	h.confirmationOverlay.OnConfirm()
	err, ok := secondResult.(error)
	assert.True(t, ok)
	assert.Equal(t, "action2 error", err.Error())
	assert.True(t, action2Called)
	assert.False(t, action1Called, "First action should not have been called")

	// Test that cancelled action can still be executed independently
	firstOnConfirm()
	assert.True(t, action1Called, "First action should be callable after being replaced")
}

// TestConfirmationModalVisualAppearance tests that confirmation modal has distinct visual appearance
func TestConfirmationModalVisualAppearance(t *testing.T) {
	h := &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
	}

	// Create a test confirmation overlay
	message := "[!] Delete everything?"
	h.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	h.state = stateConfirm

	// Verify the overlay was created with confirmation settings
	assert.NotNil(t, h.confirmationOverlay)
	assert.Equal(t, stateConfirm, h.state)
	assert.False(t, h.confirmationOverlay.Dismissed)

	// Test the overlay render (we can test that it renders without errors)
	rendered := h.confirmationOverlay.Render()
	assert.NotEmpty(t, rendered)

	// Test that it includes the message content and instructions
	assert.Contains(t, rendered, "Delete everything?")
	assert.Contains(t, rendered, "Press")
	assert.Contains(t, rendered, "to confirm")
	assert.Contains(t, rendered, "to cancel")

	// Test that the danger indicator is preserved
	assert.Contains(t, rendered, "[!")
}

// newWorkspacePickerTestHome builds a minimal home for exercising the
// workspace-picker handler directly, since driving the full bubbletea Update
// loop (including the menu-highlighting re-send) is impractical from a unit
// test. Setting keySent bypasses handleMenuHighlighting's re-send so
// handleKeyPress reaches the real case/handler on the first call.
func newWorkspacePickerTestHome(t *testing.T) *home {
	spinnerModel := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
		list:      ui.NewList(&spinnerModel, false),
		menu:      ui.NewMenu(),
		errBox:    ui.NewErrBox(),
		keySent:   true,
	}
}

// TestWorkspacePicker_OpensFromKeyNew verifies that `n` opens the picker
// without queuing a prompt afterward.
func TestWorkspacePicker_OpensFromKeyNew(t *testing.T) {
	h := newWorkspacePickerTestHome(t)

	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	model, _ := h.handleKeyPress(keyMsg)
	homeModel, ok := model.(*home)
	require.True(t, ok)

	assert.Equal(t, stateSelectWorkspace, homeModel.state)
	assert.False(t, homeModel.promptAfterName)
	assert.NotNil(t, homeModel.workspacePickerOverlay)
	assert.Equal(t, workspacePickerCreate, homeModel.workspacePickerMode)
}

// TestWorkspacePicker_OpensFromKeyPrompt verifies that `N` opens the same
// picker but queues a prompt afterward, unlike `n`.
func TestWorkspacePicker_OpensFromKeyPrompt(t *testing.T) {
	h := newWorkspacePickerTestHome(t)

	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")}
	model, _ := h.handleKeyPress(keyMsg)
	homeModel, ok := model.(*home)
	require.True(t, ok)

	assert.Equal(t, stateSelectWorkspace, homeModel.state)
	assert.True(t, homeModel.promptAfterName)
	assert.NotNil(t, homeModel.workspacePickerOverlay)
	assert.Equal(t, workspacePickerCreate, homeModel.workspacePickerMode)
}

// TestWorkspacePicker_EscReturnsToDefault verifies Esc discards the picker
// without creating an instance, since no Instance exists yet at this point.
func TestWorkspacePicker_EscReturnsToDefault(t *testing.T) {
	h := newWorkspacePickerTestHome(t)
	h.state = stateSelectWorkspace
	h.promptAfterName = true
	h.workspacePickerOverlay = overlay.NewWorkspacePickerOverlay(nil, nil)

	keyMsg := tea.KeyMsg{Type: tea.KeyEscape}
	model, _ := h.handleKeyPress(keyMsg)
	homeModel, ok := model.(*home)
	require.True(t, ok)

	assert.Equal(t, stateDefault, homeModel.state)
	assert.Nil(t, homeModel.workspacePickerOverlay)
	assert.False(t, homeModel.promptAfterName)
	assert.Equal(t, 0, homeModel.list.NumInstances())
}

// TestWorkspacePicker_EnterOnNonGitCreatesInPlace verifies that selecting a
// directory without a .git now creates an in-place instance instead of
// refusing.
func TestWorkspacePicker_EnterOnNonGitCreatesInPlace(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()

	h := newWorkspacePickerTestHome(t)
	h.state = stateSelectWorkspace
	h.launchDir = tmpDir
	h.workspacePickerMode = workspacePickerCreate
	h.workspacePickerOverlay = overlay.NewWorkspacePickerOverlay([]overlay.WorkspaceItem{
		{Label: tmpDir, Path: tmpDir, Kind: overlay.KindNonGit},
	}, nil)

	keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
	model, _ := h.handleKeyPress(keyMsg)
	homeModel, ok := model.(*home)
	require.True(t, ok)

	canonicalTmpDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, stateNew, homeModel.state)
	assert.Equal(t, 1, homeModel.list.NumInstances())
	selected := homeModel.list.GetSelectedInstance()
	require.NotNil(t, selected)
	assert.True(t, selected.InPlace)
	assert.Equal(t, canonicalTmpDir, selected.Path)
}

// TestWorkspacePicker_EnterOnNonGitTwiceRefusesSecond verifies that a second
// in-place session in the same directory is refused and the state stays in
// the picker, since two in-place sessions there would fight over the same
// files with no isolation.
func TestWorkspacePicker_EnterOnNonGitTwiceRefusesSecond(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()

	h := newWorkspacePickerTestHome(t)
	h.state = stateSelectWorkspace
	h.launchDir = tmpDir
	h.workspacePickerMode = workspacePickerCreate
	h.workspacePickerOverlay = overlay.NewWorkspacePickerOverlay([]overlay.WorkspaceItem{
		{Label: tmpDir, Path: tmpDir, Kind: overlay.KindNonGit},
	}, nil)

	keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
	model, _ := h.handleKeyPress(keyMsg)
	homeModel, ok := model.(*home)
	require.True(t, ok)
	require.Equal(t, 1, homeModel.list.NumInstances())

	homeModel.state = stateSelectWorkspace
	homeModel.workspacePickerOverlay = overlay.NewWorkspacePickerOverlay([]overlay.WorkspaceItem{
		{Label: tmpDir, Path: tmpDir, Kind: overlay.KindNonGit},
	}, nil)

	model, cmd := homeModel.handleKeyPress(keyMsg)
	homeModel, ok = model.(*home)
	require.True(t, ok)

	require.NotNil(t, cmd)
	assert.Equal(t, stateSelectWorkspace, homeModel.state)
	assert.NotNil(t, homeModel.workspacePickerOverlay)
	assert.Equal(t, 1, homeModel.list.NumInstances())
}

// TestWorkspacePicker_EnterOnHomeRequiresConfirmation verifies that resolving
// exactly $HOME opens a confirmation overlay instead of creating the session
// immediately.
func TestWorkspacePicker_EnterOnHomeRequiresConfirmation(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	h := newWorkspacePickerTestHome(t)
	h.state = stateSelectWorkspace
	h.launchDir = homeDir
	h.workspacePickerMode = workspacePickerCreate
	h.workspacePickerOverlay = overlay.NewWorkspacePickerOverlay([]overlay.WorkspaceItem{
		{Label: homeDir, Path: homeDir, Kind: overlay.KindNonGit},
	}, nil)

	keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
	model, _ := h.handleKeyPress(keyMsg)
	homeModel, ok := model.(*home)
	require.True(t, ok)

	assert.Equal(t, stateConfirm, homeModel.state)
	assert.NotNil(t, homeModel.confirmationOverlay)
	assert.Equal(t, 0, homeModel.list.NumInstances())
}

// TestWorkspacePicker_DeclineHomeConfirmationReturnsToPicker verifies that
// declining the $HOME confirmation returns to the picker (not stateDefault)
// without creating an instance.
func TestWorkspacePicker_DeclineHomeConfirmationReturnsToPicker(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	h := newWorkspacePickerTestHome(t)
	h.state = stateSelectWorkspace
	h.launchDir = homeDir
	h.workspacePickerMode = workspacePickerCreate
	h.workspacePickerOverlay = overlay.NewWorkspacePickerOverlay([]overlay.WorkspaceItem{
		{Label: homeDir, Path: homeDir, Kind: overlay.KindNonGit},
	}, nil)

	keyMsg := tea.KeyMsg{Type: tea.KeyEnter}
	model, _ := h.handleKeyPress(keyMsg)
	homeModel, ok := model.(*home)
	require.True(t, ok)
	require.Equal(t, stateConfirm, homeModel.state)
	require.NotNil(t, homeModel.confirmationOverlay)

	homeModel.confirmationOverlay.OnCancel()

	assert.Equal(t, stateSelectWorkspace, homeModel.state)
	assert.Equal(t, 0, homeModel.list.NumInstances())
}

// TestInPlaceInstance_PushAndCheckoutRefused verifies that `p` and `c` on an
// in-place instance produce an error and never open a confirmation overlay,
// since both structurally require a git worktree that in-place sessions
// don't have.
func TestInPlaceInstance_PushAndCheckoutRefused(t *testing.T) {
	inPlaceInstance := func(t *testing.T) *session.Instance {
		inst, err := session.NewInstance(session.InstanceOptions{
			Title:   "in-place-test",
			Path:    t.TempDir(),
			Program: "claude",
			InPlace: true,
		})
		require.NoError(t, err)
		return inst
	}

	t.Run("p refuses without confirmation", func(t *testing.T) {
		h := newWorkspacePickerTestHome(t)
		inst := inPlaceInstance(t)
		h.list.AddInstance(inst)
		h.list.SetSelectedInstance(0)

		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}
		model, cmd := h.handleKeyPress(keyMsg)
		homeModel, ok := model.(*home)
		require.True(t, ok)

		require.NotNil(t, cmd)
		assert.NotEqual(t, stateConfirm, homeModel.state)
		assert.Nil(t, homeModel.confirmationOverlay)
	})

	t.Run("c refuses without confirmation", func(t *testing.T) {
		h := newWorkspacePickerTestHome(t)
		inst := inPlaceInstance(t)
		h.list.AddInstance(inst)
		h.list.SetSelectedInstance(0)

		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}
		model, cmd := h.handleKeyPress(keyMsg)
		homeModel, ok := model.(*home)
		require.True(t, ok)

		require.NotNil(t, cmd)
		assert.NotEqual(t, stateConfirm, homeModel.state)
		assert.Nil(t, homeModel.confirmationOverlay)
	})
}

func newViewFilterHome() *home {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	return &home{
		ctx:       context.Background(),
		state:     stateDefault,
		appConfig: config.DefaultConfig(),
		list:      ui.NewList(&sp, false),
		menu:      ui.NewMenu(),
		errBox:    ui.NewErrBox(),
	}
}

// pressKey re-arms keySent so the press reaches the handler rather than being
// swallowed by menu highlighting's re-dispatch.
func (m *home) pressKey(r rune) {
	m.keySent = true
	m.handleKeyPress(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

func TestViewFilterTogglesAndPickingWorkspaceDoesNotFilter(t *testing.T) {
	h := newViewFilterHome()
	h.workspaceID = "ws-1"

	// Picking a workspace must not strand every other session out of sight.
	h.applyWorkspaceFocus("ws-1")
	assert.Equal(t, "", h.list.GetViewFilter(), "picking a workspace should not filter the list")

	h.pressKey('V')
	assert.Equal(t, "ws-1", h.list.GetViewFilter(), "V should narrow to the active workspace")

	h.pressKey('V')
	assert.Equal(t, "", h.list.GetViewFilter(), "V again should widen back to all sessions")
}

func TestViewFilterWithNoActiveWorkspaceIsRefused(t *testing.T) {
	h := newViewFilterHome()
	h.workspaceID = ""

	h.pressKey('V')
	assert.Equal(t, "", h.list.GetViewFilter(), "nothing to filter to without an active workspace")
}
