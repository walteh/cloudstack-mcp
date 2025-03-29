package vm

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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
	Name     string
	ID       int
	CPUs     int
	Memory   string // e.g., "2G"
	DiskSize string // e.g., "20G"
	BaseImg  Img
	Network  NetworkConfig
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
	PID       int               `json:"pid,omitempty"`
	Status    string            `json:"status"` // "created", "initializing", "ready", "started", "stopped", "failed", "deleted"
	LastError string            `json:"last_error,omitempty"`
	Config    VMConfig          `json:"config"`
	SSHInfo   SSHInfo           `json:"ssh_info"`
	MetaData  map[string]string `json:"meta_data"`
}

// // VM represents a running virtual machine instance
// type VM struct {
// 	Config        VMConfig
// 	SSHInfo       SSHInfo
// 	diskOutput    string
// 	internalState string
// }

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

func (me *VM) Process() (*os.Process, error) {
	pid := me.PID
	if pid <= 0 {
		return nil, errors.Errorf("invalid PID for VM: %d", pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return nil, errors.Errorf("finding VM process: %w", err)
	}

	// if err := process.Signal(syscall.Signal(0)); err != nil {
	// 	return nil, errors.Errorf("process is not running: %w", err)
	// }

	return process, nil
}

// SaveState saves the VM's state to disk
func (vm *VM) SaveState() error {
	if err := os.MkdirAll(vm.Dir(), 0755); err != nil {
		return errors.Errorf("creating VM directory: %w", err)
	}

	// state := VMState{
	// 	Name:   vm.Config.Name,
	// 	Status: vm.internalState,
	// }

	// // Only save PID if the VM is running
	// if vm.internalState == "running" {
	// 	pid, err := vm.GetPID()
	// 	if err == nil && pid > 0 {
	// 		state.PID = pid
	// 	}
	// }

	data, err := json.MarshalIndent(vm, "", "  ")
	if err != nil {
		return errors.Errorf("marshaling VM state: %w", err)
	}

	if err := os.WriteFile(vm.StateFilePath(), data, 0644); err != nil {
		return errors.Errorf("writing VM state file: %w", err)
	}

	return nil
}

// LoadState loads the VM's state from disk
// func (vm *VM) LoadState() error {
// 	data, err := os.ReadFile(vm.StateFilePath())
// 	if err != nil {
// 		if os.IsNotExist(err) {
// 			// If the state file doesn't exist, assume the VM is stopped
// 			vm.Status = "stopped"
// 			return nil
// 		}
// 		return errors.Errorf("reading VM state file: %w", err)
// 	}

// 	var state VMState
// 	if err := json.Unmarshal(data, &state); err != nil {
// 		return errors.Errorf("unmarshaling VM state: %w", err)
// 	}

// 	vm.internalState = state.Status

// 	// Verify that the process is still running if state is "running"
// 	if state.Status == "running" && state.PID > 0 {
// 		if isProcessRunning(state.PID) {
// 			// Process is still running
// 			vm.internalState = "running"
// 		} else {
// 			// Process is not running, update state
// 			vm.internalState = "stopped"
// 			vm.SaveState()
// 		}
// 	}

// 	return nil
// }

// // Status returns the current VM status
// func (vm *VM) Status() (string, error) {
// 	err := vm.LoadState()
// 	if err != nil {
// 		return "unknown", err
// 	}

// 	return vm.internalState, nil
// }

// // SetStatus sets the VM status and saves it
// func (vm *VM) SetStatus(status string) error {
// 	vm.internalState = status
// 	return vm.SaveState()
// }

// // GetPID returns the process ID of the VM if it's running
// func (vm *VM) GetPID() (int, error) {
// 	data, err := os.ReadFile(vm.StateFilePath())
// 	if err != nil {
// 		if os.IsNotExist(err) {
// 			return 0, nil
// 		}
// 		return 0, errors.Errorf("reading VM state file: %w", err)
// 	}

// 	var state VMState
// 	if err := json.Unmarshal(data, &state); err != nil {
// 		return 0, errors.Errorf("unmarshaling VM state: %w", err)
// 	}

// 	return state.PID, nil
// }

// // SetPID saves the VM's PID to disk
// func (vm *VM) SetPID(pid int) error {
// 	data, err := os.ReadFile(vm.StateFilePath())
// 	if err != nil && !os.IsNotExist(err) {
// 		return errors.Errorf("reading VM state file: %w", err)
// 	}

// 	var state VMState
// 	if err == nil {
// 		if err := json.Unmarshal(data, &state); err != nil {
// 			return errors.Errorf("unmarshaling VM state: %w", err)
// 		}
// 	} else {
// 		// State file doesn't exist, create a new one
// 		state = VMState{
// 			Name:   vm.Config.Name,
// 			Status: vm.internalState,
// 		}
// 	}

// 	state.PID = pid

// 	data, err = json.MarshalIndent(state, "", "  ")
// 	if err != nil {
// 		return errors.Errorf("marshaling VM state: %w", err)
// 	}

// 	if err := os.WriteFile(vm.StateFilePath(), data, 0644); err != nil {
// 		return errors.Errorf("writing VM state file: %w", err)
// 	}

// 	return nil
// }

func (vm *VM) QEMULogPath() string {
	return filepath.Join(vm.Dir(), "qemu.log")
}

// Stop attempts to stop the VM process
func (vm *VM) Stop() error {
	if vm.PID <= 0 {
		return errors.Errorf("invalid PID for VM: %d", vm.PID)
	}

	// Check if the process exists before trying to kill it
	if !isProcessRunning(vm.PID) {
		// Process is already gone, just update the status
		if vm.Status == VMStatusInitializing {
			vm.Status = VMStatusFailed
		} else {
			vm.Status = VMStatusStopped
		}
		return vm.SaveState()
	}

	process, err := os.FindProcess(vm.PID)
	if err != nil {
		return errors.Errorf("finding VM process: %w", err)
	}

	// Signal the process to terminate
	if err := process.Kill(); err != nil {
		return errors.Errorf("killing VM process: %w", err)
	}

	// Update the state based on current state
	if vm.Status == VMStatusInitializing {
		// If we stopped during initialization, mark as failed
		vm.Status = VMStatusFailed
	} else {
		// Otherwise, mark as stopped
		vm.Status = VMStatusStopped
	}

	return vm.SaveState()
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

// isProcessRunning checks if a process with the given PID is running
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// First, try using the standard FindProcess approach
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Try to signal the process, but this can be unreliable on macOS
	if err = process.Signal(os.Signal(nil)); err == nil {
		return true
	}

	// Fallback: Check if the process exists using the 'ps' command
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid))
	if err := cmd.Run(); err == nil {
		// Process exists
		return true
	}

	return false
}

