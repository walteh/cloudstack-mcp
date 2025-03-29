package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	gotmux "github.com/jubnzv/go-tmux"
	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"
)

const (
	// MasterSessionName is the name of the main tmux session that contains all VM windows
	MasterSessionName = "kvmctl"
)

// SessionManager handles tmux session and windows for virtual machines
type SessionManager struct {
	mu     sync.Mutex
	server *gotmux.Server
	logger zerolog.Logger
	// Track VM windows in the master session (vmName -> windowId)
	vmWindows map[string]int
}

// NewSessionManager creates a new tmux session manager
func NewSessionManager(logger zerolog.Logger) (*SessionManager, error) {
	// Check if tmux is installed
	_, err := exec.LookPath("tmux")
	if err != nil {
		return nil, errors.Errorf("tmux is not installed: %w", err)
	}

	// Create a tmux server instance
	server := new(gotmux.Server)

	return &SessionManager{
		server:    server,
		logger:    logger.With().Str("component", "tmux-manager").Logger(),
		vmWindows: make(map[string]int),
	}, nil
}

// ensureMasterSession makes sure the main kvmctl session exists
func (sm *SessionManager) ensureMasterSession(ctx context.Context) error {
	// Check if the master session already exists
	exists, err := sm.server.HasSession(MasterSessionName)
	if err != nil {
		return errors.Errorf("checking for master session: %w", err)
	}

	if !exists {
		sm.logger.Info().Msg("Creating master kvmctl session")
		_, err = sm.server.NewSession(MasterSessionName)
		if err != nil {
			return errors.Errorf("creating master session: %w", err)
		}
	}

	return nil
}

// HasVM checks if a VM has an active window in the master session
func (sm *SessionManager) HasVM(ctx context.Context, vmName string) (bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if vmName == "" {
		return false, errors.Errorf("VM name cannot be empty")
	}

	// Make sure the master session exists
	err := sm.ensureMasterSession(ctx)
	if err != nil {
		return false, err
	}

	// Check if we're tracking this VM
	_, exists := sm.vmWindows[vmName]
	return exists, nil
}

// CreateVMWindow creates a new window for a VM in the master session
func (sm *SessionManager) CreateVMWindow(ctx context.Context, vmName string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Validate VM name
	if vmName == "" {
		return errors.Errorf("VM name cannot be empty")
	}

	sm.logger.Debug().Str("vm", vmName).Msg("Creating VM window")

	// Make sure the master session exists
	err := sm.ensureMasterSession(ctx)
	if err != nil {
		return err
	}

	// Check if this VM already has a window
	if _, exists := sm.vmWindows[vmName]; exists {
		sm.logger.Debug().Str("vm", vmName).Msg("VM window already exists")
		return nil
	}

	// Get the master session
	session := gotmux.Session{Name: MasterSessionName}

	// Create a new window named after the VM
	window, err := session.NewWindow(vmName)
	if err != nil {
		return errors.Errorf("creating window for VM %s: %w", vmName, err)
	}

	// Set an identifying header for the window
	err = window.Panes[0].RunCommand(fmt.Sprintf("echo '=== VM: %s ===' && clear", vmName))
	if err != nil {
		return errors.Errorf("setting window header: %w", err)
	}

	// Store the window ID for this VM
	sm.vmWindows[vmName] = window.Id

	sm.logger.Info().Str("vm", vmName).Int("window", window.Id).Msg("VM window created")
	return nil
}

// RunCommand executes a command in the VM's window
func (sm *SessionManager) RunCommand(ctx context.Context, vmName string, command string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if vmName == "" {
		return errors.Errorf("VM name cannot be empty")
	}

	sm.logger.Debug().Str("vm", vmName).Str("command", command).Msg("Running command")

	// Check if this VM has a window
	windowId, exists := sm.vmWindows[vmName]
	if !exists {
		return errors.Errorf("no window exists for VM %s", vmName)
	}

	// Get the master session
	session := gotmux.Session{Name: MasterSessionName}

	// Get all windows in the session
	windows, err := session.ListWindows()
	if err != nil {
		return errors.Errorf("listing windows: %w", err)
	}

	// Find the window for this VM
	var vmWindow gotmux.Window
	for _, w := range windows {
		if w.Id == windowId {
			vmWindow = w
			break
		}
	}

	if vmWindow.Id == 0 {
		// We couldn't find the window - it may have been closed
		delete(sm.vmWindows, vmName)
		return errors.Errorf("window for VM %s not found", vmName)
	}

	// Get the main pane in the window
	panes, err := vmWindow.ListPanes()
	if err != nil {
		return errors.Errorf("listing panes in window: %w", err)
	}

	if len(panes) == 0 {
		return errors.Errorf("no panes found in window for VM %s", vmName)
	}

	// Run command in the first pane of the window
	err = panes[0].RunCommand(command)
	if err != nil {
		return errors.Errorf("running command: %w", err)
	}

	sm.logger.Debug().Str("vm", vmName).Str("command", command).Msg("Command executed")
	return nil
}

