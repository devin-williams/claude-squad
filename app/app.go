package app

import (
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const GlobalInstanceLimit = 50

// Run is the main entrypoint into the application. launchDir is the directory
// cs was invoked from, used to resolve relative paths typed into the workspace
// picker; callers must compute it once at startup rather than relying on
// os.Getwd() later.
func Run(ctx context.Context, program string, autoYes bool, workspaceID string, launchDir string) error {
	p := tea.NewProgram(
		newHome(ctx, program, autoYes, workspaceID, launchDir),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // Mouse scroll
	)
	_, err := p.Run()
	return err
}

type state int

const (
	stateDefault state = iota
	// stateNew is the state when the user is creating a new instance.
	stateNew
	// statePrompt is the state when the user is entering a prompt.
	statePrompt
	// stateHelp is the state when a help screen is displayed.
	stateHelp
	// stateConfirm is the state when a confirmation modal is displayed.
	stateConfirm
	// stateSelectWorkspace is the state while the workspace picker overlay is open.
	stateSelectWorkspace
	// stateFocus is the state when keystrokes are forwarded to the selected session.
	stateFocus
)

// workspacePickerMode distinguishes what Enter does in stateSelectWorkspace.
type workspacePickerMode int

const (
	// workspacePickerCreate resolves the selection and creates a new session in it.
	workspacePickerCreate workspacePickerMode = iota
	// workspacePickerSwitch just sets the active workspace and returns to stateDefault.
	workspacePickerSwitch
)

type home struct {
	ctx context.Context

	// -- Storage and Configuration --

	program string
	autoYes bool

	// storage is the interface for saving/loading data to/from the app's state
	storage *session.Storage
	// appConfig stores persistent application configuration
	appConfig *config.Config
	// appState stores persistent application state like seen help screens
	appState config.AppState

	// -- State --

	// state is the current discrete state of the application
	state state
	// newInstanceFinalizer is called when the state is stateNew and then you press enter.
	// It registers the new instance in the list after the instance has been started.
	newInstanceFinalizer func()

	// promptAfterName tracks if we should enter prompt mode after naming
	promptAfterName bool

	// keySent is used to manage underlining menu items
	keySent bool

	// instanceStarting is true while a background instance start is in progress.
	// Prevents double-submission and guards against interacting with a not-yet-started instance.
	instanceStarting bool
	// startingInstance holds a reference to the instance being started in the background.
	startingInstance *session.Instance

	// workspaceID is the active workspace; new instances inherit it.
	workspaceID string

	// launchDir is the directory cs was invoked from, used to resolve relative
	// paths typed into the workspace picker.
	launchDir string

	// discoveredRepos memoizes the filesystem discovery scan so reopening the
	// picker doesn't re-walk the filesystem.
	discoveredRepos       []session.DiscoveredRepo
	discoveredReposLoaded bool

	// workspacePickerMode records what Enter does while stateSelectWorkspace is active.
	workspacePickerMode workspacePickerMode

	// -- UI Components --

	// list displays the list of instances
	list *ui.List
	// menu displays the bottom menu
	menu *ui.Menu
	// tabbedWindow displays the tabbed window with preview and diff panes
	tabbedWindow *ui.TabbedWindow
	// errBox displays error messages
	errBox *ui.ErrBox
	// global spinner instance. we plumb this down to where it's needed
	spinner spinner.Model
	// textInputOverlay handles text input with state
	textInputOverlay *overlay.TextInputOverlay
	// textOverlay displays text information
	textOverlay *overlay.TextOverlay
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay
	// workspacePickerOverlay handles the type-to-filter workspace/path picker
	workspacePickerOverlay *overlay.WorkspacePickerOverlay

	metadataTick uint64

	// focusTarget is where focus-mode keystrokes and wheel scrolls go: the
	// selected instance's pane, or the terminal pane's session. Set on
	// enterFocusMode, cleared on exitFocusMode.
	focusTarget focusTarget

	// previewOriginX is the screen column where the tabbed window block starts,
	// measured in View so mouse selection maps window-relative coordinates back
	// to preview content cells.
	previewOriginX int
	// totalWidth is the last known terminal width, used to center-correct
	// previewOriginX.
	totalWidth int
}

// sessionPath returns the git-repo path a new session should be rooted at.
// When an active workspace is set we use its registered RepoPath (so cs-edge
// can be launched anywhere — cwd doesn't have to be the repo). When no
// workspace is active, we fall back to "." (used only for stale branch
// searches; new sessions now resolve their path from the workspace picker).
func (m *home) sessionPath() string {
	if m.workspaceID == "" {
		return "."
	}
	reg := config.LoadWorkspaceRegistry()
	if ws := reg.Get(m.workspaceID); ws != nil && ws.RepoPath != "" {
		return ws.RepoPath
	}
	return "."
}

// resolveWorkspaceProgram returns the program command and profile name a new
// session in the active workspace should use. If the active workspace defines
// any profiles, the first one is treated as the workspace default; otherwise
// the caller's launch-time program (m.program) is used and the profile name
// is empty.
func (m *home) resolveWorkspaceProgram() (program, profileName string) {
	program = m.program
	if m.workspaceID == "" {
		return
	}
	reg := config.LoadWorkspaceRegistry()
	ws := reg.Get(m.workspaceID)
	if ws == nil || len(ws.Profiles) == 0 {
		return
	}
	return ws.Profiles[0].Program, ws.Profiles[0].Name
}

// applyWorkspaceFocus sets the active workspace — where new sessions land. It
// deliberately does not touch the view filter: picking a workspace used to
// narrow the list with no way to widen it again, which stranded every other
// session out of sight. Narrowing is V's job.
func (m *home) applyWorkspaceFocus(id string) {
	m.workspaceID = id
	reg := config.LoadWorkspaceRegistry()
	if ws := reg.Get(id); ws != nil {
		m.list.SetActiveWorkspace(ws.DisplayName)
		m.list.SetActiveWorkspaceID(ws.ID)
	} else {
		m.list.SetActiveWorkspace("(unknown workspace)")
		m.list.SetActiveWorkspaceID(id)
	}
}

// cycleWorkspace moves the view filter to the previous (delta -1) or next
// (delta +1) stop in the cycle All -> ws1 -> ws2 -> ... -> All, wrapping at
// both ends. Landing on a workspace also makes it the active workspace via
// applyWorkspaceFocus; landing on All only clears the filter.
func (m *home) cycleWorkspace(delta int) {
	ids := m.list.WorkspaceIDs()
	if len(ids) == 0 {
		return
	}

	// Position 0 is the All stop; positions 1..len(ids) are the workspaces.
	pos := 0
	for i, id := range ids {
		if id == m.list.GetViewFilter() {
			pos = i + 1
			break
		}
	}

	n := len(ids) + 1
	pos = ((pos+delta)%n + n) % n

	if pos == 0 {
		m.list.SetViewFilter("")
		return
	}
	m.applyWorkspaceFocus(ids[pos-1])
	m.list.SetViewFilter(ids[pos-1])
}

// workspacePickerItems builds the picker rows from the registry, sorted
// most-recently-used first so an empty filter + Enter picks the most recent.
func (m *home) workspacePickerItems() []overlay.WorkspaceItem {
	reg := config.LoadWorkspaceRegistry()
	workspaces := make([]config.Workspace, len(reg.Workspaces))
	copy(workspaces, reg.Workspaces)
	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].LastUsedAt.After(workspaces[j].LastUsedAt)
	})

	items := make([]overlay.WorkspaceItem, 0, len(workspaces))
	seen := make(map[string]bool)
	for _, ws := range workspaces {
		items = append(items, overlay.WorkspaceItem{
			Label:    ws.DisplayName,
			Path:     ws.RepoPath,
			Kind:     overlay.KindRegistered,
			Existing: true,
		})
		seen[filepath.Clean(ws.RepoPath)] = true
	}

	for _, repo := range m.discoverRepos() {
		if seen[filepath.Clean(repo.Path)] {
			continue
		}
		items = append(items, overlay.WorkspaceItem{
			Label: repo.Name,
			Path:  repo.Path,
			Kind:  overlay.KindRepoRoot,
		})
	}
	return items
}

