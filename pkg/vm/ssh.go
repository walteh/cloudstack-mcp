package vm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/walteh/cloudstack-mcp/pkg/tmux"
	"gitlab.com/tozd/go/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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

	// Try the walteh and mcverse keys first
	keyPaths := []string{
		"walteh.git",
	}

	signers, err := getSSHAgentSigners()
	if err != nil {
		return nil, errors.Errorf("getting SSH agent signers: %w", err)
	}

	for _, signer := range signers {
		pubKey := signer.PublicKey()
		for _, keyPath := range keyPaths {
			if strings.Contains(string(pubKey.Marshal()), keyPath) {
				authMethods = append(authMethods, ssh.PublicKeys(signer))
				break
			}
		}
	}

	// for _, signer := range signers {
	// 	if strings.Contains(signer
	// 	authMethods = append(authMethods, ssh.PublicKeys(signer))
	// }

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

func getSSHAgentSigners() ([]ssh.Signer, error) {
	// Get the SSH agent socket from the environment.
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.Errorf("SSH_AUTH_SOCK not found")
	}

	// Connect to the SSH agent.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, errors.Errorf("failed to connect to SSH agent: %w", err)
	}

	// Create a new agent client.
	ag := agent.NewClient(conn)

	// Retrieve all available signers from the agent.
	signers, err := ag.Signers()
	if err != nil {
		conn.Close()
		return nil, errors.Errorf("failed to retrieve signers: %w", err)
	}

	// kill the connection on signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		conn.Close()
	}()

	// // Create an SSH client configuration using the agent's signers.
	// config := &ssh.ClientConfig{
	// 	User: "your_username",
	// 	Auth: []ssh.AuthMethod{
	// 		ssh.PublicKeys(signers...),
	// 	},
	// 	// Replace this with a proper host key callback in production.
	// 	HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	// }

	return signers, nil
}

// connectToVM establishes an SSH connection to the VM
func (vm *VM) connectToVM(ctx context.Context, mgr *tmux.SessionManager) (*ssh.Client, error) {
	// Check if the VM is running
	if isRunning, err := VMIsRunning(ctx, mgr, vm); err != nil {
		return nil, errors.Errorf("checking if VM is running: %w", err)
	} else if !isRunning {
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
func (vm *VM) ExecShell(ctx context.Context, mgr *tmux.SessionManager) error {
	// Since we can't easily create an interactive shell with the crypto/ssh package,
	// we'll use the command-line ssh as a fallback for this specific use case

	// Check if the VM is running
	if isRunning, err := VMIsRunning(ctx, mgr, vm); err != nil {
		return errors.Errorf("checking if VM is running: %w", err)
	} else if !isRunning {
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

// WaitForSSH attempts to connect to the VM via SSH, retrying until successful or timeout
func (vm *VM) WaitForSSH(ctx context.Context, timeoutSeconds int) (*ssh.Client, error) {
	// Create SSH config
	config, err := vm.createSSHConfig()
	if err != nil {
		return nil, errors.Errorf("creating SSH config: %w", err)
	}

	// Set a timeout for the entire operation
	timeout := time.After(time.Duration(timeoutSeconds) * time.Second)
	retryInterval := 5 * time.Second
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	address := fmt.Sprintf("%s:%d", vm.SSHInfo.Host, vm.SSHInfo.Port)
	var client *ssh.Client
	var lastError error
	attempts := 0

	fmt.Printf("Attempting SSH connection to %s...\n", address)

	// Try to connect until success or timeout
	for {
		select {
		case <-ctx.Done():
			return nil, errors.Errorf("context canceled while waiting for SSH: %w", ctx.Err())
		case <-timeout:
			errDetails := ""
			if lastError != nil {
				errDetails = fmt.Sprintf(" Last error: %v", lastError)
			}
			return nil, errors.Errorf("timed out waiting for SSH connection after %d attempts.%s", attempts, errDetails)
		case <-ticker.C:
			attempts++
			fmt.Printf("SSH connection attempt %d to %s...\n", attempts, address)

			var dialErr error
			client, dialErr = ssh.Dial("tcp", address, config)
			if dialErr == nil {
				// Successfully connected
				fmt.Printf("SSH connection established successfully after %d attempts\n", attempts)
				return client, nil
			}

			// Store the last error for diagnostics
			lastError = dialErr

			// Log more detailed error information
			if attempts%5 == 0 { // Log detailed error every 5 attempts
				errorType := "unknown error"
				if _, ok := dialErr.(*net.OpError); ok {
					errorType = "network error (connection refused/timeout)"
				} else if strings.Contains(dialErr.Error(), "auth") || strings.Contains(dialErr.Error(), "authentication") {
					errorType = "authentication error"
				} else if strings.Contains(dialErr.Error(), "handshake") {
					errorType = "SSH handshake error"
				}

				fmt.Printf("SSH connection failed (attempt %d): %s - %v\n", attempts, errorType, dialErr)
			}
		}
	}
}

// RunCommand executes a command on the VM via SSH and returns the output
func (vm *VM) RunCommand(ctx context.Context, command string) (string, error) {
	// Check if context is already canceled
	if ctx.Err() != nil {
		return "", errors.Errorf("context already canceled: %w", ctx.Err())
	}

	// Connect to the VM
	client, err := vm.WaitForSSH(ctx, 30) // 30 second timeout for initial connection
	if err != nil {
		return "", errors.Errorf("connecting to VM: %w", err)
	}
	defer client.Close()

	// Create a session
	session, err := client.NewSession()
	if err != nil {
		return "", errors.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	// Run the command
	output, err := session.CombinedOutput(command)
	if err != nil {
		return string(output), errors.Errorf("command failed: %w: %s", err, output)
	}

	return string(output), nil
}

// ExecCommand executes a command on the VM via SSH and returns the output
// This is a simpler version that uses the ssh command-line client
func (vm *VM) ExecCommand(ctx context.Context, mgr *tmux.SessionManager, command string) (string, error) {
	// Check if the VM is running
	if isRunning, err := VMIsRunning(ctx, mgr, vm); err != nil {
		return "", errors.Errorf("checking if VM is running: %w", err)
	} else if !isRunning {
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

// ConnectSSH creates an SSH client connection to the VM
func (vm *VM) ConnectSSH(ctx context.Context, mgr *tmux.SessionManager) (*ssh.Client, error) {

	client, err := vm.connectToVM(ctx, mgr)
	if err != nil {
		return nil, errors.Errorf("connecting to SSH: %w", err)
	}

	return client, nil
}

// GenerateSSHKey generates a new SSH key pair
func GenerateSSHKey() (string, string, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", errors.Errorf("generating RSA key: %w", err)
	}

	// Convert private key to PEM format
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyStr := string(pem.EncodeToMemory(privateKeyPEM))

	// Generate public key
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", errors.Errorf("generating SSH public key: %w", err)
	}

	// Convert public key to authorized_keys format
	publicKeyStr := fmt.Sprintf("%s\n", string(ssh.MarshalAuthorizedKey(publicKey)))

	return publicKeyStr, privateKeyStr, nil
}
