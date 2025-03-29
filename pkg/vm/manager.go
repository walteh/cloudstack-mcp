package vm

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/walteh/cloudstack-mcp/pkg/tmux"
	"gitlab.com/tozd/go/errors"
)

// Manager defines the interface for managing VMs
type Manager interface {
	// CreateVM creates a new VM with the given configuration
	CreateVM(ctx context.Context, config VMConfig) (*VM, error)

	// StartVM starts a VM
	StartVM(ctx context.Context, vm *VM) error

	// StopVM stops a running VM
	StopVM(ctx context.Context, vm *VM) error

	// DeleteVM deletes a VM and its associated resources
	DeleteVM(ctx context.Context, vm *VM) error

	// GetVM retrieves a VM by name
	GetVM(ctx context.Context, name string) (*VM, error)

	// ListVMs lists all available VMs
	ListVMs(ctx context.Context) ([]*VM, error)

	// WaitForInitialization waits for VM initialization to complete
	WaitForInitialization(ctx context.Context, vm *VM) error

	// StartVMVisible starts a VM in a visible terminal
	StartVMVisible(ctx context.Context, vm *VM) error

	// StartVMForeground starts a VM in the foreground
	StartVMForeground(ctx context.Context, vm *VM) error

	// ShellVM connects to a VM via SSH and opens an interactive shell
	ShellVM(ctx context.Context, vm *VM) error

	// ExecVM runs a command on a VM via SSH
	ExecVM(ctx context.Context, vm *VM, command []string) error
}

// LocalManager implements Manager for local QEMU VMs
type LocalManager struct {
	QemuPath    string
	MkisofsPath string
	QemuImgPath string
	baseDir     string
	TmuxManager *tmux.SessionManager
}

// NewLocalManager creates a new LocalManager
func NewLocalManager() (*LocalManager, error) {
	logger := log.Logger.With().Str("component", "vm-manager").Logger()

	// Get VM data directory
	vmsPath := vmsDir()

	// Check if vms directory exists, if not, create it
	if _, err := os.Stat(vmsPath); os.IsNotExist(err) {
		logger.Info().Str("dir", vmsPath).Msg("Creating VMs directory")
		if err := os.MkdirAll(vmsPath, 0755); err != nil {
			return nil, errors.Errorf("creating VMs directory: %w", err)
		}
	}

	// Find qemu-system-aarch64 binary
	qemuPath, err := exec.LookPath("qemu-system-aarch64")
	if err != nil {
		return nil, errors.Errorf("looking for qemu-system-aarch64 binary: %w", err)
	}

	// Find genisoimage or mkisofs binary
	mkisofsPath, err := exec.LookPath("genisoimage")
	if err != nil {
		// Try mkisofs as an alternative
		mkisofsPath, err = exec.LookPath("mkisofs")
		if err != nil {
			return nil, errors.Errorf("looking for genisoimage or mkisofs binary: %w", err)
		}
	}

	// Find qemu-img binary
	qemuImgPath, err := exec.LookPath("qemu-img")
	if err != nil {
		return nil, errors.Errorf("looking for qemu-img binary: %w", err)
	}

	// Initialize tmux session manager
	tmuxManager, err := tmux.NewSessionManager(logger)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to initialize tmux session manager, continuing without it")
		// Continue without tmux support
	}

	return &LocalManager{
		QemuPath:    qemuPath,
		MkisofsPath: mkisofsPath,
		QemuImgPath: qemuImgPath,
		baseDir:     vmsDir(),    // Set the baseDir field here
		TmuxManager: tmuxManager, // Add tmux manager
	}, nil
}