// discoverRepos runs (and memoizes) the filesystem discovery scan: the launch
// dir's roots at depth 1, plus the configured roots at the configured depth.
func (m *home) discoverRepos() []session.DiscoveredRepo {
	if m.discoveredReposLoaded {
		return m.discoveredRepos
	}
	m.discoveredReposLoaded = true

	discovery := m.appConfig.Discovery
	m.discoveredRepos = session.DiscoverAll(m.launchDir, session.DiscoverOptions{
		Roots: discovery.Roots,
		Depth: discovery.Depth,
		Skip:  discovery.Skip,
	})
	return m.discoveredRepos
}

// classifyWorkspacePath adapts session.ClassifyPath for the picker's
// PathClassifier callback.
func (m *home) classifyWorkspacePath(raw string) (overlay.ItemKind, string) {
	kind, note := session.ClassifyPath(raw, m.launchDir)
	switch kind {
	case session.KindRepoRoot:
		return overlay.KindRepoRoot, note
	case session.KindInsideRepo:
		return overlay.KindInsideRepo, note
	case session.KindNonGit:
		return overlay.KindNonGit, note
	case session.KindNotDir:
		return overlay.KindNotDir, note
	default:
		return overlay.KindMissing, note
	}
}

// openWorkspacePicker opens the workspace picker overlay in the given mode.
func (m *home) openWorkspacePicker(mode workspacePickerMode) tea.Cmd {
	m.workspacePickerMode = mode
	m.workspacePickerOverlay = overlay.NewWorkspacePickerOverlay(m.workspacePickerItems(), m.classifyWorkspacePath)
	m.state = stateSelectWorkspace
	return tea.WindowSize()
}

func newHome(ctx context.Context, program string, autoYes bool, workspaceID string, launchDir string) *home {
	// Load application config
	appConfig := config.LoadConfig()

	// Load application state
	appState := config.LoadState()

	// Initialize storage
	storage, err := session.NewStorage(appState)
	if err != nil {
		fmt.Printf("Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:       ui.NewErrBox(),
		storage:      storage,
		appConfig:    appConfig,
		program:      program,
		autoYes:      autoYes,
		state:        stateDefault,
		appState:     appState,
		workspaceID:  workspaceID,
		launchDir:    launchDir,
	}
	h.list = ui.NewList(&h.spinner, autoYes)

	// Seed the list with the active workspace's display name so the title bar
	// shows it from launch (and `W` to switch is discoverable in the menu).
	if workspaceID != "" {
		reg := config.LoadWorkspaceRegistry()
		if ws := reg.Get(workspaceID); ws != nil {
			h.list.SetActiveWorkspace(ws.DisplayName)
			h.list.SetActiveWorkspaceID(ws.ID)
		}
	}

	// Load saved instances
	instances, err := storage.LoadInstances()
	if err != nil {
		fmt.Printf("Failed to load instances: %v\n", err)
		os.Exit(1)
	}

	// Add loaded instances to the list
	for _, instance := range instances {
		// Call the finalizer immediately.
		h.list.AddInstance(instance)()
		if autoYes {
			instance.AutoYes = true
		}
	}

	return h
}

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	// List takes 30% of width, preview takes 70%
	listWidth := int(float32(msg.Width) * 0.3)
	m.totalWidth = msg.Width
	tabsWidth := msg.Width - listWidth

	// Menu takes 10% of height, list and window take 90%
	contentHeight := int(float32(msg.Height) * 0.9)
	menuHeight := msg.Height - contentHeight - 1     // minus 1 for error box
	m.errBox.SetSize(int(float32(msg.Width)*0.9), 1) // error box takes 1 row

	m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	m.list.SetSize(listWidth, contentHeight)

	if m.textInputOverlay != nil {
		m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}
	if m.textOverlay != nil {
		m.textOverlay.SetWidth(int(float32(msg.Width) * 0.6))
	}
	if m.workspacePickerOverlay != nil {
		m.workspacePickerOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}

	previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
	if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
		log.ErrorLog.Print(err)
	}
	m.menu.SetSize(msg.Width, menuHeight)
}

