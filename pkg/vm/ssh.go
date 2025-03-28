package vm

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"gitlab.com/tozd/go/errors"
	"golang.org/x/crypto/ssh"
)

// findAvailablePort finds an available TCP port to use
func findAvailablePort() (int, error) {
	// Ask the OS to give us an available port by binding to port 0
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, errors.Errorf("resolving TCP address: %w", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, errors.Errorf("finding available port: %w", err)
	}
	defer l.Close()

	// Get the port that was assigned by the OS
	return l.Addr().(*net.TCPAddr).Port, nil
}

// createSSHConfig creates an SSH client configuration
func (vm *VM) createSSHConfig() (*ssh.ClientConfig, error) {
	authMethods := []ssh.AuthMethod{}

	// If we have a private key in the VM config, use it
	if vm.SSHInfo.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(vm.SSHInfo.PrivateKey))
		if err != nil {
			return nil, errors.Errorf("parsing private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else {
		// Check for all possible keys, including walteh and mcverse specific keys
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.Errorf("getting user home directory: %w", err)
		}

		// Try the walteh and mcverse keys first
		keyPaths := []string{
			filepath.Join(homeDir, ".ssh", "walteh.git"),
			filepath.Join(homeDir, ".ssh", "mcverse-org.git"),
			filepath.Join(homeDir, ".ssh", "id_rsa"),
			filepath.Join(homeDir, ".ssh", "id_ed25519"),
			filepath.Join(homeDir, ".ssh", "id_ecdsa"),
		}

		keyFound := false
		for _, keyPath := range keyPaths {
			if _, err := os.Stat(keyPath); err == nil {
				keyData, err := os.ReadFile(keyPath)
				if err != nil {
					continue
				}

				signer, err := ssh.ParsePrivateKey(keyData)
				if err != nil {
					continue
				}

				authMethods = append(authMethods, ssh.PublicKeys(signer))
				keyFound = true
				break
			}
		}

		// If no private key was found, we'll try agent auth later
		if !keyFound {
			// Use password authentication as fallback
			if vm.SSHInfo.Password != "" {
				authMethods = append(authMethods, ssh.Password(vm.SSHInfo.Password))
			} else {
				// Try using empty password
				authMethods = append(authMethods, ssh.Password(""))
			}
		}
	}

	// Ensure username is set
	if vm.SSHInfo.Username == "" {
		vm.SSHInfo.Username = "ubuntu" // Default for Ubuntu cloud images
		vm.SaveState()
	}

	return &ssh.ClientConfig{
		User:            vm.SSHInfo.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // For simplicity, but not secure
		Timeout:         10 * time.Second,
	}, nil
}

// connectToVM establishes an SSH connection to the VM
func (vm *VM) connectToVM() (*ssh.Client, error) {
	// Check if the VM is running
	if vm.GetStatus() != "running" {
		return nil, errors.Errorf("VM is not running")
	}

	// Ensure host and port are set
	if vm.SSHInfo.Host == "" {
		// For user networking, default to localhost with port forwarding
		if vm.Config.Network.Type == "user" {
			vm.SSHInfo.Host = "localhost"
			// If port is not set or is default (22), find an available port
			if vm.SSHInfo.Port == 0 || vm.SSHInfo.Port == 22 {
				port, err := findAvailablePort()
				if err != nil {
					return nil, errors.Errorf("finding available port: %w", err)
				}
				vm.SSHInfo.Port = port
				vm.SaveState()
			}
		} else {
			return nil, errors.Errorf("VM SSH host information is not available")
		}
	}

	// Create SSH config
	config, err := vm.createSSHConfig()
	if err != nil {
		return nil, errors.Errorf("creating SSH config: %w", err)
	}

	// Connect to the VM
	address := fmt.Sprintf("%s:%d", vm.SSHInfo.Host, vm.SSHInfo.Port)
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, errors.Errorf("connecting to VM: %w", err)
	}

	return client, nil
}

