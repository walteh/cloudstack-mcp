package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/tmux"
	"gitlab.com/tozd/go/errors"
)

type Img struct {
	Name string
	Url  string
}

type CloudInit struct {
	UserData      string
	NetworkConfig string
	MetaData      string
}

// VMConfig represents the configuration for a virtual machine
type VMConfig struct {
	Name      string
	ID        int
	CPUs      int
	Memory    string // e.g., "2G"
	DiskSize  string // e.g., "20G"
	BaseImg   Img
	Network   NetworkConfig
	ExtraArgs []string // Additional QEMU arguments
}

// NetworkConfig represents the network configuration for a VM
type NetworkConfig struct {
	Type     string // e.g., "vmnet-shared", "tap"
	MAC      string
	IPRange  string // e.g., "192.168.1.1,192.168.1.20"
	Subnet   string // e.g., "255.255.255.0"
	Hostname string
}

// VMState represents the persistent state of a VM
type VM struct {
	Name      string            `json:"name"`
	LastError string            `json:"last_error,omitempty"`
	Config    VMConfig          `json:"config"`
	SSHInfo   SSHInfo           `json:"ssh_info"`
	MetaData  map[string]string `json:"meta_data"`
	VNCPort   int               `json:"vnc_port,omitempty"` // VNC port for this VM if enabled
}

// SSHInfo contains information needed to SSH into the VM
type SSHInfo struct {
	Username   string `json:"username"`
	PrivateKey string `json:"private_key"`
	Password   string `json:"password,omitempty"` // Optional password for SSH authentication
	Host       string `json:"host"`
	Port       int    `json:"port"`
}

// Dir returns the VM's directory
func (vm *VM) Dir() string {
	return filepath.Join(vmsDir(), vm.Config.Name)
}

// DiskPath returns the path to the VM's disk image
func (vm *VM) DiskPath() string {
	return filepath.Join(vm.Dir(), vm.Config.Name+".qcow2")
}

// CIDataPath returns the path to the VM's cloud-init data ISO
func (vm *VM) CIDataPath() string {
	return filepath.Join(vm.Dir(), "cidata.iso")
}

// StateFilePath returns the path to the VM's state file
func (vm *VM) StateFilePath() string {
	return filepath.Join(vm.Dir(), "vm-state.json")
}

// SaveState saves the VM's state to disk
func (vm *VM) SaveState() error {
	if err := os.MkdirAll(vm.Dir(), 0755); err != nil {
		return errors.Errorf("creating VM directory: %w", err)
	}

	data, err := json.MarshalIndent(vm, "", "  ")
	if err != nil {
		return errors.Errorf("marshaling VM state: %w", err)
	}

	if err := os.WriteFile(vm.StateFilePath(), data, 0644); err != nil {
		return errors.Errorf("writing VM state file: %w", err)
	}

	return nil
}

// Stop attempts to stop the VM process
func (vm *VM) Stop(ctx context.Context, tmuxManager *tmux.SessionManager) error {

	if err := tmuxManager.CloseVMWindow(ctx, vm.Name+"-root"); err != nil {
		return errors.Errorf("closing VM root window: %w", err)
	}

	// Clear the PID
	return nil
}