func (m *home) Init() tea.Cmd {
	// Upon starting, we want to start the spinner. Whenever we get a spinner.TickMsg, we
	// update the spinner, which sends a new spinner.TickMsg. I think this lasts forever lol.
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return previewTickMsg{}
		},
		tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance(), m.metadataTick),
	)
}

func (m *home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hideErrMsg:
		m.errBox.Clear()
	case previewTickMsg:
		cmd := m.instanceChanged()
		return m, tea.Batch(
			cmd,
			func() tea.Msg {
				time.Sleep(100 * time.Millisecond)
				return previewTickMsg{}
			},
		)
	case keyupMsg:
		m.menu.ClearKeydown()
		return m, nil
	case instanceStartDoneMsg:
		m.instanceStarting = false
		inst := msg.instance
		m.startingInstance = nil

		if msg.err != nil {
			// Start failed — remove the instance from the list and show the error.
			m.list.Kill()
			return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), m.handleError(msg.err))
		}

		// Save after successful start.
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}

		if m.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = overlay.NewTextInputOverlay("Enter prompt", "")
			m.promptAfterName = false
		} else {
			m.showHelpScreen(helpStart(inst), nil)
		}

		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case metadataUpdateDoneMsg:
		for _, r := range msg.results {
			// Skip instances that were paused while metadata was being computed
			if r.instance.Status == session.Paused {
				continue
			}
			if r.updated {
				r.instance.SetStatus(session.Running)
			} else if r.hasPrompt {
				r.instance.TapEnter()
			} else {
				r.instance.SetStatus(session.Ready)
			}
			if !r.diffComputed {
				// Throttled tick for a non-selected instance: leave its last diff stats in place.
				continue
			}
			if r.diffStats != nil && r.diffStats.Error != nil {
				if !strings.Contains(r.diffStats.Error.Error(), "base commit SHA not set") {
					log.WarningLog.Printf("could not update diff stats: %v", r.diffStats.Error)
				}
				r.instance.SetDiffStats(nil)
			} else {
				r.instance.SetDiffStats(r.diffStats)
			}
		}
		m.metadataTick++
		return m, tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance(), m.metadataTick)
	case tea.MouseMsg:
		// Handle mouse wheel events for scrolling the diff/preview pane
		if msg.Action == tea.MouseActionPress &&
			(msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelUp) {
			selected := m.list.GetSelectedInstance()
			if selected == nil || selected.Status == session.Paused {
				return m, nil
			}

			// In focus mode, forward the wheel to the focus target (like when
			// attached): alt-screen apps such as Claude Code scroll their own
			// transcript — tmux keeps no scrollback for them. The preview
			// mirrors the result.
			if m.state == stateFocus && m.focusTarget != nil && m.focusTarget.Alive() {
				if err := m.focusTarget.SendWheel(msg.Button == tea.MouseButtonWheelUp); err != nil {
					return m, tea.Batch(m.exitFocusMode(), m.handleError(err))
				}
				return m, nil
			}

			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.tabbedWindow.ScrollUp()
			case tea.MouseButtonWheelDown:
				m.tabbedWindow.ScrollDown()
			}
			return m, nil
		}

		// Left-drag in the preview: squad-side selection restricted to the pane
		// content, copied to the clipboard on release.
		if m.state == stateDefault || m.state == stateFocus {
			relX := msg.X - m.previewOriginX
			relY := msg.Y - 1 // top padding above the tabbed window
			switch {
			case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
				if row, col, ok := m.tabbedWindow.PreviewCellAt(relX, relY); ok {
					m.tabbedWindow.SelectionStart(row, col)
				}
			case msg.Action == tea.MouseActionMotion && m.tabbedWindow.SelectionActive():
				row, col := m.tabbedWindow.PreviewCellClamped(relX, relY)
				m.tabbedWindow.SelectionDrag(row, col)
			case msg.Action == tea.MouseActionRelease && m.tabbedWindow.SelectionActive():
				row, col := m.tabbedWindow.PreviewCellClamped(relX, relY)
				m.tabbedWindow.SelectionDrag(row, col)
				if text := m.tabbedWindow.SelectionFinish(); text != "" {
					if err := clipboard.WriteAll(text); err != nil {
						// No clipboard on a headless box; don't pop an error
						// box on every drag-release.
						log.WarningLog.Printf("could not copy selection to clipboard: %v", err)
						return m, nil
					}
					lineCount := strings.Count(text, "\n") + 1
					return m, m.showInfo(fmt.Sprintf("copied selection (%d lines)", lineCount))
				}
			}
		}
		return m, nil
	case branchSearchDebounceMsg:
		// Debounce timer fired — check if this is still the current filter version
		if m.textInputOverlay == nil {
			return m, nil
		}
		if msg.version != m.textInputOverlay.BranchFilterVersion() {
			return m, nil // stale, a newer debounce is pending
		}
		return m, m.runBranchSearch(msg.filter, msg.version)
	case branchSearchResultMsg:
		if m.textInputOverlay != nil {
			m.textInputOverlay.SetBranchResults(msg.branches, msg.version)
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.updateHandleWindowSizeEvent(msg)
		return m, nil
	case error:
		// Handle errors from confirmation actions
		return m, m.handleError(msg)
	case instanceChangedMsg:
		// Handle instance changed after confirmation action
		return m, m.instanceChanged()
	case instanceStartedMsg:
		// Select the instance that just started (or failed)
		m.list.SelectInstance(msg.instance)

		if msg.err != nil {
			m.list.Kill()
			return m, tea.Batch(m.handleError(msg.err), m.instanceChanged())
		}

		// Save after successful start
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}
		if m.autoYes {
			msg.instance.AutoYes = true
		}

		if msg.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = m.newPromptOverlay()
		} else {
			// If instance has a prompt (set from Shift+N flow), send it now
			if msg.instance.Prompt != "" {
				if err := msg.instance.SendPrompt(msg.instance.Prompt); err != nil {
					log.ErrorLog.Printf("failed to send prompt: %v", err)
				}
				msg.instance.Prompt = ""
			}
			m.menu.SetState(ui.StateDefault)
			m.showHelpScreen(helpStart(msg.instance), nil)
		}

		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *home) handleQuit() (tea.Model, tea.Cmd) {
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, m.handleError(err)
	}
	return m, tea.Quit
}