// ExecShell opens an interactive shell to the VM via SSH
func (vm *VM) ExecShell() error {
	// Since we can't easily create an interactive shell with the crypto/ssh package,
	// we'll use the command-line ssh as a fallback for this specific use case

	// Check if the VM is running
	if vm.GetStatus() != "running" {
		return errors.Errorf("VM is not running")
	}

	// Build SSH command arguments
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-p", strconv.Itoa(vm.SSHInfo.Port),
	}

	// Add private key if available
	if vm.SSHInfo.PrivateKey != "" {
		tmpKeyFile, err := os.CreateTemp("", "vm-ssh-key-*.pem")
		if err != nil {
			return errors.Errorf("creating temporary SSH key file: %w", err)
		}
		defer os.Remove(tmpKeyFile.Name())

		if err := os.WriteFile(tmpKeyFile.Name(), []byte(vm.SSHInfo.PrivateKey), 0600); err != nil {
			return errors.Errorf("writing SSH key: %w", err)
		}
		args = append(args, "-i", tmpKeyFile.Name())
	} else {
		// Try to find a key in ~/.ssh
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Errorf("getting user home directory: %w", err)
		}

		// Check for common key files, prioritizing walteh keys
		keyPaths := []string{
			filepath.Join(homeDir, ".ssh", "walteh.git"),
			filepath.Join(homeDir, ".ssh", "mcverse-org.git"),
			filepath.Join(homeDir, ".ssh", "id_rsa"),
			filepath.Join(homeDir, ".ssh", "id_ed25519"),
			filepath.Join(homeDir, ".ssh", "id_ecdsa"),
		}

		keyFound := false
		for _, keyPath := range keyPaths {
			if _, err := os.Stat(keyPath); err == nil {
				args = append(args, "-i", keyPath)
				keyFound = true
				break
			}
		}

		// If no key is found, try to use SSH agent forwarding
		if !keyFound {
			// No key file, use agent forwarding
			args = append(args, "-A")
		}
	}

	// Add username and host
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
	if vm.GetStatus() != "running" {
		return "", errors.Errorf("VM is not running")
	}

	// Build SSH command arguments
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-p", strconv.Itoa(vm.SSHInfo.Port),
	}

	// Add private key if available
	if vm.SSHInfo.PrivateKey != "" {
		tmpKeyFile, err := os.CreateTemp("", "vm-ssh-key-*.pem")
		if err != nil {
			return "", errors.Errorf("creating temporary SSH key file: %w", err)
		}
		defer os.Remove(tmpKeyFile.Name())

		if err := os.WriteFile(tmpKeyFile.Name(), []byte(vm.SSHInfo.PrivateKey), 0600); err != nil {
			return "", errors.Errorf("writing SSH key: %w", err)
		}
		args = append(args, "-i", tmpKeyFile.Name())
	} else {
		// Try to find a key in ~/.ssh
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Errorf("getting user home directory: %w", err)
		}

		// Check for common key files, prioritizing walteh keys
		keyPaths := []string{
			filepath.Join(homeDir, ".ssh", "walteh.git"),
			filepath.Join(homeDir, ".ssh", "mcverse-org.git"),
			filepath.Join(homeDir, ".ssh", "id_rsa"),
			filepath.Join(homeDir, ".ssh", "id_ed25519"),
			filepath.Join(homeDir, ".ssh", "id_ecdsa"),
		}

		keyFound := false
		for _, keyPath := range keyPaths {
			if _, err := os.Stat(keyPath); err == nil {
				args = append(args, "-i", keyPath)
				keyFound = true
				break
			}
		}

		// If no key is found, try to use SSH agent forwarding
		if !keyFound {
			// No key file, use agent forwarding
			args = append(args, "-A")
		}
	}

	// Add username, host, and command
	args = append(args, fmt.Sprintf("%s@%s", vm.SSHInfo.Username, vm.SSHInfo.Host), command)

	// Execute SSH command
	cmd := exec.Command("ssh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), errors.Errorf("command failed: %w: %s", err, output)
	}

	return string(output), nil
}