func (vm *VM) Start(ctx context.Context, mgr *tmux.SessionManager) error {

	logger := zerolog.Ctx(ctx)

	if isRunning, err := VMIsRunning(ctx, mgr, vm); err != nil {
		return errors.Errorf("checking if VM is running: %w", err)
	} else if isRunning {
		return errors.Errorf("VM is already running")
	}

	logger.Info().Str("name", vm.Config.Name).Msg("Starting VM process")

	// Make sure the VM name is set
	// Make sure the VM name is set
	vm.Name = vm.Config.Name

	// Determine architecture and platform-specific settings
	imageName := vm.Config.BaseImg.Name
	arch := DetectArchitecture(imageName)
	isAppleSilicon := IsAppleSilicon()

	// Set the appropriate QEMU path based on architecture
	qemuPath := GetQEMUPath(arch)

	// Get machine settings and EFI path
	machine := GetMachineSetting(arch, isAppleSilicon)
	efi := GetEFIPath(arch)

	logger.Debug().
		Str("arch", arch).
		Str("qemu", qemuPath).
		Str("machine", machine).
		Str("efi", efi).
		Msg("VM hardware configuration")

	// Check if disk and cidata ISO exist
	diskPath := vm.DiskPath()
	ciDataPath := vm.CIDataPath()

	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		return errors.Errorf("VM disk does not exist: %s", diskPath)
	}

	if _, err := os.Stat(ciDataPath); os.IsNotExist(err) {
		return errors.Errorf("cloud-init ISO does not exist: %s", ciDataPath)
	}

	// Generate a unique MAC address for this VM
	macAddress := vm.GetMACAddress()

	// Prepare the CPU and QEMU flags
	cpuType := GetCPUType(arch, isAppleSilicon)

	// Set network configuration
	networkDevice := "virtio-net-device"
	if arch == "aarch64" && isAppleSilicon {
		networkDevice = "virtio-net-pci"
	}

	// Generate a unique VNC port to avoid conflicts
	vncPort := 5900 + (rand.Intn(100) + 1) // Random port between 5901 and 6000
	vm.VNCPort = vncPort

	// Prepare QEMU command with appropriate flags
	qemuArgs := []string{
		"-name", vm.Config.Name,
		"-machine", machine,
		"-cpu", cpuType,
		"-smp", fmt.Sprintf("%d", vm.Config.CPUs),
		"-m", vm.Config.Memory,
		"-drive", fmt.Sprintf("file=%s,if=virtio,cache=none,discard=unmap", diskPath),
		"-drive", fmt.Sprintf("file=%s,if=virtio,media=cdrom", ciDataPath),
		"-boot", "order=cd,once=d", // Boot from cdrom first time
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp::%d-:22", vm.SSHPort()),
		"-device", fmt.Sprintf("%s,netdev=net0,mac=%s", networkDevice, macAddress),
		"-vnc", fmt.Sprintf("127.0.0.1:%d", vncPort-5900),
		"-device", "virtio-rng-pci", // Random number generator to speed up boot
		"-nographic", // No GUI
	}

	// Add EFI if available
	if efi != "" {
		qemuArgs = append(qemuArgs, "-bios", efi)
	}

	// Add any additional arguments from VM config
	if len(vm.Config.ExtraArgs) > 0 {
		qemuArgs = append(qemuArgs, vm.Config.ExtraArgs...)
	}

	// Log the full command
	qemuCommand := qemuPath + " " + strings.Join(qemuArgs, " ")
	logger.Debug().Str("command", qemuCommand).Msg("Starting QEMU with command")

	// Set VM SSH info
	vm.SSHInfo.Host = "localhost"
	vm.SSHInfo.Port = vm.SSHPort()
	vm.SSHInfo.Username = "ubuntu"

	// Generate random key name for this VM
	keyName := fmt.Sprintf("vm_%s_key", vm.Name)
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".ssh", keyName)); os.IsNotExist(err) {
		// Generate a new SSH key for this VM
		pubKey, privKey, err := GenerateSSHKey()
		if err != nil {
			return errors.Errorf("generating SSH key: %w", err)
		}

		// Save the public key in the VM's metadata for future reference
		vm.MetaData["ssh_public_key"] = pubKey

		// Save the private key to the VM's directory
		keyPath := filepath.Join(vm.Dir(), keyName)
		if err := os.WriteFile(keyPath, []byte(privKey), 0600); err != nil {
			return errors.Errorf("writing private key: %w", err)
		}

		// Set the private key path in the VM
		vm.SSHInfo.PrivateKey = keyPath
	} else {
		// Use existing key
		vm.SSHInfo.PrivateKey = filepath.Join(os.Getenv("HOME"), ".ssh", keyName)
	}

	// Save updated VM state
	if err := vm.SaveState(); err != nil {
		return errors.Errorf("saving VM state: %w", err)
	}

	// Create a tmux window for the VM if tmux manager is available
	// Create a window for the VM
	if err := mgr.CreateVMWindow(ctx, vm.Name+"-root"); err != nil {
		return errors.Errorf("creating tmux window for VM: %w", err)
	}

	// Run QEMU in the VM's window
	if err := mgr.RunCommand(ctx, vm.Name+"-root", qemuCommand); err != nil {
		return errors.Errorf("running QEMU command in tmux window: %w", err)
	}

	logger.Info().Str("name", vm.Name).Msg("VM started in tmux window")
	fmt.Printf("VM %s started in tmux window\n", vm.Name)
	fmt.Printf("Use 'vmctl attach' to view and manage all VMs\n")

	return nil
}