func (m *home) handleMenuHighlighting(msg tea.KeyMsg) (cmd tea.Cmd, returnEarly bool) {
	// Handle menu highlighting when you press a button. We intercept it here and immediately return to
	// update the ui while re-sending the keypress. Then, on the next call to this, we actually handle the keypress.
	if m.keySent {
		m.keySent = false
		return nil, false
	}
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm || m.state == stateSelectWorkspace {
		return nil, false
	}
	// If it's in the global keymap, we should try to highlight it.
	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return nil, false
	}

	if m.list.GetSelectedInstance() != nil && m.list.GetSelectedInstance().Paused() && (name == keys.KeyEnter || name == keys.KeyAttach) {
		return nil, false
	}
	if name == keys.KeyShiftDown || name == keys.KeyShiftUp {
		return nil, false
	}

	// Skip the menu highlighting if the key is not in the map or we are using the shift up and down keys.
	// TODO: cleanup: when you press enter on stateNew, we use keys.KeySubmitName. We should unify the keymap.
	if name == keys.KeyEnter && m.state == stateNew {
		name = keys.KeySubmitName
	}
	m.keySent = true
	return tea.Batch(
		func() tea.Msg { return msg },
		m.keydownCallback(name)), true
}

func (m *home) handleKeyPress(msg tea.KeyMsg) (mod tea.Model, cmd tea.Cmd) {
	// Typing cancels any in-progress mouse selection. This also heals a stuck
	// selection whose mouse release never reached the app (e.g. a window
	// switch mid-click), which would otherwise freeze the preview content.
	if m.tabbedWindow != nil && m.tabbedWindow.SelectionActive() {
		m.tabbedWindow.SelectionCancel()
	}

	// In focus mode every keystroke is forwarded to the session's PTY.
	if m.state == stateFocus {
		return m.handleFocusState(msg)
	}

	cmd, returnEarly := m.handleMenuHighlighting(msg)
	if returnEarly {
		return m, cmd
	}

	if m.state == stateHelp {
		return m.handleHelpState(msg)
	}

	if m.state == stateNew {
		// Handle quit commands first. Don't handle q because the user might want to type that.
		if msg.String() == "ctrl+c" {
			m.state = stateDefault
			m.promptAfterName = false
			m.list.Kill()
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		}

		instance := m.list.GetInstances()[m.list.NumInstances()-1]
		switch msg.Type {
		// Start the instance (enable previews etc) and go back to the main menu state.
		case tea.KeyEnter:
			if len(instance.Title) == 0 {
				return m, m.handleError(fmt.Errorf("title cannot be empty"))
			}

			// If promptAfterName, show prompt+branch overlay before starting
			if m.promptAfterName {
				m.promptAfterName = false
				m.state = statePrompt
				m.menu.SetState(ui.StatePrompt)
				m.textInputOverlay = m.newPromptOverlay()
				// Trigger initial branch search (no debounce, version 0)
				initialSearch := m.runBranchSearch("", m.textInputOverlay.BranchFilterVersion())
				return m, tea.Batch(tea.WindowSize(), initialSearch)
			}

			// Set Loading status and finalize into the list immediately
			instance.SetStatus(session.Loading)
			m.newInstanceFinalizer()
			m.promptAfterName = false
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)

			// Return a tea.Cmd that runs instance.Start in the background
			startCmd := func() tea.Msg {
				err := instance.Start(true)
				return instanceStartedMsg{
					instance:        instance,
					err:             err,
					promptAfterName: false,
				}
			}

			return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
		case tea.KeyRunes:
			if runewidth.StringWidth(instance.Title) >= 32 {
				return m, m.handleError(fmt.Errorf("title cannot be longer than 32 characters"))
			}
			if err := instance.SetTitle(instance.Title + string(msg.Runes)); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyBackspace:
			runes := []rune(instance.Title)
			if len(runes) == 0 {
				return m, nil
			}
			if err := instance.SetTitle(string(runes[:len(runes)-1])); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeySpace:
			if err := instance.SetTitle(instance.Title + " "); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyEsc:
			m.list.Kill()
			m.state = stateDefault
			m.instanceChanged()

			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		default:
		}
		return m, nil
	} else if m.state == statePrompt {
		// Handle cancel via ctrl+c before delegating to the overlay
		if msg.String() == "ctrl+c" {
			return m, m.cancelPromptOverlay()
		}

		// Use the new TextInputOverlay component to handle all key events
		shouldClose, branchFilterChanged := m.textInputOverlay.HandleKeyPress(msg)

		// Check if the form was submitted or canceled
		if shouldClose {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}

			if m.textInputOverlay.IsCanceled() {
				return m, m.cancelPromptOverlay()
			}

			if m.textInputOverlay.IsSubmitted() {
				prompt := m.textInputOverlay.GetValue()
				selectedBranch := m.textInputOverlay.GetSelectedBranch()
				selectedProgram := m.textInputOverlay.GetSelectedProgram()

				if !selected.Started() {
					// Shift+N flow: instance not started yet — set branch, start, then send prompt
					if selectedBranch != "" {
						selected.SetSelectedBranch(selectedBranch)
					}
					if selectedProgram != "" {
						selected.Program = selectedProgram
					}
					selected.Prompt = prompt

					// Finalize into list and start
					selected.SetStatus(session.Loading)
					m.newInstanceFinalizer()
					m.textInputOverlay = nil
					m.state = stateDefault
					m.menu.SetState(ui.StateDefault)

					startCmd := func() tea.Msg {
						err := selected.Start(true)
						return instanceStartedMsg{
							instance:        selected,
							err:             err,
							promptAfterName: false,
							selectedBranch:  selectedBranch,
						}
					}

					return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
				}

				// Regular flow: instance already running, just send prompt
				if err := selected.SendPrompt(prompt); err != nil {
					return m, m.handleError(err)
				}
			}

			// Close the overlay and reset state
			m.textInputOverlay = nil
			m.state = stateDefault
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					m.showHelpScreen(helpStart(selected), nil)
					return nil
				},
			)
		}

		// Schedule a debounced branch search if the filter changed
		if branchFilterChanged {
			filter := m.textInputOverlay.BranchFilter()
			version := m.textInputOverlay.BranchFilterVersion()
			return m, m.scheduleBranchSearch(filter, version)
		}

		return m, nil
	} else if m.state == stateSelectWorkspace {
		return m.handleWorkspacePickerState(msg)
	}

	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.state = stateDefault
			m.confirmationOverlay = nil
			return m, nil
		}
		return m, nil
	}

	// Exit scrolling mode when ESC is pressed and preview pane is in scrolling mode
	// Check if Escape key was pressed and we're not in the diff tab (meaning we're in preview tab)
	// Always check for escape key first to ensure it doesn't get intercepted elsewhere
	if msg.Type == tea.KeyEsc {
		// If in preview tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInPreviewTab() && m.tabbedWindow.IsPreviewInScrollMode() {
			// Use the selected instance from the list
			selected := m.list.GetSelectedInstance()
			err := m.tabbedWindow.ResetPreviewToNormalMode(selected)
			if err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		// If in terminal tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsTerminalInScrollMode() {
			m.tabbedWindow.ResetTerminalToNormalMode()
			return m, m.instanceChanged()
		}
	}

	// Handle quit commands first
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m.handleQuit()
	}

	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return m, nil
	}

	switch name {
	case keys.KeyHelp:
		return m.showHelpScreen(helpTypeGeneral{}, nil)
	case keys.KeyPrompt:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}
		m.promptAfterName = true
		return m, m.openWorkspacePicker(workspacePickerCreate)
	case keys.KeyNew:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}
		m.promptAfterName = false
		return m, m.openWorkspacePicker(workspacePickerCreate)
	case keys.KeyUp:
		m.list.Up()
		return m, m.instanceChanged()
	case keys.KeyDown:
		m.list.Down()
		return m, m.instanceChanged()
	case keys.KeyShiftUp:
		m.tabbedWindow.ScrollUp()
		return m, m.instanceChanged()
	case keys.KeyShiftDown:
		m.tabbedWindow.ScrollDown()
		return m, m.instanceChanged()
	case keys.KeyTab:
		m.tabbedWindow.Toggle()
		m.menu.SetActiveTab(m.tabbedWindow.GetActiveTab())
		return m, m.instanceChanged()
	case keys.KeyKill:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the kill action as a tea.Cmd
		killAction := func() tea.Msg {
			// Get worktree and check if branch is checked out
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}

			checkedOut, err := worktree.IsBranchCheckedOut()
			if err != nil {
				return err
			}

			if checkedOut {
				return fmt.Errorf("instance %s is currently checked out", selected.Title)
			}

			// Clean up terminal session for this instance
			m.tabbedWindow.CleanupTerminalForInstance(selected.Title)

			// Delete from storage first
			if err := m.storage.DeleteInstance(selected.Title); err != nil {
				return err
			}

			// Then kill the instance
			m.list.Kill()
			return instanceChangedMsg{}
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
		return m, m.confirmAction(message, killAction)
	case keys.KeySubmit:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if selected.InPlace {
			return m, m.handleError(fmt.Errorf("push is unavailable for in-place sessions — commit from your own shell"))
		}

		// Create the push action as a tea.Cmd
		pushAction := func() tea.Msg {
			// Default commit message with timestamp
			commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s", selected.Title, time.Now().Format(time.RFC822))
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}
			if err = worktree.PushChanges(commitMsg, true); err != nil {
				return err
			}
			return nil
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Push changes from session '%s'?", selected.Title)
		return m, m.confirmAction(message, pushAction)
	case keys.KeyCheckout:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if selected.InPlace {
			return m, m.handleError(fmt.Errorf("checkout is unavailable for in-place sessions — commit from your own shell"))
		}

		// Show help screen before pausing
		m.showHelpScreen(helpTypeInstanceCheckout{}, func() {
			if err := selected.Pause(); err != nil {
				m.handleError(err)
			}
			m.tabbedWindow.CleanupTerminalForInstance(selected.Title)
			m.instanceChanged()
		})
		return m, nil
	case keys.KeyMoveUp:
		if m.list.MoveUp() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyMoveDown:
		if m.list.MoveDown() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyResume:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if err := selected.Resume(); err != nil {
			return m, m.handleError(err)
		}
		return m, tea.WindowSize()
	case keys.KeySwitchWorkspace:
		return m, m.openWorkspacePicker(workspacePickerSwitch)
	case keys.KeyCollapseWorkspace:
		m.list.ToggleCollapseCurrent()
		return m, nil
	case keys.KeyViewFilter:
		if m.list.GetViewFilter() != "" {
			m.list.SetViewFilter("")
			return m, nil
		}
		if m.workspaceID == "" {
			return m, m.handleError(fmt.Errorf("no active workspace to filter to — press W to pick one"))
		}
		m.list.SetViewFilter(m.workspaceID)
		return m, nil
	case keys.KeyPrevWorkspace:
		m.cycleWorkspace(-1)
		return m, nil
	case keys.KeyNextWorkspace:
		m.cycleWorkspace(1)
		return m, nil
	case keys.KeyFocus:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
			return m, nil
		}
		if !m.tabbedWindow.IsInPreviewTab() && !m.tabbedWindow.IsInTerminalTab() {
			return m, nil
		}
		return m.enterFocusMode(selected)
	case keys.KeyEnter:
		selected := m.attachableSelection()
		if selected == nil {
			return m, nil
		}
		// Enter types into the preview/terminal rather than taking over the
		// terminal; o still attaches.
		if m.tabbedWindow.IsInPreviewTab() || m.tabbedWindow.IsInTerminalTab() {
			return m.enterFocusMode(selected)
		}
		return m.attachSelected()
	case keys.KeyAttach:
		if m.attachableSelection() == nil {
			return m, nil
		}
		return m.attachSelected()
	default:
		return m, nil
	}
}

