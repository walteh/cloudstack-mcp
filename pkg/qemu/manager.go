package qemu

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/digitalocean/go-qemu/qemu"
	"github.com/digitalocean/go-qemu/qmp"
	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"
)

// VMInfo contains information about a VM
type VMInfo struct {
	CPUs     int
	MemoryMB int
	VNCPort  string
	QMPPort  string
}

// Status represents the VM status
type Status int

const (
	StatusUnknown Status = iota
	StatusRunning
	StatusShutdown
	StatusPaused
	StatusSaved
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusShutdown:
		return "shutdown"
	case StatusPaused:
		return "paused"
	case StatusSaved:
		return "saved"
	default:
		return "unknown"
	}
}

// VMConfig represents configuration for a QEMU VM
type VMConfig struct {
	Name      string
	CPU       int
	MemoryMB  int
	DiskPath  string
	NetDevice string
	NetBridge string
	CDROM     string
	VGA       string
	UseBIOS   bool
	UseTPM    bool
	KVM       bool
	Headless  bool
	Machine   string // Machine type (especially needed for ARM64)
}

// NewVMConfig creates a new VM configuration with defaults
func NewVMConfig(name string, diskPath string) VMConfig {
	// Default machine type based on architecture
	machine := "q35"
	if runtime.GOARCH == "arm64" {
		machine = "virt"
	}

	return VMConfig{
		Name:      name,
		CPU:       4,
		MemoryMB:  4096,
		DiskPath:  diskPath,
		NetDevice: "virtio-net-pci",
		NetBridge: "virbr0",
		VGA:       "std",
		KVM:       true,
		UseBIOS:   false,
		UseTPM:    false,
		Headless:  false,
		Machine:   machine,
	}
}

// Domain represents a running QEMU VM
type Domain struct {
	cmd       *exec.Cmd
	qmpSocket string
	pidFile   string
	domain    *qemu.Domain
}

// Status returns the current status of the domain
func (d *Domain) Status() (Status, error) {
	if d.cmd == nil {
		return StatusUnknown, fmt.Errorf("domain not initialized")
	}

	// Check if process is running
	if d.cmd.Process != nil {
		if err := d.cmd.Process.Signal(syscall.Signal(0)); err == nil {
			return StatusRunning, nil
		}
	}

	return StatusShutdown, nil
}

// SystemPowerdown initiates a graceful shutdown of the domain
func (d *Domain) SystemPowerdown() error {
	if d.cmd == nil || d.cmd.Process == nil {
		return fmt.Errorf("domain not running")
	}

	// Send SIGTERM for graceful shutdown
	if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send powerdown signal: %w", err)
	}

	return nil
}

// Manager handles QEMU VM operations
type Manager struct {
	workDir  string
	logger   zerolog.Logger
	sockets  map[string]string
	domains  map[string]*Domain
	monitors map[string]*qmp.SocketMonitor
	hasKVM   bool
}

// NewManager creates a new QEMU manager
func NewManager(workDir string, logger zerolog.Logger) *Manager {
	socketDir := filepath.Join(workDir, "sockets")
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		logger.Fatal().Err(err).Str("socketDir", socketDir).Msg("Failed to create socket directory")
	}

	return &Manager{
		workDir:  workDir,
		logger:   logger,
		sockets:  make(map[string]string),
		domains:  make(map[string]*Domain),
		monitors: make(map[string]*qmp.SocketMonitor),
		hasKVM:   isKVMAvailable(),
	}
}

