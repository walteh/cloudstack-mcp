package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
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
}

// LocalManager implements Manager for local QEMU VMs
type LocalManager struct {
	QemuPath    string
	MkisofsPath string
	QemuImgPath string
}

// NewLocalManager creates a new LocalManager
func NewLocalManager() (*LocalManager, error) {

	// Find required executables
	qemuPath, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		// Try ARM architecture
		qemuPath, err = exec.LookPath("qemu-system-aarch64")
		if err != nil {
			return nil, errors.Errorf("finding qemu executable: %w", err)
		}
	}

	mkisofsPath, err := exec.LookPath("mkisofs")
	if err != nil {
		return nil, errors.Errorf("finding mkisofs executable: %w", err)
	}

	qemuImgPath, err := exec.LookPath("qemu-img")
	if err != nil {
		return nil, errors.Errorf("finding qemu-img executable: %w", err)
	}

	return &LocalManager{
		QemuPath:    qemuPath,
		MkisofsPath: mkisofsPath,
		QemuImgPath: qemuImgPath,
	}, nil
}

const (
	diskName = "disk.img"
)

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

	// Create VM object
	// vm := &VM{
	// 	Config:     config,
	// 	diskOutput: string(output),
	// 	SSHInfo: SSHInfo{
	// 		Username:   "ubuntu", // Default for Ubuntu cloud images
	// 		PrivateKey: "",       // Will be set when VM starts
	// 		Host:       "",       // Will be set when VM starts
	// 		Port:       22,
	// 	},
	// }
	vm.MetaData["disk_output"] = string(output)

	metaData, err := vm.BuildMetaData()
	if err != nil {
		return nil, errors.Errorf("generating meta-data: %w", err)
	}

	userData, err := vm.UserData()
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

	vm.Status = "created"

	if err := vm.SaveState(); err != nil {
		return nil, errors.Errorf("saving VM state: %w", err)
	}

	return vm, nil
}

// StartVM starts a VM
func (m *LocalManager) StartVM(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Starting VM")

	// Make sure the VM name is set
	vm.Name = vm.Config.Name

	// Determine architecture and platform-specific settings
	arch := ""
	machine := ""
	nic := ""
	efi := ""

	// Detect architecture from image name instead of QEMU binary
	imageName := vm.Config.BaseImg.Name
	if strings.Contains(imageName, "arm64") || strings.Contains(imageName, "aarch64") {
		arch = "aarch64"
		m.QemuPath = "qemu-system-aarch64"
	} else if strings.Contains(imageName, "amd64") || strings.Contains(imageName, "x86_64") {
		arch = "x86_64"
		m.QemuPath = "qemu-system-x86_64"
	} else {
		// Default to host architecture
		switch strings.ToLower(filepath.Base(m.QemuPath)) {
		case "qemu-system-aarch64":
			arch = "aarch64"
		case "qemu-system-x86_64":
			arch = "x86_64"
		default:
			return errors.Errorf("unsupported QEMU architecture")
		}
	}

	logger.Debug().Str("arch", arch).Str("qemu", m.QemuPath).Msg("Detected architecture")

	switch arch {
	case "aarch64":
		if _, err := os.Stat("/opt/homebrew/share/qemu/edk2-aarch64-code.fd"); err == nil {
			efi = "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"
			machine = "virt,accel=hvf,highmem=on"
		} else {
			efi = "/usr/share/qemu/edk2-aarch64-code.fd"
			machine = "virt,accel=kvm"
		}
	case "x86_64":
		if _, err := os.Stat("/opt/homebrew/share/qemu/edk2-x86_64-code.fd"); err == nil {
			efi = "/opt/homebrew/share/qemu/edk2-x86_64-code.fd"
			machine = "q35,accel=hvf"
		} else {
			efi = "/usr/share/qemu/OVMF.fd"
			machine = "q35,accel=kvm"
		}
	}

	logger.Debug().
		Str("machine", machine).
		Str("efi", efi).
		Msg("VM hardware configuration")

	// Check if disk and cidata ISO exist
	diskPath := vm.DiskPath()
	ciDataPath := vm.CIDataPath()

	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		return errors.Errorf("VM disk image does not exist: %s", diskPath)
	}

	if _, err := os.Stat(ciDataPath); os.IsNotExist(err) {
		return errors.Errorf("VM cloud-init ISO does not exist: %s", ciDataPath)
	}

	logger.Debug().
		Str("disk", diskPath).
		Str("cidata", ciDataPath).
		Msg("VM disk configuration")

	// Set up networking based on platform
	switch vm.Config.Network.Type {
	case "vmnet-shared":
		nic = fmt.Sprintf("vmnet-shared,start-address=%s,subnet-mask=%s",
			vm.Config.Network.IPRange,
			vm.Config.Network.Subnet)
	case "tap":
		// TODO: Implement tap script handling
		nic = "tap"
	default:
		// Default to user mode networking with port forwarding
		// Use a dynamic port to avoid conflicts
		port, err := findAvailablePort()
		if err != nil {
			return errors.Errorf("finding available port: %w", err)
		}
		nic = fmt.Sprintf("user,hostfwd=tcp::%d-:22", port)
		vm.SSHInfo.Host = "localhost"
		vm.SSHInfo.Port = port
	}

	logger.Debug().
		Str("nic", nic).
		Str("mac", vm.Config.Network.MAC).
		Msg("VM network configuration")

	// Ensure SSH info is set
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
	}

	// Prepare QEMU command
	qemuArgs := []string{
		"-nographic",
		"-machine", machine,
		"-cpu", "host",
		"-smp", fmt.Sprintf("%d", vm.Config.CPUs),
		"-m", vm.Config.Memory,
		"-bios", efi,
		"-nic", fmt.Sprintf("%s,mac=%s", nic, vm.Config.Network.MAC),
		"-hda", diskPath,
		"-drive", fmt.Sprintf("file=%s,driver=raw,if=virtio", ciDataPath),
	}

	logger.Debug().
		Strs("args", qemuArgs).
		Msg("QEMU command arguments")

	// Create a log file for QEMU output
	logFile, err := os.Create(vm.QEMULogPath())
	if err != nil {
		return errors.Errorf("creating QEMU log file: %w", err)
	}

	// Try running QEMU and capturing output
	cmd := exec.CommandContext(ctx, m.QemuPath, qemuArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	logger.Info().
		Str("command", m.QemuPath).
		Strs("args", qemuArgs).
		Str("log_file", vm.QEMULogPath()).
		Msg("Starting QEMU process")

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return errors.Errorf("starting VM: %w", err)
	}

	// Update VM state
	vm.Status = "running"
	vm.PID = cmd.Process.Pid

	// Save the state
	if err := vm.SaveState(); err != nil {
		logger.Error().Err(err).Msg("Failed to save VM state")
		// Continue anyway, as the VM is running
	}

	logger.Info().
		Str("name", vm.Name).
		Int("pid", vm.PID).
		Str("ssh_host", vm.SSHInfo.Host).
		Int("ssh_port", vm.SSHInfo.Port).
		Msg("VM started successfully")

	return nil
}