// attachableSelection returns the selected instance when it is in a state that
// can receive keystrokes, or nil.
func (m *home) attachableSelection() *session.Instance {
	if m.list.NumInstances() == 0 {
		return nil
	}
	selected := m.list.GetSelectedInstance()
	if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
		return nil
	}
	return selected
}

func (m *home) attachSelected() (tea.Model, tea.Cmd) {
	if m.tabbedWindow.IsInTerminalTab() {
		m.showHelpScreen(helpTypeInstanceAttach{}, func() {
			ch, err := m.tabbedWindow.AttachTerminal()
			if err != nil {
				m.handleError(err)
				return
			}
			<-ch
			m.state = stateDefault
		})
		return m, nil
	}
	m.showHelpScreen(helpTypeInstanceAttach{}, func() {
		ch, err := m.list.Attach()
		if err != nil {
			m.handleError(err)
			return
		}
		<-ch
		m.state = stateDefault
		m.instanceChanged()
	})
	return m, nil
}

// enterFocusMode switches to focus mode: keystrokes go to the selected
// session's pane, or to the terminal pane's session if the terminal tab is
// active.
func (m *home) enterFocusMode(selected *session.Instance) (tea.Model, tea.Cmd) {
	m.state = stateFocus
	m.menu.SetState(ui.StateFocus)
	m.tabbedWindow.SetFocused(true)
	m.tabbedWindow.SetShowCursor(true)

	if m.tabbedWindow.IsInTerminalTab() {
		// The terminal tab lazily creates its session; make sure it exists
		// before handing it focus.
		if err := m.tabbedWindow.EnsureTerminalSession(selected); err != nil {
			return m, m.handleError(err)
		}
		if m.tabbedWindow.IsTerminalInScrollMode() {
			m.tabbedWindow.ResetTerminalToNormalMode()
		}
		m.focusTarget = m.tabbedWindow.Terminal()
	} else {
		// Snap the preview to the live view so keystrokes are visible.
		if m.tabbedWindow.IsPreviewInScrollMode() {
			if err := m.tabbedWindow.ResetPreviewToNormalMode(selected); err != nil {
				log.ErrorLog.Print(err)
			}
		}
		m.focusTarget = instanceFocusTarget{selected}
	}
	return m, m.instanceChanged()
}