// CreateVM creates a new VM with the given configuration
func (m *LocalManager) CreateVM(ctx context.Context, config VMConfig) (*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", config.Name).Msg("Creating VM")

	vm := &VM{
		Config: config,
		SSHInfo: SSHInfo{
			Username:   "ubuntu", // Default for Ubuntu cloud images
			PrivateKey: "",       // Will be set when VM starts
			Host:       "",       // Will be set when VM starts
			Port:       22,
		},
		MetaData: map[string]string{},
	}

	if err := os.MkdirAll(vm.Dir(), 0755); err != nil {
		return nil, errors.Errorf("creating VM directory: %w", err)
	}

	// Create VM disk based on base image

	imgPath := config.BaseImg.Path()

	// Download base image if it doesn't exist
	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		// TODO: implement image download
		return nil, errors.Errorf("base image %s does not exist", imgPath)
	}

	// Create disk using qemu-img
	cmd := exec.CommandContext(
		ctx,
		m.QemuImgPath,
		"create",
		"-F", "qcow2",
		"-b", imgPath,
		"-f", "qcow2",
		vm.DiskPath(),
		config.DiskSize,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Errorf("creating VM disk: %s: %w", output, err)
	}

	vm.MetaData["disk_output"] = string(output)

	metaData, err := vm.BuildMetaData()
	if err != nil {
		return nil, errors.Errorf("generating meta-data: %w", err)
	}

	userData, err := vm.UserData(true)
	if err != nil {
		return nil, errors.Errorf("generating user-data: %w", err)
	}

	networkConfig, err := vm.NetworkConfig()
	if err != nil {
		return nil, errors.Errorf("generating network-config: %w", err)
	}

	// Create cloud-init files
	metaDataPath := filepath.Join(vm.Dir(), "meta-data")
	if err := os.WriteFile(metaDataPath, []byte(metaData), 0644); err != nil {
		return nil, errors.Errorf("writing meta-data: %w", err)
	}

	userDataPath := filepath.Join(vm.Dir(), "user-data")
	if err := os.WriteFile(userDataPath, []byte(userData), 0644); err != nil {
		return nil, errors.Errorf("writing user-data: %w", err)
	}

	networkConfigPath := filepath.Join(vm.Dir(), "network-config")
	if err := os.WriteFile(networkConfigPath, []byte(networkConfig), 0644); err != nil {
		return nil, errors.Errorf("writing network-config: %w", err)
	}

	// Create cloud-init ISO
	mkisofsCmdArgs := []string{
		"-output", vm.CIDataPath(),
		"-volid", "cidata",
		"-joliet",
		"-rock",
		metaDataPath,
		userDataPath,
		networkConfigPath,
	}

	cmd = exec.CommandContext(ctx, m.MkisofsPath, mkisofsCmdArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, errors.Errorf("creating cloud-init ISO: %s: %w", output, err)
	}

	logger.Info().Str("name", config.Name).Msg("VM created successfully")

	// Save the state
	if err := vm.SaveState(); err != nil {
		return nil, errors.Errorf("saving VM state: %w", err)
	}

	return vm, nil
}

// StopVM stops a running VM

// DeleteVM deletes a VM and its associated resources
func (m *LocalManager) DeleteVM(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Deleting VM")

	// Stop the VM if it's running
	if isRunning, err := VMIsRunning(ctx, m.TmuxManager, vm); err != nil {
		return errors.Errorf("checking if VM is running: %w", err)
	} else if isRunning {
		if err := vm.Stop(ctx, m.TmuxManager); err != nil {
			return errors.Errorf("stopping VM before deletion: %w", err)
		}
	}

	// Delete VM directory and all resources
	if err := os.RemoveAll(vm.Dir()); err != nil {
		return errors.Errorf("deleting VM directory: %w", err)
	}

	return nil
}

// GetVM retrieves a VM by name
func (m *LocalManager) GetVM(ctx context.Context, name string) (*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", name).Msg("Getting VM")

	vmDir := filepath.Join(m.baseDir, name)
	if _, err := os.Stat(vmDir); os.IsNotExist(err) {
		return nil, errors.Errorf("VM not found: %s", name)
	}

	// Create a basic VM object
	vm := &VM{
		Name: name,
		Config: VMConfig{
			Name: name,
		},
	}

	// Load VM state from disk
	stateFile := filepath.Join(vmDir, "vm-state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		// No state file, assume VM is stopped
		return vm, nil
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, errors.Errorf("reading VM state file: %w", err)
	}

	if err := json.Unmarshal(data, vm); err != nil {
		return nil, errors.Errorf("unmarshaling VM state: %w", err)
	}

	// Ensure the VM name is set
	if vm.Name == "" {
		vm.Name = name
		vm.SaveState()
	}

	// Ensure config is at least partially populated
	if vm.Config.Name == "" {
		vm.Config.Name = name
		vm.SaveState()
	}

	return vm, nil
}

// ListVMs lists all available VMs
func (m *LocalManager) ListVMs(ctx context.Context) ([]*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Listing VMs")

	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			// VMs directory doesn't exist, return empty list
			return []*VM{}, nil
		}
		return nil, errors.Errorf("reading VMs directory: %w", err)
	}

	vms := make([]*VM, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			vmName := entry.Name()
			vm, err := m.GetVM(ctx, vmName)
			if err != nil {
				logger.Warn().Err(err).Str("name", vmName).Msg("Failed to load VM")
				// Continue with other VMs
				continue
			}
			vms = append(vms, vm)
		}
	}

	return vms, nil
}