// StartVMForeground starts a VM in foreground mode for testing
func (m *LocalManager) StartVMForeground(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Starting VM in foreground")

	// Make sure the VM name is set
	vm.Name = vm.Config.Name

	// Determine architecture and platform-specific settings
	arch := ""
	machine := ""
	nic := ""
	efi := ""

	// Detect architecture from image name instead of QEMU binary
	imageName := vm.Config.BaseImg.Name
	if strings.Contains(imageName, "arm64") || strings.Contains(imageName, "aarch64") {
		arch = "aarch64"
		m.QemuPath = "qemu-system-aarch64"
	} else if strings.Contains(imageName, "amd64") || strings.Contains(imageName, "x86_64") {
		arch = "x86_64"
		m.QemuPath = "qemu-system-x86_64"
	} else {
		// Default to host architecture
		switch strings.ToLower(filepath.Base(m.QemuPath)) {
		case "qemu-system-aarch64":
			arch = "aarch64"
		case "qemu-system-x86_64":
			arch = "x86_64"
		default:
			return errors.Errorf("unsupported QEMU architecture")
		}
	}

	logger.Debug().Str("arch", arch).Str("qemu", m.QemuPath).Msg("Detected architecture")

	switch arch {
	case "aarch64":
		if _, err := os.Stat("/opt/homebrew/share/qemu/edk2-aarch64-code.fd"); err == nil {
			efi = "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"
			machine = "virt,accel=hvf,highmem=on"
		} else {
			efi = "/usr/share/qemu/edk2-aarch64-code.fd"
			machine = "virt,accel=kvm"
		}
	case "x86_64":
		if _, err := os.Stat("/opt/homebrew/share/qemu/edk2-x86_64-code.fd"); err == nil {
			efi = "/opt/homebrew/share/qemu/edk2-x86_64-code.fd"
			machine = "q35,accel=hvf"
		} else {
			efi = "/usr/share/qemu/OVMF.fd"
			machine = "q35,accel=kvm"
		}
	}

	logger.Debug().
		Str("machine", machine).
		Str("efi", efi).
		Msg("VM hardware configuration")

	// Check if disk and cidata ISO exist
	diskPath := vm.DiskPath()
	ciDataPath := vm.CIDataPath()

	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		return errors.Errorf("VM disk image does not exist: %s", diskPath)
	}

	if _, err := os.Stat(ciDataPath); os.IsNotExist(err) {
		return errors.Errorf("VM cloud-init ISO does not exist: %s", ciDataPath)
	}

	logger.Debug().
		Str("disk", diskPath).
		Str("cidata", ciDataPath).
		Msg("VM disk configuration")

	// Set up networking based on platform
	switch vm.Config.Network.Type {
	case "vmnet-shared":
		nic = fmt.Sprintf("vmnet-shared,start-address=%s,subnet-mask=%s",
			vm.Config.Network.IPRange,
			vm.Config.Network.Subnet)
	case "tap":
		// TODO: Implement tap script handling
		nic = "tap"
	default:
		// Default to user mode networking with port forwarding
		// Use a dynamic port to avoid conflicts
		port, err := findAvailablePort()
		if err != nil {
			return errors.Errorf("finding available port: %w", err)
		}
		nic = fmt.Sprintf("user,hostfwd=tcp::%d-:22", port)
		vm.SSHInfo.Host = "localhost"
		vm.SSHInfo.Port = port
	}

	logger.Debug().
		Str("nic", nic).
		Str("mac", vm.Config.Network.MAC).
		Msg("VM network configuration")

	// Ensure SSH info is set
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
	}

	// Prepare QEMU command
	qemuArgs := []string{
		"-nographic",
		"-machine", machine,
		"-cpu", "host",
		"-smp", fmt.Sprintf("%d", vm.Config.CPUs),
		"-m", vm.Config.Memory,
		"-bios", efi,
		"-nic", fmt.Sprintf("%s,mac=%s", nic, vm.Config.Network.MAC),
		"-hda", diskPath,
		"-drive", fmt.Sprintf("file=%s,driver=raw,if=virtio", ciDataPath),
		// Add debugging options
		"-d", "guest_errors,unimp",
		"-D", vm.QEMULogPath(),
	}

	logger.Debug().
		Strs("args", qemuArgs).
		Msg("QEMU command arguments")

	// Update VM state before running (in case we want to kill it)
	vm.Status = "running"
	// We can't set a real PID yet since the process hasn't started
	vm.SaveState()

	// Run QEMU in foreground
	cmd := exec.CommandContext(ctx, m.QemuPath, qemuArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Info().
		Str("command", m.QemuPath).
		Strs("args", qemuArgs).
		Msg("Starting QEMU process in foreground")

	// This will block until QEMU exits
	err := cmd.Run()
	if err != nil {
		logger.Error().Err(err).Msg("QEMU exited with error")
		vm.Status = "error"
		vm.SaveState()
		return err
	}

	// QEMU has exited
	vm.Status = "stopped"
	vm.SaveState()

	logger.Info().
		Str("name", vm.Name).
		Msg("VM stopped")

	return nil
}