// exitFocusMode returns from focus mode to the default list navigation.
func (m *home) exitFocusMode() tea.Cmd {
	m.state = stateDefault
	m.menu.SetState(ui.StateDefault)
	m.tabbedWindow.SetFocused(false)
	m.tabbedWindow.SetShowCursor(false)
	m.focusTarget = nil
	return m.instanceChanged()
}

// handleFocusState forwards keystrokes to the focus target while in focus
// mode. Exit is ctrl+q or ctrl+space; esc is deliberately forwarded (agents
// use it to interrupt), so it is not an exit key.
func (m *home) handleFocusState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+space arrives as ctrl+@ (NUL). ctrl+f mirrors the f that enters
	// focus mode; ctrl+q and ctrl+space stay so existing muscle memory works.
	if msg.String() == "ctrl+q" || msg.String() == "ctrl+@" || msg.String() == "ctrl+f" {
		return m, m.exitFocusMode()
	}

	selected := m.list.GetSelectedInstance()
	if selected == nil || selected.Paused() || m.focusTarget == nil || !m.focusTarget.Alive() {
		return m, m.exitFocusMode()
	}

	// shift+up/down scroll like the wheel: forwarded to the session so Claude
	// Code scrolls its own transcript (alt-screen apps have no tmux scrollback
	// to snapshot).
	if msg.String() == "shift+up" || msg.String() == "shift+down" {
		if err := m.focusTarget.SendWheel(msg.String() == "shift+up"); err != nil {
			return m, tea.Batch(m.exitFocusMode(), m.handleError(err))
		}
		return m, m.instanceChanged()
	}

	data := keyMsgToBytes(msg)
	if len(data) == 0 {
		return m, nil
	}
	if err := m.focusTarget.SendKeys(string(data)); err != nil {
		return m, tea.Batch(m.exitFocusMode(), m.handleError(err))
	}
	return m, m.instanceChanged()
}

// instanceChanged updates the preview pane, menu, and diff pane based on the selected instance. It returns an error
// Cmd if there was any error.
func (m *home) instanceChanged() tea.Cmd {
	// selected may be nil
	selected := m.list.GetSelectedInstance()

	m.tabbedWindow.UpdateDiff(selected)
	m.tabbedWindow.SetInstance(selected)
	// Update menu with current instance
	m.menu.SetInstance(selected)

	// If there's no selected instance, we don't need to update the preview.
	if err := m.tabbedWindow.UpdatePreview(selected); err != nil {
		return m.handleError(err)
	}
	if err := m.tabbedWindow.UpdateTerminal(selected); err != nil {
		return m.handleError(err)
	}
	return nil
}

type keyupMsg struct{}

// keydownCallback clears the menu option highlighting after 500ms.
func (m *home) keydownCallback(name keys.KeyName) tea.Cmd {
	m.menu.Keydown(name)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}

		return keyupMsg{}
	}
}

// hideErrMsg implements tea.Msg and clears the error text from the screen.
type hideErrMsg struct{}

