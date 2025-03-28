package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Determine architecture and platform-specific settings
	arch := ""
	machine := ""
	nic := ""
	efi := ""

	switch strings.ToLower(filepath.Base(m.QemuPath)) {
	case "qemu-system-aarch64":
		arch = "aarch64"
	case "qemu-system-x86_64":
		arch = "x86_64"
	default:
		return errors.Errorf("unsupported QEMU architecture")
	}

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
		nic = "user,hostfwd=tcp::2222-:22"
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
		"-hda", vm.DiskPath(),
		"-drive", fmt.Sprintf("file=%s,driver=raw,if=virtio", vm.CIDataPath()),
	}

	// Launch VM
	cmd := exec.CommandContext(ctx, m.QemuPath, qemuArgs...)

	logfile, err := os.Create(vm.QEMULogPath())
	if err != nil {
		return errors.Errorf("creating qemu log file: %w", err)
	}
	defer logfile.Close()
	cmd.Stdout = logfile
	cmd.Stderr = logfile

	// TODO: Handle VM output and PID
	if err := cmd.Start(); err != nil {
		return errors.Errorf("starting VM: %w", err)
	}

	vm.Status = "running"
	if cmd.Process == nil {
		return errors.Errorf("failed to get VM PID")
	}
	vm.PID = cmd.Process.Pid

	if err := vm.SaveState(); err != nil {
		return errors.Errorf("saving VM state: %w", err)
	}

	// TODO: Wait for VM to boot and collect SSH info

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
	entries, err := os.ReadDir(vmsDir())
	if err != nil {
		return nil, errors.Errorf("reading VMs directory: %w", err)
	}

	for _, entry := range entries {
		if entry.Name() == name {
			return &VM{
				Config: VMConfig{
					Name: entry.Name(),
				},
			}, nil
		}
	}

	return nil, errors.Errorf("not implemented")
}

// ListVMs lists all available VMs
func (m *LocalManager) ListVMs(ctx context.Context) ([]*VM, error) {
	entries, err := os.ReadDir(vmsDir())
	if err != nil {
		return nil, errors.Errorf("reading VMs directory: %w", err)
	}

	vms := make([]*VM, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			vms = append(vms, &VM{
				Config: VMConfig{
					Name: entry.Name(),
				},
			})
		}
	}

	return vms, nil
}