// CheckQEMUInstalled verifies if QEMU is installed
func (m *Manager) CheckQEMUInstalled(ctx context.Context) error {
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
func (m *Manager) CreateVMWithConfig(ctx context.Context, config VMConfig) error {
	m.logger.Info().
		Str("name", config.Name).
		Int("cpu", config.CPU).
		Int("memoryMB", config.MemoryMB).
		Str("diskPath", config.DiskPath).
		Str("machine", config.Machine).
		Msg("Creating VM")

	// Create socket for QMP
	socketPath := filepath.Join(m.workDir, "sockets", config.Name+".sock")
	m.sockets[config.Name] = socketPath

	// Create PID file path
	pidFile := filepath.Join(m.workDir, "sockets", config.Name+".pid")

	// Build QEMU command based on architecture
	var qemuBin string
	if runtime.GOARCH == "arm64" {
		qemuBin = "qemu-system-aarch64"
	} else {
		qemuBin = "qemu-system-x86_64"
	}

	// Common args
	args := []string{
		"-name", config.Name,
		"-machine", config.Machine,
		"-m", fmt.Sprintf("%d", config.MemoryMB),
		"-smp", fmt.Sprintf("%d", config.CPU),
		"-drive", fmt.Sprintf("file=%s,format=qcow2", config.DiskPath),
		"-qmp", fmt.Sprintf("unix:%s,server,nowait", socketPath),
		"-pidfile", pidFile,
	}

	// Add KVM if enabled and available
	if config.KVM && isKVMAvailable() {
		args = append(args, "-enable-kvm")
		m.logger.Info().Msg("KVM acceleration enabled")
	} else if config.KVM {
		m.logger.Warn().Msg("KVM requested but not available, running without acceleration")
	}

	// Add networking
	if config.NetDevice != "" {
		// On macOS, we can't use bridge networking directly, so use user networking
		if isMacOS() {
			netArgs := []string{
				"-netdev", "user,id=net0",
				"-device", fmt.Sprintf("%s,netdev=net0", config.NetDevice),
			}
			args = append(args, netArgs...)
			m.logger.Info().Msg("Using user networking for macOS")
		} else {
			netArgs := []string{
				"-netdev", fmt.Sprintf("bridge,id=net0,br=%s", config.NetBridge),
				"-device", fmt.Sprintf("%s,netdev=net0,mac=52:54:00:12:34:56", config.NetDevice),
			}
			args = append(args, netArgs...)
		}
	}

	// Add CDROM if specified
	if config.CDROM != "" {
		args = append(args, "-cdrom", config.CDROM)
	}

	// Add VGA configuration
	if !config.Headless {
		if runtime.GOARCH == "arm64" {
			// For ARM64, use virtio-gpu instead of VGA
			args = append(args,
				"-device", "virtio-gpu-pci",
				"-display", "default")
			m.logger.Info().Msg("Using virtio-gpu for ARM64")
		} else {
			args = append(args, "-vga", config.VGA)
		}
	} else {
		args = append(args, "-display", "none")
	}

	// Add BIOS/UEFI if needed
	if config.UseBIOS {
		if runtime.GOARCH == "arm64" {
			args = append(args, "-bios", "/usr/share/qemu-efi-aarch64/QEMU_EFI.fd")
		} else {
			args = append(args, "-bios", "/usr/share/ovmf/OVMF.fd")
		}
	}

	// Add TPM if needed
	if config.UseTPM {
		args = append(args,
			"-chardev", "socket,id=chrtpm,path=/tmp/tpm-socket",
			"-tpmdev", "emulator,id=tpm0,chardev=chrtpm",
			"-device", "tpm-tis,tpmdev=tpm0",
		)
	}

	// Start QEMU process
	cmd := exec.CommandContext(ctx, qemuBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	m.logger.Debug().Strs("args", args).Msg("QEMU command")

	if err := cmd.Start(); err != nil {
		return errors.Errorf("failed to start QEMU: %w", err)
	}

	// Allow time for QEMU to start up and create the socket
	time.Sleep(2 * time.Second)

	// Connect to QMP
	monitor, err := qmp.NewSocketMonitor("unix", socketPath, 2*time.Second)
	if err != nil {
		return errors.Errorf("failed to create QMP monitor: %w", err)
	}

	if err := monitor.Connect(); err != nil {
		return errors.Errorf("failed to connect to QMP: %w", err)
	}

	// Create domain
	domain, err := qemu.NewDomain(monitor, config.Name)
	if err != nil {
		return errors.Errorf("failed to create domain: %w", err)
	}

	m.domains[config.Name] = &Domain{
		cmd:       cmd,
		qmpSocket: socketPath,
		pidFile:   pidFile,
		domain:    domain,
	}
	m.monitors[config.Name] = monitor

	m.logger.Info().Str("name", config.Name).Msg("VM created and connected")
	return nil
}

// CreateVM is a simple version using default values (for backward compatibility)
func (m *Manager) CreateVM(ctx context.Context, name string, cpu int, memoryMB int, diskPath string, machine string) error {
	m.logger.Info().
		Str("name", name).
		Int("cpu", cpu).
		Int("memoryMB", memoryMB).
		Str("diskPath", diskPath).
		Str("machine", machine).
		Msg("Creating VM")

	// Check if VM already exists
	if _, exists := m.domains[name]; exists {
		return fmt.Errorf("VM %s already exists", name)
	}

	// Create sockets directory if it doesn't exist
	socketsDir := filepath.Join(m.workDir, "sockets")
	if err := os.MkdirAll(socketsDir, 0755); err != nil {
		return fmt.Errorf("failed to create sockets directory: %w", err)
	}

	// Create VM configuration
	qmpSocket := filepath.Join(socketsDir, name+".sock")
	pidFile := filepath.Join(socketsDir, name+".pid")

	// Remove any existing socket or PID files
	_ = os.Remove(qmpSocket)
	_ = os.Remove(pidFile)

	// Basic QEMU arguments
	args := []string{
		"-name", name,
		"-machine", machine,
		"-m", fmt.Sprintf("%d", memoryMB),
		"-smp", fmt.Sprintf("%d", cpu),
		"-drive", fmt.Sprintf("file=%s,format=qcow2", diskPath),
		"-qmp", fmt.Sprintf("unix:%s,server,nowait", qmpSocket),
		"-pidfile", pidFile,
	}

	// Check if KVM is available
	if !m.hasKVM {
		m.logger.Warn().Msg("KVM requested but not available, running without acceleration")
	}

	// Add networking
	if runtime.GOOS == "darwin" {
		m.logger.Info().Msg("Using user networking for macOS")
		args = append(args,
			"-netdev", "user,id=net0",
			"-device", "virtio-net-pci,netdev=net0",
		)
	}

	// Add display for ARM64
	if runtime.GOARCH == "arm64" {
		m.logger.Info().Msg("Using virtio-gpu for ARM64")
		args = append(args,
			"-device", "virtio-gpu-pci",
			"-display", "default",
		)
	}

	m.logger.Debug().Strs("args", args).Msg("QEMU command")

	// Create VM configuration file
	configPath := filepath.Join(m.workDir, name+".conf")
	config := fmt.Sprintf(`name=%s
cpu=%d
memory=%d
disk=%s
qmp_socket=%s
pid_file=%s
`, name, cpu, memoryMB, diskPath, qmpSocket, pidFile)

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write VM config: %w", err)
	}

	// Start QEMU process
	cmd := exec.CommandContext(ctx, "qemu-system-"+runtime.GOARCH, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start QEMU: %w", err)
	}

	// Wait for PID file to be created
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Store domain information
	m.domains[name] = &Domain{
		cmd:       cmd,
		qmpSocket: qmpSocket,
		pidFile:   pidFile,
	}

	m.logger.Info().Str("name", name).Msg("VM created and connected")

	return nil
}

// StopVM stops a running VM
func (m *Manager) StopVM(ctx context.Context, name string) error {
	domain, exists := m.domains[name]
	if !exists {
		return errors.Errorf("VM %s does not exist", name)
	}

	m.logger.Info().Str("name", name).Msg("Stopping VM")

	// Use SystemPowerdown instead of Stop
	if err := domain.SystemPowerdown(); err != nil {
		return errors.Errorf("failed to stop VM: %w", err)
	}

	monitor, exists := m.monitors[name]
	if exists {
		monitor.Disconnect()
	}

	delete(m.domains, name)
	delete(m.monitors, name)

	m.logger.Info().Str("name", name).Msg("VM stopped")
	return nil
}

// CreateDisk creates a QCOW2 disk image
func (m *Manager) CreateDisk(ctx context.Context, path string, sizeGB int) error {
	m.logger.Info().Str("path", path).Int("sizeGB", sizeGB).Msg("Creating disk image")

	cmd := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", path, fmt.Sprintf("%dG", sizeGB))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("failed to create disk: %w - %s", err, string(output))
	}

	m.logger.Info().Str("path", path).Msg("Disk created")
	return nil
}

