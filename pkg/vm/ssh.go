package vm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gitlab.com/tozd/go/errors"
)

// ExecShell opens an interactive shell to the VM via SSH
func (vm *VM) ExecShell() error {
	// Check if the VM is running
	process, err := os.FindProcess(vm.PID)
	if err != nil || process == nil {
		vm.Status = "stopped"
		vm.SaveState()
		return errors.Errorf("VM is not running")
	}

	// Check if the process is still running
	if err := process.Signal(os.Signal(nil)); err != nil {
		vm.Status = "stopped"
		vm.SaveState()
		return errors.Errorf("VM process is not running")
	}

	if vm.SSHInfo.Host == "" {
		// For user networking, default to localhost with port forwarding
		if vm.Config.Network.Type == "user" {
			vm.SSHInfo.Host = "localhost"
			vm.SSHInfo.Port = 2222
			vm.SaveState()
		} else {
			return errors.Errorf("VM SSH host information is not available")
		}
	}

	// Create a temporary SSH key file if needed
	var keyFile string
	if vm.SSHInfo.PrivateKey != "" {
		tmpKeyFile, err := os.CreateTemp("", "vm-ssh-key-*.pem")
		if err != nil {
			return errors.Errorf("creating temporary SSH key file: %w", err)
		}
		defer os.Remove(tmpKeyFile.Name())

		if err := os.WriteFile(tmpKeyFile.Name(), []byte(vm.SSHInfo.PrivateKey), 0600); err != nil {
			return errors.Errorf("writing SSH key: %w", err)
		}
		keyFile = tmpKeyFile.Name()
	} else {
		// If no private key in the VM object, try using the default SSH key
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Errorf("getting user home directory: %w", err)
		}
		keyFile = filepath.Join(homeDir, ".ssh", "id_rsa")
		if _, err := os.Stat(keyFile); os.IsNotExist(err) {
			return errors.Errorf("SSH key not found: %s", keyFile)
		}
	}

	// Build SSH command
	args := []string{
		"-i", keyFile,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-p", fmt.Sprintf("%d", vm.SSHInfo.Port),
	}

	// Add username and host
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
		vm.SaveState()
	}

	args = append(args, fmt.Sprintf("%s@%s", vm.SSHInfo.Username, vm.SSHInfo.Host))

	// Execute SSH command
	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// ExecCommand executes a command on the VM via SSH and returns the output
func (vm *VM) ExecCommand(command string) (string, error) {
	// Check if the VM is running
	process, err := os.FindProcess(vm.PID)
	if err != nil || process == nil {
		vm.Status = "stopped"
		vm.SaveState()
		return "", errors.Errorf("VM is not running")
	}

	// Check if the process is still running
	if err := process.Signal(os.Signal(nil)); err != nil {
		vm.Status = "stopped"
		vm.SaveState()
		return "", errors.Errorf("VM process is not running")
	}

	if vm.SSHInfo.Host == "" {
		// For user networking, default to localhost with port forwarding
		if vm.Config.Network.Type == "user" {
			vm.SSHInfo.Host = "localhost"
			vm.SSHInfo.Port = 2222
			vm.SaveState()
		} else {
			return "", errors.Errorf("VM SSH host information is not available")
		}
	}

	// Create a temporary SSH key file if needed
	var keyFile string
	if vm.SSHInfo.PrivateKey != "" {
		tmpKeyFile, err := os.CreateTemp("", "vm-ssh-key-*.pem")
		if err != nil {
			return "", errors.Errorf("creating temporary SSH key file: %w", err)
		}
		defer os.Remove(tmpKeyFile.Name())

		if err := os.WriteFile(tmpKeyFile.Name(), []byte(vm.SSHInfo.PrivateKey), 0600); err != nil {
			return "", errors.Errorf("writing SSH key: %w", err)
		}
		keyFile = tmpKeyFile.Name()
	} else {
		// If no private key in the VM object, try using the default SSH key
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Errorf("getting user home directory: %w", err)
		}
		keyFile = filepath.Join(homeDir, ".ssh", "id_rsa")
		if _, err := os.Stat(keyFile); os.IsNotExist(err) {
			return "", errors.Errorf("SSH key not found: %s", keyFile)
		}
	}

	// Build SSH command
	args := []string{
		"-i", keyFile,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-p", fmt.Sprintf("%d", vm.SSHInfo.Port),
	}

	// Add username and host
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
		vm.SaveState()
	}

	args = append(args, fmt.Sprintf("%s@%s", vm.SSHInfo.Username, vm.SSHInfo.Host))
	args = append(args, command)

	// Execute SSH command
	output, err := exec.Command("ssh", args...).CombinedOutput()
	if err != nil {
		return string(output), errors.Errorf("executing command: %w", err)
	}

	return string(output), nil
}