// previewTickMsg implements tea.Msg and triggers a preview update
type previewTickMsg struct{}

type instanceChangedMsg struct{}

type instanceStartedMsg struct {
	instance        *session.Instance
	err             error
	promptAfterName bool
	selectedBranch  string
}

// branchSearchDebounceMsg fires after the debounce interval to trigger a search.
type branchSearchDebounceMsg struct {
	filter  string
	version uint64
}

// branchSearchResultMsg carries search results back to Update.
type branchSearchResultMsg struct {
	branches []string
	version  uint64
}

const branchSearchDebounce = 150 * time.Millisecond

// scheduleBranchSearch returns a debounced tea.Cmd: sleeps, then triggers a search message.
func (m *home) scheduleBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(branchSearchDebounce)
		return branchSearchDebounceMsg{filter: filter, version: version}
	}
}

// runBranchSearch returns a tea.Cmd that performs the git search in the background.
func (m *home) runBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		branches, err := git.SearchBranches(m.sessionPath(), filter)
		if err != nil {
			log.WarningLog.Printf("branch search failed: %v", err)
			return nil
		}
		return branchSearchResultMsg{branches: branches, version: version}
	}
}

// instanceMetaResult holds the results of a single instance's metadata update,
// computed in a background goroutine.
type instanceMetaResult struct {
	instance  *session.Instance
	updated   bool
	hasPrompt bool
	diffStats *git.DiffStats
	// False on a throttled tick, so the consumer leaves the last stats in place instead of clearing them.
	diffComputed bool
}

// metadataUpdateDoneMsg is sent when the background metadata update completes.
type metadataUpdateDoneMsg struct {
	results []instanceMetaResult
}

// instanceStartDoneMsg is sent when the background instance start completes.
type instanceStartDoneMsg struct {
	instance *session.Instance
	err      error
}

// runInstanceStartCmd returns a Cmd that performs the expensive instance.Start(true)
// in a background goroutine so the main event loop stays responsive.
func runInstanceStartCmd(instance *session.Instance) tea.Cmd {
	return func() tea.Msg {
		err := instance.Start(true)
		return instanceStartDoneMsg{instance: instance, err: err}
	}
}

// snapshotActiveInstances returns the currently active (started, not paused)
// instances. Called on the main thread so the filtering doesn't race with
// state mutations.
func (m *home) snapshotActiveInstances() []*session.Instance {
	var out []*session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Started() && !inst.Paused() {
			out = append(out, inst)
		}
	}
	return out
}

// Diff and DiffNumstat both do a `git add -N .` index write, so at high instance counts the diff is throttled while the cheap tmux capture is not.
const metadataDiffThrottle = 8

// tickUpdateMetadataCmd returns a self-chaining Cmd that sleeps 500ms, then performs
// expensive metadata I/O (tmux capture, git diff) in parallel background goroutines.
// Because it only re-schedules after completing, overlapping ticks are impossible.
// The active instances slice should be snapshotted on the main thread via
// snapshotActiveInstances() before being passed here.
func tickUpdateMetadataCmd(active []*session.Instance, selected *session.Instance, tick uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)

		if len(active) == 0 {
			return metadataUpdateDoneMsg{}
		}

		results := make([]instanceMetaResult, len(active))
		var wg sync.WaitGroup
		for idx, inst := range active {
			wg.Add(1)
			go func(i int, instance *session.Instance) {
				defer wg.Done()
				r := &results[i]
				r.instance = instance
				r.updated, r.hasPrompt = instance.HasUpdated()
				if instance == selected {
					r.diffStats = instance.ComputeDiff()
					r.diffComputed = true
				} else if tick%metadataDiffThrottle == 0 {
					r.diffStats = instance.ComputeDiffNumstat()
					r.diffComputed = true
				}
			}(idx, inst)
		}
		wg.Wait()

		return metadataUpdateDoneMsg{results: results}
	}
}

// handleError handles all errors which get bubbled up to the app. sets the error message. We return a callback tea.Cmd that returns a hideErrMsg message
// which clears the error message after 3 seconds.
func (m *home) handleError(err error) tea.Cmd {
	log.ErrorLog.Printf("%v", err)
	m.errBox.SetError(err)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(3 * time.Second):
		}

		return hideErrMsg{}
	}
}

// showInfo displays a neutral status message that clears itself after 2 seconds.
func (m *home) showInfo(text string) tea.Cmd {
	m.errBox.SetInfo(text)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(2 * time.Second):
		}
		return hideErrMsg{}
	}
}

func (m *home) newPromptOverlay() *overlay.TextInputOverlay {
	return overlay.NewTextInputOverlayWithBranchPicker("Enter prompt", "", m.appConfig.GetProfiles())
}

// cancelPromptOverlay cancels the prompt overlay, cleaning up unstarted instances.
func (m *home) cancelPromptOverlay() tea.Cmd {
	selected := m.list.GetSelectedInstance()
	if selected != nil && !selected.Started() {
		m.list.Kill()
	}
	m.textInputOverlay = nil
	m.state = stateDefault
	return tea.Sequence(
		tea.WindowSize(),
		func() tea.Msg {
			m.menu.SetState(ui.StateDefault)
			return nil
		},
	)
}

