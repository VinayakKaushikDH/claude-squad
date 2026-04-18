package app

import (
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/log"
	"claude-squad/notify"
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"claude-squad/session/jj"
	"claude-squad/session/vcs"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"errors"
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

const GlobalInstanceLimit = 10

// Run is the main entrypoint into the application.
func Run(ctx context.Context, program string, autoYes bool) error {
	p := tea.NewProgram(
		newHome(ctx, program, autoYes),
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
	// editingInstance holds a direct reference to the instance being named in
	// stateNew. This prevents background messages (metadataUpdateDoneMsg) from
	// changing which instance receives SetTitle() calls via list re-filtering.
	editingInstance *session.Instance

	// keySent is used to manage underlining menu items
	keySent bool

	// instanceStarting is true while a background instance start is in progress.
	// Prevents double-submission and guards against interacting with a not-yet-started instance.
	instanceStarting bool
	// startingInstance holds a reference to the instance being started in the background.
	startingInstance *session.Instance

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

	// -- Workspace State --

	workspaces      []Workspace
	activeWorkspace int
	workspaceTabBar *ui.WorkspaceTabBar // nil when <=1 workspace
	currentRepoPath string

	// -- Cross-process coordination --

	// reloadTickCounter counts metadata ticks; triggers disk reload every 10 ticks (~5s).
	reloadTickCounter int
	// locallyDeleted tracks instance IDs deleted in this process so they aren't
	// re-added on the next disk reload.
	locallyDeleted map[string]bool
}

func newHome(ctx context.Context, program string, autoYes bool) *home {
	appConfig := config.LoadConfig()
	appState := config.LoadState()

	storage, err := session.NewStorage(appState)
	if err != nil {
		fmt.Printf("Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	h := &home{
		ctx:            ctx,
		spinner:        spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:           ui.NewMenu(),
		tabbedWindow:   ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:         ui.NewErrBox(),
		storage:        storage,
		appConfig:      appConfig,
		program:        program,
		autoYes:        autoYes,
		state:          stateDefault,
		appState:       appState,
		locallyDeleted: make(map[string]bool),
	}
	h.list = ui.NewList(&h.spinner, autoYes)

	// Load saved instances
	instances, err := storage.LoadInstances()
	if err != nil {
		fmt.Printf("Failed to load instances: %v\n", err)
		os.Exit(1)
	}

	for _, instance := range instances {
		h.list.AddInstance(instance)() // call finalizer immediately
		if autoYes {
			instance.AutoYes = true
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		h.currentRepoPath, _ = filepath.Abs(cwd)
	}

	h.refreshWorkspaces()

	if len(h.workspaces) > 0 {
		h.activeWorkspace = FindWorkspaceIndex(h.workspaces, h.currentRepoPath)
		h.list.SetFilter(h.workspaces[h.activeWorkspace].Path)
	}

	if len(h.workspaces) >= 1 {
		h.workspaceTabBar = ui.NewWorkspaceTabBar()
		h.workspaceTabBar.SetWorkspaces(h.workspaceTabs())
		h.workspaceTabBar.SetActiveIdx(h.activeWorkspace)
		h.menu.SetHasMultipleWorkspaces(len(h.workspaces) >= 2)
	}

	return h
}

func (m *home) refreshWorkspaces() {
	activePath := ""
	if m.activeWorkspace < len(m.workspaces) {
		activePath = m.workspaces[m.activeWorkspace].Path
	}
	m.workspaces = DeriveWorkspaces(m.list.GetInstances(), activePath)
	// Ensure the current working directory always has a workspace entry,
	// even when no instances exist for it yet. This prevents a second cs
	// process from defaulting to showing another process's workspace.
	if m.currentRepoPath != "" {
		found := false
		for _, ws := range m.workspaces {
			if ws.Path == m.currentRepoPath {
				found = true
				break
			}
		}
		if !found {
			m.workspaces = append(m.workspaces, Workspace{
				Name: filepath.Base(m.currentRepoPath),
				Path: m.currentRepoPath,
			})
			// Re-sort to maintain alphabetical order.
			sort.Slice(m.workspaces, func(i, j int) bool {
				return m.workspaces[i].Name < m.workspaces[j].Name
			})
		}
	}
}

func (m *home) workspaceTabs() []ui.WorkspaceTab {
	tabs := make([]ui.WorkspaceTab, len(m.workspaces))
	for i, ws := range m.workspaces {
		tabs[i] = ui.WorkspaceTab{Name: ws.Name, Path: ws.Path, HasReady: ws.HasReady}
	}
	return tabs
}

// syncWorkspaceUI updates the tab bar and menu after workspace changes.
func (m *home) syncWorkspaceUI() {
	m.refreshWorkspaces()

	if len(m.workspaces) >= 1 {
		if m.workspaceTabBar == nil {
			m.workspaceTabBar = ui.NewWorkspaceTabBar()
		}
		if m.activeWorkspace >= len(m.workspaces) {
			m.activeWorkspace = len(m.workspaces) - 1
		}
		m.workspaceTabBar.SetWorkspaces(m.workspaceTabs())
		m.workspaceTabBar.SetActiveIdx(m.activeWorkspace)
		m.menu.SetHasMultipleWorkspaces(len(m.workspaces) >= 2)
	} else {
		m.workspaceTabBar = nil
		m.activeWorkspace = 0
		m.menu.SetHasMultipleWorkspaces(false)
		m.list.SetFilter("")
	}
}

// mergeReloadedInstances reconciles in-memory instances with freshly loaded disk data.
// Rules:
//  1. On disk + not in memory + not locallyDeleted → add
//  2. On disk + not in memory + locallyDeleted → skip
//  3. In memory + not on disk + Started() + not editingInstance → remove (no Kill)
//  4. In memory + not on disk + !Started() → keep (still being created)
//  5. Both exist → update metadata only (status, diff stats)
func (m *home) mergeReloadedInstances(diskData []session.InstanceData) {
	// Build lookup of disk instances by ID (primary) and title (fallback).
	diskByID := make(map[string]*session.InstanceData, len(diskData))
	diskByTitle := make(map[string]*session.InstanceData, len(diskData))
	for i := range diskData {
		d := &diskData[i]
		if d.ID != "" {
			diskByID[d.ID] = d
		}
		diskByTitle[d.Title] = d
	}

	// Build lookup of in-memory instances by ID and title.
	memInstances := m.list.GetInstances()
	memByID := make(map[string]*session.Instance, len(memInstances))
	memByTitle := make(map[string]*session.Instance, len(memInstances))
	for _, inst := range memInstances {
		if inst.ID != "" {
			memByID[inst.ID] = inst
		}
		memByTitle[inst.Title] = inst
	}

	// Rule 1 & 2: find disk instances not in memory.
	for i := range diskData {
		d := &diskData[i]
		found := false
		if d.ID != "" {
			_, found = memByID[d.ID]
		}
		if !found {
			_, found = memByTitle[d.Title]
		}
		if found {
			continue
		}
		// Rule 2: skip if locally deleted.
		if d.ID != "" && m.locallyDeleted[d.ID] {
			continue
		}
		if d.ID == "" && d.Title != "" && m.locallyDeleted["title:"+d.Title] {
			continue
		}
		// Rule 1: add new instance from disk.
		inst, err := session.FromInstanceData(*d)
		if err != nil {
			log.WarningLog.Printf("disk reload: skipping instance %s: %v", d.Title, err)
			continue
		}
		if m.autoYes {
			inst.AutoYes = true
		}
		m.list.AddInstance(inst)() // call finalizer immediately
	}

	// Rule 3 & 4: find in-memory instances not on disk.
	var toRemove []string
	for _, inst := range memInstances {
		found := false
		if inst.ID != "" {
			_, found = diskByID[inst.ID]
		}
		if !found {
			_, found = diskByTitle[inst.Title]
		}
		if found {
			continue
		}
		// Rule 4: keep instances still being created.
		if !inst.Started() {
			continue
		}
		// Rule 3: skip the instance currently being edited.
		if m.editingInstance != nil && inst == m.editingInstance {
			continue
		}
		toRemove = append(toRemove, inst.Title)
	}
	for _, title := range toRemove {
		m.list.RemoveInstanceByTitle(title)
	}

	// Rule 5: update metadata for instances that exist in both.
	for _, inst := range m.list.GetInstances() {
		var diskInst *session.InstanceData
		if inst.ID != "" {
			diskInst = diskByID[inst.ID]
		}
		if diskInst == nil {
			diskInst = diskByTitle[inst.Title]
		}
		if diskInst == nil {
			continue
		}
		// Update diff stats from disk if available.
		if diskInst.DiffStats.Added != 0 || diskInst.DiffStats.Removed != 0 {
			inst.SetDiffStats(&vcs.DiffStats{
				Added:   diskInst.DiffStats.Added,
				Removed: diskInst.DiffStats.Removed,
				Content: diskInst.DiffStats.Content,
			})
		}
		// Propagate acknowledgment from other processes. ReadyAcknowledged
		// is a one-way latch (reset only on the next Running→Ready
		// transition), so adopting true from disk is always safe.
		if diskInst.ReadyAcknowledged && !inst.ReadyAcknowledged {
			inst.ReadyAcknowledged = true
		}
	}

	m.syncWorkspaceUI()
}

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	// List takes 30% of width, preview takes 70%
	listWidth := int(float32(msg.Width) * 0.3)
	tabsWidth := msg.Width - listWidth

	// Menu takes 10% of height, list and window take 90%
	contentHeight := int(float32(msg.Height) * 0.9)
	menuHeight := msg.Height - contentHeight - 1     // minus 1 for error box
	m.errBox.SetSize(int(float32(msg.Width)*0.9), 1) // error box takes 1 row

	// Subtract workspace tab bar height from content area.
	if m.workspaceTabBar != nil {
		tabBarHeight := m.workspaceTabBar.Height()
		contentHeight -= tabBarHeight
		m.workspaceTabBar.SetWidth(msg.Width)
	}

	m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	m.list.SetSize(listWidth, contentHeight)

	if m.textInputOverlay != nil {
		m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}
	if m.textOverlay != nil {
		m.textOverlay.SetWidth(int(float32(msg.Width) * 0.6))
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
		tickUpdateMetadataCmd(m.snapshotActiveInstances()),
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
		var notifyCmds []tea.Cmd
		for _, r := range msg.results {
			prevStatus := r.instance.Status
			if r.updated {
				r.instance.SetStatus(session.Running)
				if !r.instance.JustDetached {
					r.instance.ReadyAcknowledged = false
				}
				r.instance.JustDetached = false
			} else if r.hasPrompt {
				r.instance.TapEnter()
				r.instance.NotifiedReady = false
			} else {
				r.instance.SetStatus(session.Ready)
				// Check if user is currently viewing this instance's workspace.
				isViewingWorkspace := m.activeWorkspace < len(m.workspaces) &&
					m.workspaces[m.activeWorkspace].Path == r.instance.Path
				// Fire notification on Running -> Ready transition (only if not viewing).
				// Do NOT auto-acknowledge here — the blink should always appear so the
				// user sees the Ready signal even when already in that workspace.
				// Acknowledgment happens when the user presses Enter.
				if prevStatus == session.Running && !r.instance.NotifiedReady && !isViewingWorkspace && m.appConfig.GetNotifications() {
					r.instance.NotifiedReady = true
					title := "Claude Squad"
					for _, ws := range m.workspaces {
						if ws.Path == r.instance.Path {
							title += " - " + ws.Name
							break
						}
					}
					body := r.instance.Title + " is ready"
					notifyCmds = append(notifyCmds, func() tea.Msg {
						if err := notify.Send(title, body); err != nil {
							log.WarningLog.Printf("notification failed: %v", err)
						}
						return nil
					})
				}
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
		// Refresh workspace HasReady flags and tab bar.
		m.syncWorkspaceUI()

		// Trigger periodic disk reload every 2 ticks (~1 second).
		m.reloadTickCounter++
		if m.reloadTickCounter >= 2 {
			m.reloadTickCounter = 0
			storage := m.storage
			notifyCmds = append(notifyCmds, func() tea.Msg {
				data, err := storage.ReloadAndParse()
				return diskReloadDoneMsg{instances: data, err: err}
			})
		}

		cmds := append(notifyCmds, tickUpdateMetadataCmd(m.snapshotActiveInstances()))
		return m, tea.Batch(cmds...)
	case diskReloadDoneMsg:
		if msg.err != nil {
			log.WarningLog.Printf("disk reload failed: %v", msg.err)
			return m, nil
		}
		m.mergeReloadedInstances(msg.instances)
		// After merge, the active workspace may have lost all its instances
		// (e.g. another process killed them). Auto-switch to our own workspace.
		m.syncWorkspaceUI()
		if m.list.NumInstances() == 0 && m.currentRepoPath != "" && len(m.workspaces) > 0 {
			ownIdx := FindWorkspaceIndex(m.workspaces, m.currentRepoPath)
			if ownIdx != m.activeWorkspace {
				m.activeWorkspace = ownIdx
				m.list.SetFilter(m.workspaces[m.activeWorkspace].Path)
				if m.workspaceTabBar != nil {
					m.workspaceTabBar.SetActiveIdx(m.activeWorkspace)
				}
			}
		}
		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case tea.MouseMsg:
		// Handle mouse wheel events for scrolling the diff/preview pane
		if msg.Action == tea.MouseActionPress {
			if msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelUp {
				selected := m.list.GetSelectedInstance()
				if selected == nil || selected.Status == session.Paused {
					return m, nil
				}

				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.tabbedWindow.ScrollUp()
				case tea.MouseButtonWheelDown:
					m.tabbedWindow.ScrollDown()
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
		// Handle instance changed after confirmation action (e.g. kill).
		// Re-derive workspaces — a tab may have appeared or disappeared.
		m.syncWorkspaceUI()
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
		// Re-derive workspaces — a new tab may have appeared.
		m.syncWorkspaceUI()
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
		m.list.IncrementBlinkFrame()
		return m, cmd
	}
	return m, nil
}

func (m *home) handleQuit() (tea.Model, tea.Cmd) {
	// Guard: only allow quitting from the workspace cs was launched in.
	if m.currentRepoPath != "" && m.activeWorkspace < len(m.workspaces) &&
		m.workspaces[m.activeWorkspace].Path != m.currentRepoPath {
		return m, m.handleError(fmt.Errorf("switch to workspace %q before quitting",
			filepath.Base(m.currentRepoPath)))
	}
	if err := m.storage.SaveInstancesNonBlocking(m.list.GetInstances()); err != nil {
		log.WarningLog.Printf("quit: skipping save (lock busy): %v", err)
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
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm || m.state == stateNew {
		return nil, false
	}
	// If it's in the global keymap, we should try to highlight it.
	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return nil, false
	}

	if m.list.GetSelectedInstance() != nil && m.list.GetSelectedInstance().Paused() && name == keys.KeyEnter {
		return nil, false
	}
	if name == keys.KeyShiftDown || name == keys.KeyShiftUp || name == keys.KeyQuit {
		return nil, false
	}

	m.keySent = true
	return tea.Batch(
		func() tea.Msg { return msg },
		m.keydownCallback(name)), true
}

func (m *home) handleKeyPress(msg tea.KeyMsg) (mod tea.Model, cmd tea.Cmd) {
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
			m.editingInstance = nil
			m.list.Kill()
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		}

		// Use the stable editingInstance pointer rather than re-deriving from
		// the list. Background messages (metadataUpdateDoneMsg) can re-filter
		// the list and change what GetSelectedInstance() returns.
		instance := m.editingInstance
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
			m.editingInstance = nil
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
			m.editingInstance = nil
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
		if m.list.NumAllInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}

		// Start a background fetch so branches/bookmarks are up to date by the time the picker opens
		fetchCmd := func() tea.Msg {
			currentDir, _ := os.Getwd()
			cfg := config.LoadConfig()
			if cfg.GetVCSType() == "jj" {
				jj.FetchBookmarks(currentDir)
			} else {
				git.FetchBranches(currentDir)
			}
			return nil
		}

		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    m.currentRepoPath,
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.editingInstance = instance
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)
		m.promptAfterName = true

		return m, fetchCmd
	case keys.KeyNewPiMono, keys.KeyNewOpencode, keys.KeyNewClaude:
		if m.list.NumAllInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}

		var program string
		switch name {
		case keys.KeyNewPiMono:
			program = tmux.ProgramPiMono
		case keys.KeyNewOpencode:
			program = tmux.ProgramOpencode
		case keys.KeyNewClaude:
			claudeCmd, err := config.GetClaudeCommand()
			if err != nil {
				program = tmux.ProgramClaude
			} else {
				program = claudeCmd
			}
		}

		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    m.currentRepoPath,
			Program: program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.editingInstance = instance
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)

		return m, nil
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
			if err := selected.CanKill(); err != nil {
				return err
			}

			// Track locally deleted instance so disk reload doesn't re-add it.
			if selected.ID != "" {
				m.locallyDeleted[selected.ID] = true
			} else if selected.Title != "" {
				m.locallyDeleted["title:"+selected.Title] = true
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

		// Create the push action as a tea.Cmd
		pushAction := func() tea.Msg {
			// Default commit message with timestamp
			commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s", selected.Title, time.Now().Format(time.RFC822))
			if err := selected.PushChanges(commitMsg, true); err != nil {
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

		err := selected.CheckoutInMainRepo()
		if errors.Is(err, vcs.ErrCheckoutRequiresPause) {
			// Git: show help screen then pause
			m.showHelpScreen(helpTypeInstanceCheckout{}, func() {
				if err := selected.Pause(); err != nil {
					m.handleError(err)
				}
				m.tabbedWindow.CleanupTerminalForInstance(selected.Title)
				m.instanceChanged()
			})
		} else if err != nil {
			return m, m.handleError(err)
		} else {
			// JJ: already checked out, show confirmation + copy to clipboard
			_ = clipboard.WriteAll(selected.Branch)
			m.showHelpScreen(helpTypeJJCheckout{}, nil)
		}
		return m, nil
	case keys.KeyPrevWorkspace:
		if len(m.workspaces) < 2 {
			return m, nil
		}
		m.activeWorkspace--
		if m.activeWorkspace < 0 {
			m.activeWorkspace = len(m.workspaces) - 1
		}
		m.list.SetFilter(m.workspaces[m.activeWorkspace].Path)
		m.workspaceTabBar.SetActiveIdx(m.activeWorkspace)
		return m, m.instanceChanged()
	case keys.KeyNextWorkspace:
		if len(m.workspaces) < 2 {
			return m, nil
		}
		m.activeWorkspace++
		if m.activeWorkspace >= len(m.workspaces) {
			m.activeWorkspace = 0
		}
		m.list.SetFilter(m.workspaces[m.activeWorkspace].Path)
		m.workspaceTabBar.SetActiveIdx(m.activeWorkspace)
		return m, m.instanceChanged()
	case keys.KeyResume:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if err := selected.Resume(); err != nil {
			return m, m.handleError(err)
		}
		selected.ReadyAcknowledged = false
		return m, tea.WindowSize()
	case keys.KeyEnter:
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
			return m, nil
		}
		// Acknowledge immediately so blink/badge clears on enter, not on detach.
		selected.ReadyAcknowledged = true
		m.syncWorkspaceUI()
		// Terminal tab: attach to terminal session
		if m.tabbedWindow.IsInTerminalTab() {
			m.showHelpScreen(helpTypeInstanceAttach{}, func() {
				ch, err := m.tabbedWindow.AttachTerminal()
				if err != nil {
					m.handleError(err)
					return
				}
				<-ch
				selected.JustDetached = true
				selected.ReadyAcknowledged = true
				m.state = stateDefault
				m.syncWorkspaceUI()
			})
			return m, nil
		}
		// Show help screen before attaching
		m.showHelpScreen(helpTypeInstanceAttach{}, func() {
			ch, err := m.list.Attach()
			if err != nil {
				m.handleError(err)
				return
			}
			<-ch
			selected.JustDetached = true
			selected.ReadyAcknowledged = true
			m.state = stateDefault
			m.syncWorkspaceUI()
			// instanceChanged() is intentionally omitted here. Running tmux
			// capture-pane synchronously inside Update blocks Bubbletea from
			// rendering the TUI and is the primary cause of visible detach lag.
			// The previewTickMsg loop fires within 100ms and refreshes the preview.
		})
		return m, nil
	default:
		return m, nil
	}
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

// runBranchSearch returns a tea.Cmd that performs the branch/bookmark search in the background.
func (m *home) runBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		currentDir, _ := os.Getwd()
		cfg := config.LoadConfig()
		var branches []string
		var err error
		if cfg.GetVCSType() == "jj" {
			branches, err = jj.SearchBookmarks(currentDir, filter)
		} else {
			branches, err = git.SearchBranches(currentDir, filter)
		}
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
	diffStats *vcs.DiffStats
}

// metadataUpdateDoneMsg is sent when the background metadata update completes.
type metadataUpdateDoneMsg struct {
	results []instanceMetaResult
}

// diskReloadDoneMsg is sent when the periodic disk reload completes.
type diskReloadDoneMsg struct {
	instances []session.InstanceData
	err       error
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

// metadataTimeout is the maximum time to wait for all metadata goroutines to
// complete. If any goroutine hangs (e.g., due to jj/git lock contention when
// multiple cs processes share the same repo), the loop returns partial results
// instead of blocking forever.
const metadataTimeout = 10 * time.Second

// tickUpdateMetadataCmd returns a self-chaining Cmd that sleeps 500ms, then performs
// expensive metadata I/O (tmux capture, git diff) in parallel background goroutines.
// Because it only re-schedules after completing, overlapping ticks are impossible.
// The active instances slice should be snapshotted on the main thread via
// snapshotActiveInstances() before being passed here.
func tickUpdateMetadataCmd(active []*session.Instance) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)

		if len(active) == 0 {
			return metadataUpdateDoneMsg{}
		}

		results := make([]instanceMetaResult, len(active))
		done := make(chan struct{})
		go func() {
			var wg sync.WaitGroup
			for idx, inst := range active {
				wg.Add(1)
				go func(i int, instance *session.Instance) {
					defer wg.Done()
					r := &results[i]
					r.instance = instance
					r.updated, r.hasPrompt = instance.HasUpdated()
					r.diffStats = instance.ComputeDiff()
				}(idx, inst)
			}
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All goroutines completed normally.
		case <-time.After(metadataTimeout):
			log.WarningLog.Printf("metadata update timed out after %v, returning partial results", metadataTimeout)
		}

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

	rows := []string{listAndPreview, m.menu.String(), m.errBox.String()}
	if m.workspaceTabBar != nil {
		rows = append([]string{m.workspaceTabBar.View()}, rows...)
	}
	mainView := lipgloss.JoinVertical(lipgloss.Center, rows...)

	if m.state == statePrompt {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
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