// SelectVMWindow selects (focuses) a VM's window
func (sm *SessionManager) SelectVMWindow(ctx context.Context, vmName string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if vmName == "" {
		return errors.Errorf("VM name cannot be empty")
	}

	sm.logger.Debug().Str("vm", vmName).Msg("Selecting VM window")

	// Check if this VM has a window
	windowId, exists := sm.vmWindows[vmName]
	if !exists {
		return errors.Errorf("no window exists for VM %s", vmName)
	}

	// Get the master session
	session := gotmux.Session{Name: MasterSessionName}

	// Get all windows in the session
	windows, err := session.ListWindows()
	if err != nil {
		return errors.Errorf("listing windows: %w", err)
	}

	// Find the window for this VM
	var vmWindow gotmux.Window
	for _, w := range windows {
		if w.Id == windowId {
			vmWindow = w
			break
		}
	}

	if vmWindow.Id == 0 {
		// We couldn't find the window - it may have been closed
		delete(sm.vmWindows, vmName)
		return errors.Errorf("window for VM %s not found", vmName)
	}

	// Select the window
	err = vmWindow.Select()
	if err != nil {
		return errors.Errorf("selecting window: %w", err)
	}

	sm.logger.Debug().Str("vm", vmName).Msg("VM window selected")
	return nil
}

// AttachSession attaches to the master session
func (sm *SessionManager) AttachSession(ctx context.Context) error {
	sm.logger.Debug().Msg("Attaching to kvmctl session")

	// Make sure the master session exists
	err := sm.ensureMasterSession(ctx)
	if err != nil {
		return err
	}

	// Attach to the session
	session := gotmux.Session{Name: MasterSessionName}
	err = session.AttachSession()
	if err != nil {
		return errors.Errorf("attaching to session: %w", err)
	}

	return nil
}

// CloseVMWindow closes a VM's window in the master session
func (sm *SessionManager) CloseVMWindow(ctx context.Context, vmName string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if vmName == "" {
		return errors.Errorf("VM name cannot be empty")
	}

	sm.logger.Debug().Str("vm", vmName).Msg("Closing VM window")

	// Check if this VM has a window
	windowId, exists := sm.vmWindows[vmName]
	if !exists {
		// VM doesn't have a window, nothing to do
		return nil
	}

	// Get the master session
	session := gotmux.Session{Name: MasterSessionName}

	// Get all windows in the session
	windows, err := session.ListWindows()
	if err != nil {
		return errors.Errorf("listing windows: %w", err)
	}

	// Find the window for this VM
	var vmWindow gotmux.Window
	for _, w := range windows {
		if w.Id == windowId {
			vmWindow = w
			break
		}
	}

	if vmWindow.Id != 0 {
		// Have the window exit by sending an exit command to its main pane
		panes, err := vmWindow.ListPanes()
		if err == nil && len(panes) > 0 {
			err = panes[0].RunCommand("exit")
			if err != nil {
				sm.logger.Warn().Err(err).Str("vm", vmName).Msg("Error sending exit command to window, removing from tracking anyway")
			}
		}
	}

	// Remove the VM from our tracking
	delete(sm.vmWindows, vmName)

	sm.logger.Info().Str("vm", vmName).Msg("VM window closed")
	return nil
}

// CloseAllVMWindows closes all VM windows
func (sm *SessionManager) CloseAllVMWindows(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.logger.Debug().Msg("Closing all VM windows")

	// Check if the session exists
	exists, err := sm.server.HasSession(MasterSessionName)
	if err != nil {
		return errors.Errorf("checking for master session: %w", err)
	}

	if !exists {
		// Session doesn't exist, nothing to do
		sm.vmWindows = make(map[string]int)
		return nil
	}

	// For each VM, try to close its window
	for vmName, _ := range sm.vmWindows {
		sm.mu.Unlock() // Temporarily unlock to avoid deadlock
		err := sm.CloseVMWindow(ctx, vmName)
		sm.mu.Lock() // Lock again
		if err != nil {
			sm.logger.Warn().Err(err).Str("vm", vmName).Msg("Error closing VM window")
		}
	}

	// Clear all VM window mappings
	sm.vmWindows = make(map[string]int)

	sm.logger.Info().Msg("All VM windows closed")
	return nil
}

// ShutdownSession completely shuts down the master session
func (sm *SessionManager) ShutdownSession(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.logger.Debug().Msg("Shutting down kvmctl session")

	// Check if the session exists
	exists, err := sm.server.HasSession(MasterSessionName)
	if err != nil {
		return errors.Errorf("checking for master session: %w", err)
	}

	if !exists {
		// Session doesn't exist, nothing to do
		sm.vmWindows = make(map[string]int)
		return nil
	}

	// Kill the session
	err = sm.server.KillSession(MasterSessionName)
	if err != nil {
		return errors.Errorf("killing session: %w", err)
	}

	// Clear all VM window mappings
	sm.vmWindows = make(map[string]int)

	sm.logger.Info().Msg("Session shutdown complete")
	return nil
}

// GetSessionName returns the tmux session name for a VM
func GetSessionName(vmName string) string {
	return fmt.Sprintf("vm-%s", vmName)
}
