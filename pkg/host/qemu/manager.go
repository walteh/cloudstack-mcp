package qemu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/digitalocean/go-qemu/qmp"
	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/host"
	"gitlab.com/tozd/go/errors"
)

// Status represents the VM status

var _ host.Host = &Manager{}

// Manager handles QEMU VM operations
type Manager struct {
	workDir string
	logger  zerolog.Logger
	vms     map[string]*exec.Cmd
}

// NewManager creates a new QEMU manager
func NewManager(workDir string, logger zerolog.Logger) *Manager {
	return &Manager{
		workDir: workDir,
		logger:  logger,
		vms:     make(map[string]*exec.Cmd),
	}
}

// CheckQEMUInstalled verifies if QEMU is installed
func (m *Manager) InstallDependencies(ctx context.Context) error {
	m.logger.Info().Msg("Checking if QEMU is installed")

	var cmd *exec.Cmd
	if runtime.GOARCH == "arm64" {
		cmd = exec.CommandContext(ctx, "qemu-system-aarch64", "--version")
	} else {
		cmd = exec.CommandContext(ctx, "qemu-system-x86_64", "--version")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("QEMU is not installed or not in PATH: %w", err)
	}

	m.logger.Info().Str("version", string(output)).Msg("QEMU is installed")
	return nil
}

// CreateVMWithConfig creates a new VM with the given configuration
func (m *Manager) CreateVMWithConfig(ctx context.Context, config host.VMConfig) error {
	m.logger.Info().
		Str("name", config.Name).
		Int("cpu", config.CPU).
		Int("memoryMB", config.MemoryMB).
		Str("diskPath", config.DiskPath).
		Str("machine", config.Machine).
		Msg("Creating VM")

	// Check if VM is already running
	if cmd, exists := m.vms[config.Name]; exists && cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGCONT); err == nil {
			m.logger.Info().Str("name", config.Name).Msg("VM is already running")
			return nil
		}
		delete(m.vms, config.Name)
	}

	// Create sockets directory if it doesn't exist
	socketsDir := filepath.Join(m.workDir, "sockets")
	if err := os.MkdirAll(socketsDir, 0755); err != nil {
		return errors.Errorf("creating sockets directory: %w", err)
	}

	// Build QEMU command based on architecture
	var qemuBin string
	if runtime.GOARCH == "arm64" {
		qemuBin = "qemu-system-aarch64"
	} else {
		qemuBin = "qemu-system-x86_64"
	}

	// Create the command
	args := []string{
		"-name", config.Name,
		"-machine", config.Machine,
		"-m", fmt.Sprintf("%d", config.MemoryMB),
		"-smp", fmt.Sprintf("%d", config.CPU),
		"-drive", fmt.Sprintf("file=%s,format=qcow2", config.DiskPath),
	}

	// Add QMP socket
	socketPath := filepath.Join(socketsDir, config.Name+".sock")
	args = append(args, "-qmp", fmt.Sprintf("unix:%s,server,nowait", socketPath))

	// Add networking
	args = append(args,
		"-netdev", "user,id=net0",
		"-device", fmt.Sprintf("%s,netdev=net0", config.NetDevice),
	)

	// Add display
	if runtime.GOARCH == "arm64" {
		m.logger.Info().Msg("Using virtio-gpu for ARM64")
		args = append(args,
			"-device", "virtio-gpu-pci",
			"-display", "cocoa",
		)
	} else if config.Headless {
		args = append(args, "-nographic")
	} else {
		args = append(args,
			"-vga", config.VGA,
			"-display", "cocoa",
		)
	}

	// Start QEMU
	cmd := exec.CommandContext(ctx, qemuBin, args...)
	m.logger.Debug().Strs("args", args).Msg("QEMU command")

	if err := cmd.Start(); err != nil {
		return errors.Errorf("failed to start QEMU: %w", err)
	}

	// Store the command for later use
	m.vms[config.Name] = cmd

	// Wait for QMP socket to be available
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	m.logger.Info().Str("name", config.Name).Msg("VM created successfully")
	return nil
}

// ListRunningVMs returns a list of running VM names
func (m *Manager) ListRunningVMs() ([]string, error) {
	var names []string
	for name, cmd := range m.vms {
		if cmd.Process != nil {
			if err := cmd.Process.Signal(syscall.SIGCONT); err == nil {
				names = append(names, name)
			}
		}
	}
	return names, nil
}

// GetVMStatus returns the status of a VM
func (m *Manager) GetVMStatus(ctx context.Context, name string) (host.Status, error) {
	if cmd, exists := m.vms[name]; exists && cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGCONT); err == nil {
			return host.StatusRunning, nil
		}
	}
	return host.StatusUnknown, nil
}

// GetVMInfo returns information about a VM
func (m *Manager) GetVMInfo(ctx context.Context, name string) (*host.VMInfo, error) {
	if cmd, exists := m.vms[name]; exists && cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGCONT); err == nil {
			// Create QMP monitor for detailed info
			socketPath := filepath.Join(m.workDir, "sockets", name+".sock")
			mon, err := qmp.NewSocketMonitor("unix", socketPath, 2*time.Second)
			if err != nil {
				return nil, errors.Errorf("creating QMP monitor: %w", err)
			}

			if err := mon.Connect(); err != nil {
				return nil, errors.Errorf("connecting to QMP monitor: %w", err)
			}
			defer mon.Disconnect()

			// For now, return default values since we need to parse QMP output
			// to get accurate CPU and memory information
			info := &host.VMInfo{
				CPUs:     4,    // Default value
				MemoryMB: 4096, // Default value
			}
			return info, nil
		}
	}

	return nil, errors.Errorf("VM not found or not running: %s", name)
}

// CreateDisk creates a new QCOW2 disk image
func (m *Manager) CreateDisk(ctx context.Context, path string, sizeGB int) error {
	cmd := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", path, fmt.Sprintf("%dG", sizeGB))
	if err := cmd.Run(); err != nil {
		return errors.Errorf("creating disk image: %w", err)
	}
	return nil
}