// DownloadCloudStackTemplate downloads a CloudStack system VM template
func (m *Manager) DownloadCloudStackTemplate(ctx context.Context, templateURL string, destPath string) error {
	m.logger.Info().Str("url", templateURL).Str("dest", destPath).Msg("Downloading CloudStack template")

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return errors.Errorf("failed to create destination directory: %w", err)
	}

	// Use curl to download the file
	cmd := exec.CommandContext(ctx, "curl", "-L", "-o", destPath, templateURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return errors.Errorf("failed to download template: %w", err)
	}

	m.logger.Info().Str("dest", destPath).Msg("Template downloaded")
	return nil
}

// IsSocketActive checks if a QMP socket is active and usable
func IsSocketActive(socketPath string) bool {
	_, err := net.Dial("unix", socketPath)
	return err == nil
}

// isKVMAvailable checks if KVM is available
func isKVMAvailable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

// GetVMStatus returns the status of a VM
func (m *Manager) GetVMStatus(ctx context.Context, name string) (string, error) {
	domain, exists := m.domains[name]
	if !exists {
		return "", fmt.Errorf("VM %s not found", name)
	}

	status, err := domain.Status()
	if err != nil {
		return "", fmt.Errorf("failed to get VM status: %w", err)
	}

	return status.String(), nil
}

// ListRunningVMs returns a list of running VM names
func (m *Manager) ListRunningVMs() ([]string, error) {
	vms := make([]string, 0)
	files, err := os.ReadDir(m.workDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read working directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.HasSuffix(file.Name(), ".pid") {
			vmName := strings.TrimSuffix(file.Name(), ".pid")
			vms = append(vms, vmName)
		}
	}
	return vms, nil
}

// GetVMInfo returns detailed information about a VM
func (m *Manager) GetVMInfo(ctx context.Context, vmName string) (*VMInfo, error) {
	// Read the VM configuration file
	configPath := filepath.Join(m.workDir, vmName+".conf")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read VM config: %w", err)
	}

	// Parse the configuration
	config := make(map[string]string)
	lines := strings.Split(string(configData), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		config[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	// Extract VM information
	info := &VMInfo{
		CPUs:     4, // Default values for now
		MemoryMB: 4096,
		VNCPort:  config["vnc_port"],
		QMPPort:  config["qmp_port"],
	}

	return info, nil
}