// GetStatus dynamically checks the status of the VM rather than relying on saved state
func (vm *VM) GetStatus() string {
	// If the VM is in initializing or failed state, don't try to check running state
	if vm.Status == VMStatusInitializing || vm.Status == VMStatusFailed {
		return vm.Status
	}

	// First try the PID-based check
	if vm.PID > 0 && isProcessRunning(vm.PID) {
		// If we're running but we weren't in "started" state, update it
		if vm.Status != VMStatusStarted {
			vm.Status = VMStatusStarted
			vm.SaveState()
		}
		return VMStatusStarted
	}

	// If that fails, check for QEMU processes with this VM's name
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err == nil {
		outputStr := string(output)
		// Look for QEMU processes with this VM's name in the command line
		if strings.Contains(outputStr, "qemu") && strings.Contains(outputStr, vm.Name) {
			// Parse PID from output
			lines := strings.Split(outputStr, "\n")
			for _, line := range lines {
				if strings.Contains(line, "qemu") && strings.Contains(line, vm.Name) {
					fields := strings.Fields(line)
					if len(fields) > 1 {
						pid, err := strconv.Atoi(fields[1])
						if err == nil && pid > 0 {
							// Update the PID
							vm.PID = pid
							vm.Status = VMStatusStarted
							vm.SaveState()
							return VMStatusStarted
						}
					}
				}
			}
		}
	}

	// No running processes found, but if the status is already set to "started"
	// or "ready", maintain that status - this is important for VMs that started
	// successfully and completed cloud-init, but don't have a detectable process
	if vm.Status == VMStatusStarted || vm.Status == VMStatusReady {
		return vm.Status
	}

	// Otherwise assume we're stopped
	if vm.Status != VMStatusStopped && vm.Status != VMStatusCreated && vm.Status != VMStatusDeleted {
		vm.Status = VMStatusStopped
		vm.SaveState()
	}

	return vm.Status
}