func (vm *VM) Wait(ctx context.Context, mgr *tmux.SessionManager) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Msg("Waiting for VM to be ready")

	// Wait for VM to be running
	if isRunning, err := VMIsRunning(ctx, mgr, vm); err != nil {
		return errors.Errorf("checking if VM is running: %w", err)
	} else if !isRunning {
		return errors.Errorf("VM is not running")
	}

	for i := 0; i < 10; i++ {
		// Try to connect via SSH
		sshClient, err := vm.ConnectSSH(ctx, mgr)
		if err != nil {
			if i == 9 {
				return errors.Errorf("waiting for SSH: %w", err)
			}
			time.Sleep(1 * time.Second)
			continue
		}
		defer sshClient.Close()
		return nil
	}

	return errors.Errorf("failed to connect to VM via SSH")
}

func (vm *VM) Exec(ctx context.Context, mgr *tmux.SessionManager, command []string) (string, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("name", vm.Config.Name).Strs("command", command).Msg("Executing command on VM")

	// Combine command args into a single string
	cmdStr := strings.Join(command, " ")

	// Fall back to direct SSH if tmux is not available or failed
	sshClient, err := vm.ConnectSSH(ctx, mgr)
	if err != nil {
		return "", errors.Errorf("connecting to VM via SSH: %w", err)
	}
	defer sshClient.Close()

	// Create a new SSH session
	session, err := sshClient.NewSession()
	if err != nil {
		return "", errors.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	// Connect session to stdin/stdout/stderr
	session.Stdin = os.Stdin

	// Run the command
	output, err := session.CombinedOutput(cmdStr)
	if err != nil {
		return "", errors.Errorf("running command: %w", err)
	}

	return string(output), nil
}

func (vm *VM) QEMULogPath() string {
	return filepath.Join(vm.Dir(), "qemu.log")
}

// baseDir returns the base directory for VM data
func baseDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("getting user home directory: %s", err))
	}
	return filepath.Join(homeDir, ".cloudstack-mcp")
}

// vmsDir returns the directory for VM data
func vmsDir() string {
	return filepath.Join(baseDir(), "vms")
}

// imagesDir returns the directory for VM images
func imagesDir() string {
	return filepath.Join(baseDir(), "images")
}

// GetMACAddress returns a MAC address for the VM
func (vm *VM) GetMACAddress() string {
	if vm.Config.Network.MAC != "" {
		return vm.Config.Network.MAC
	}
	// Generate a random MAC address with the qemu prefix (52:54:00)
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x",
		rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

// SSHPort returns the SSH port for the VM
func (vm *VM) SSHPort() int {
	if vm.SSHInfo.Port > 0 {
		return vm.SSHInfo.Port
	}
	// Generate a random port between 10000 and 20000
	return 10000 + rand.Intn(10000)
}

// IsRunning checks if the VM process is currently running
func VMIsRunning(ctx context.Context, tmuxManager *tmux.SessionManager, vm *VM) (bool, error) {
	hasWindow, err := tmuxManager.HasVM(ctx, vm.Name+"-root")
	if err != nil {
		return false, errors.Errorf("checking for VM window: %w", err)
	}
	return hasWindow, nil
}