// CleanupVMs stops all VM processes and moves their folders to a vms-deleted directory
func (m *LocalManager) CleanupVMs(ctx context.Context) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Cleaning up all VMs")

	// Get all VMs
	vms, err := m.ListVMs(ctx)
	if err != nil {
		return errors.Errorf("listing VMs: %w", err)
	}

	// Create vms-deleted directory if it doesn't exist
	deletedDir := filepath.Join(m.baseDir, "vms-deleted")
	if err := os.MkdirAll(deletedDir, 0755); err != nil {
		return errors.Errorf("creating vms-deleted directory: %w", err)
	}

	// Process each VM
	for _, vm := range vms {
		logger.Info().Str("name", vm.Name).Msg("Cleaning up VM")

		// Kill the process if it's running
		if isRunning, err := VMIsRunning(ctx, m.TmuxManager, vm); err != nil {
			logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to check if VM is running")
		} else if isRunning {
			logger.Info().Str("name", vm.Name).Msg("Stopping VM process")
			if err := vm.Stop(ctx, m.TmuxManager); err != nil {
				logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to stop VM gracefully, killing process")
				if err := vm.Stop(ctx, m.TmuxManager); err != nil {
					logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to stop VM gracefully, killing process")
				}
			}
		}

		// Move the VM directory to vms-deleted
		srcDir := vm.Dir()
		destDir := filepath.Join(deletedDir, vm.Name)

		// Remove destination directory if it already exists
		if _, err := os.Stat(destDir); err == nil {
			logger.Info().Str("name", vm.Name).Msg("Removing existing VM in deleted directory")
			if err := os.RemoveAll(destDir); err != nil {
				logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to remove existing deleted VM directory")
			}
		}

		logger.Info().Str("name", vm.Name).Str("src", srcDir).Str("dest", destDir).Msg("Moving VM directory")
		if err := os.Rename(srcDir, destDir); err != nil {
			logger.Warn().Err(err).Str("name", vm.Name).Msg("Failed to move VM directory")
		}
	}

	logger.Info().Int("count", len(vms)).Msg("VM cleanup completed")
	return nil
}

// // ShellVM connects to a VM via SSH and opens an interactive shell
// func (m *LocalManager) ShellVM(ctx context.Context, vm *VM) error {
// 	logger := zerolog.Ctx(ctx)
// 	logger.Info().Str("name", vm.Config.Name).Msg("Opening shell to VM")

// 	// If we have tmux support, use the tmux window
// 	if m.TmuxManager != nil {
// 		// Check if the VM has a tmux window
// 		hasWindow, err := m.TmuxManager.HasVM(ctx, vm.Name)
// 		if err != nil {
// 			logger.Warn().Err(err).Msg("Error checking for VM tmux window")
// 		} else if !hasWindow {
// 			// Create a window if it doesn't exist
// 			if err := m.TmuxManager.CreateVMWindow(ctx, vm.Name); err != nil {
// 				logger.Warn().Err(err).Msg("Failed to create tmux window for VM, falling back to direct SSH")
// 			}
// 		}

// 		// Run SSH command in the VM's window
// 		sshCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p %d %s@%s",
// 			vm.SSHInfo.Port, vm.SSHInfo.Username, vm.SSHInfo.Host)

// 		if err := m.TmuxManager.RunCommand(ctx, vm.Name, sshCmd); err != nil {
// 			logger.Warn().Err(err).Msg("Failed to run SSH in tmux window, falling back to direct SSH")
// 		} else {
// 			// Attach to the tmux session to interact with the VM
// 			if err := m.TmuxManager.AttachSession(ctx); err != nil {
// 				logger.Warn().Err(err).Msg("Failed to attach to tmux session, falling back to direct SSH")
// 			} else {
// 				// Successfully connected via tmux
// 				return nil
// 			}
// 		}
// 	}

// 	// Fall back to direct SSH if tmux is not available or failed
// 	sshClient, err := vm.ConnectSSH()
// 	if err != nil {
// 		return errors.Errorf("connecting to VM via SSH: %w", err)
// 	}
// 	defer sshClient.Close()

// 	// Create a new SSH session
// 	session, err := sshClient.NewSession()
// 	if err != nil {
// 		return errors.Errorf("creating SSH session: %w", err)
// 	}
// 	defer session.Close()

// 	// Set up terminal modes
// 	modes := ssh.TerminalModes{
// 		ssh.ECHO:          1,     // enable echoing
// 		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
// 		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
// 	}

// 	// Request a pseudo terminal
// 	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
// 		return errors.Errorf("requesting pseudo terminal: %w", err)
// 	}

// 	// Connect session to stdin/stdout
// 	session.Stdin = os.Stdin
// 	session.Stdout = os.Stdout
// 	session.Stderr = os.Stderr

// 	// Start interactive shell
// 	if err := session.Shell(); err != nil {
// 		return errors.Errorf("starting shell: %w", err)
// 	}

// 	// Wait for session to end
// 	if err := session.Wait(); err != nil {
// 		// Don't return error here, as it's normal for shell sessions to exit with a signal
// 		logger.Debug().Err(err).Msg("SSH shell session ended")
// 	}

// 	return nil
// }

// ExecVM runs a command on a VM via SSH

// startVMProcess starts the QEMU process for a VM

// WaitForVM waits for a VM to be ready