// handleWorkspacePickerState handles key events while stateSelectWorkspace is active.
func (m *home) handleWorkspacePickerState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.workspacePickerOverlay = nil
		m.promptAfterName = false
		m.state = stateDefault
		return m, tea.WindowSize()
	}

	if msg.Type == tea.KeyEnter {
		item, ok := m.workspacePickerOverlay.Selected()
		if !ok {
			return m, nil
		}

		target, err := session.ResolveSessionTarget(item.Path, m.launchDir)
		if err != nil {
			return m, m.handleError(err)
		}
		if target.Mode == session.ModeInPlace {
			return m.enterInPlaceTarget(target)
		}

		reg := config.LoadWorkspaceRegistry()
		ws, err := reg.EnsureWorkspace(target.Dir, git.FirstRemoteURL(target.Dir))
		if err != nil {
			return m, m.handleError(err)
		}
		m.applyWorkspaceFocus(ws.ID)
		m.workspacePickerOverlay = nil

		if m.workspacePickerMode == workspacePickerSwitch {
			m.state = stateDefault
			return m, tea.WindowSize()
		}

		// Start a background fetch so branches are up to date by the time the prompt overlay's picker opens.
		fetchCmd := func() tea.Msg {
			git.FetchBranches(target.Dir)
			return nil
		}

		program, profileName := m.resolveWorkspaceProgram()
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:       "",
			Path:        target.Dir,
			Program:     program,
			WorkspaceID: m.workspaceID,
			ProfileName: profileName,
		})
		if err != nil {
			return m, m.handleError(err)
		}
		instance.SubDir = target.SubDir

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)

		return m, fetchCmd
	}

	m.workspacePickerOverlay.HandleKeyPress(msg)
	return m, nil
}

// enterInPlaceTarget creates a session for a resolved ModeInPlace target,
// refusing a second in-place session in the same directory and requiring
// confirmation when the target is exactly $HOME.
func (m *home) enterInPlaceTarget(target session.SessionTarget) (tea.Model, tea.Cmd) {
	if existing := m.findInPlaceInstance(target.Dir); existing != nil {
		return m, m.handleError(fmt.Errorf("an in-place session %q is already running in %s", existing.Title, target.Dir))
	}

	if isHomeDir(target.Dir) {
		message := "Start an agent in your home directory? It edits files in place across all of ~ with no worktree, no branch, and no diff to review."
		m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
		m.confirmationOverlay.SetWidth(50)
		m.confirmationOverlay.OnConfirm = func() {
			_ = m.createInPlaceInstance(target)
		}
		m.confirmationOverlay.OnCancel = func() {
			m.state = stateSelectWorkspace
		}
		m.state = stateConfirm
		return m, nil
	}

	return m, m.createInPlaceInstance(target)
}

// findInPlaceInstance returns the existing in-place instance rooted at dir, if
// any. Two in-place sessions in one directory have no isolation and would
// fight over the same files. Only this process's list is visible.
func (m *home) findInPlaceInstance(dir string) *session.Instance {
	for _, inst := range m.list.GetInstances() {
		if inst.InPlace && filepath.Clean(inst.Path) == dir {
			return inst
		}
	}
	return nil
}

// isHomeDir reports whether dir is exactly the user's home directory
// (canonical comparison, not a prefix match).
func isHomeDir(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	if canonical, err := filepath.EvalSymlinks(home); err == nil {
		home = canonical
	}
	return dir == home
}

// createInPlaceInstance registers target.Dir as a non-git workspace and
// creates an in-place session in it, mirroring the worktree creation path in
// handleWorkspacePickerState minus the branch prefetch (there are no branches).
func (m *home) createInPlaceInstance(target session.SessionTarget) tea.Cmd {
	reg := config.LoadWorkspaceRegistry()
	ws, err := reg.EnsureWorkspace(target.Dir, "")
	if err != nil {
		return m.handleError(err)
	}
	if !ws.NonGit {
		ws.NonGit = true
		if err := reg.Upsert(*ws); err != nil {
			return m.handleError(err)
		}
	}
	m.applyWorkspaceFocus(ws.ID)
	m.workspacePickerOverlay = nil

	if m.workspacePickerMode == workspacePickerSwitch {
		m.state = stateDefault
		return tea.WindowSize()
	}

	program, profileName := m.resolveWorkspaceProgram()
	instance, err := session.NewInstance(session.InstanceOptions{
		Title:       "",
		Path:        target.Dir,
		Program:     program,
		WorkspaceID: m.workspaceID,
		ProfileName: profileName,
		InPlace:     true,
	})
	if err != nil {
		return m.handleError(err)
	}

	m.newInstanceFinalizer = m.list.AddInstance(instance)
	m.list.SetSelectedInstance(m.list.NumInstances() - 1)
	m.state = stateNew
	m.menu.SetState(ui.StateNewInstance)
	return nil
}

// confirmAction shows a confirmation modal and stores the action to execute on confirm
func (m *home) confirmAction(message string, action tea.Cmd) tea.Cmd {
	m.state = stateConfirm

	// Create and show the confirmation overlay using ConfirmationOverlay
	m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	// Set a fixed width for consistent appearance
	m.confirmationOverlay.SetWidth(50)

	// Set callbacks for confirmation and cancellation
	m.confirmationOverlay.OnConfirm = func() {
		m.state = stateDefault
		// Execute the action if it exists
		if action != nil {
			_ = action()
		}
	}

	m.confirmationOverlay.OnCancel = func() {
		m.state = stateDefault
	}

	return nil
}

func (m *home) View() string {
	listWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.list.String())
	previewWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.tabbedWindow.String())
	listAndPreview := lipgloss.JoinHorizontal(lipgloss.Top, listWithPadding, previewWithPadding)

	// JoinVertical centers this block under the wider menu row; measure where
	// the tabbed window actually starts so mouse selection maps correctly.
	// centerPad/2 mirrors lipgloss's left pad; on odd-width terminals it may
	// be one column off, which at worst shifts a selection by a single cell.
	centerPad := m.totalWidth - lipgloss.Width(listAndPreview)
	if centerPad < 0 {
		centerPad = 0
	}
	m.previewOriginX = centerPad/2 + lipgloss.Width(listWithPadding)

	mainView := lipgloss.JoinVertical(
		lipgloss.Center,
		listAndPreview,
		m.menu.String(),
		m.errBox.String(),
	)

	if m.state == statePrompt {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
	} else if m.state == stateSelectWorkspace {
		if m.workspacePickerOverlay == nil {
			log.ErrorLog.Printf("workspace picker overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.workspacePickerOverlay.Render(), mainView, true, true)
	} else if m.state == stateHelp {
		if m.textOverlay == nil {
			log.ErrorLog.Printf("text overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	}

	return mainView
}