// StopVM stops a running VM
// func (m *LocalManager) StopVM(ctx context.Context, vm *VM) error {
// 	logger := zerolog.Ctx(ctx)
// 	logger.Info().Str("name", vm.Config.Name).Msg("Stopping VM")

// 	if vm.internalState != "running" {
// 		return errors.Errorf("VM is not running")
// 	}

// 	vm.internalState = "stopping"

// 	if err := vm.Stop(); err != nil {
// 		return errors.Errorf("stopping VM: %w", err)
// 	}

// 	vm.internalState = "stopped"

// 	return nil
// }

// DeleteVM deletes a VM and its associated resources
func (m *LocalManager) DeleteVM(ctx context.Context, vm *VM) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Deleting VM")

	if vm.Status == "running" {
		if err := vm.Stop(); err != nil {
			return errors.Errorf("stopping VM: %w", err)
		}
	}

	if err := os.RemoveAll(vm.Dir()); err != nil {
		return errors.Errorf("removing VM directory: %w", err)
	}

	return nil
}

// GetVM retrieves a VM by name
func (m *LocalManager) GetVM(ctx context.Context, name string) (*VM, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", name).Msg("Getting VM")

	vmDir := filepath.Join(vmsDir(), name)
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

	// Also check for QEMU processes that match this VM's name if we have no PID or
	// the process with our saved PID is not running
	if vm.GetStatus() != "running" {
		cmd := exec.Command("ps", "aux")
		output, err := cmd.Output()
		if err == nil {
			outputStr := string(output)
			// Look for QEMU processes with this VM's name in the command line
			if strings.Contains(outputStr, "qemu") && strings.Contains(outputStr, name) {
				// Parse PID from output
				lines := strings.Split(outputStr, "\n")
				for _, line := range lines {
					if strings.Contains(line, "qemu") && strings.Contains(line, name) {
						fields := strings.Fields(line)
						if len(fields) > 1 {
							pid, err := strconv.Atoi(fields[1])
							if err == nil && pid > 0 {
								logger.Info().Int("pid", pid).Msg("Found QEMU process for VM, updating PID")
								vm.PID = pid
								vm.SaveState()
								break
							}
						}
					}
				}
			}
		}
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

	entries, err := os.ReadDir(vmsDir())
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
