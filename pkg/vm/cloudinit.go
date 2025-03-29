package vm

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/tozd/go/errors"
)

// Initialize random number generator
func init() {
	rand.Seed(time.Now().UnixNano())
}

// GenerateMetaData generates the cloud-init meta-data for this VM
func (vm *VM) BuildMetaData() (string, error) {
	return fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n",
		vm.Config.Name,
		vm.Config.Network.Hostname), nil
}

// // GenerateUserData generates the cloud-init user-data for this VM based on node type
// func (vm *VM) GenerateUserData(nodeType string) (string, error) {
// 	switch nodeType {
// 	case "control":
// 		return vm.generateControlPlaneUserData()
// 	case "worker":
// 		return vm.generateWorkerUserData()
// 	default:
// 		return vm.generateDefaultUserData()
// 	}
// }

// generateDefaultUserData generates a default cloud-init user-data configuration
func (vm *VM) UserData(withKvm bool) (string, error) {
	// Try to get the user's SSH public key
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Errorf("getting user home directory: %w", err)
	}

	// Look for multiple SSH public key locations
	sshPubKeyPaths := []string{
		filepath.Join(homeDir, ".ssh", "walteh.git.pub"),
		filepath.Join(homeDir, ".ssh", "id_rsa.pub"),
		filepath.Join(homeDir, ".ssh", "id_ed25519.pub"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa.pub"),
	}

	var sshPubKey []byte
	var foundKey bool

	for _, keyPath := range sshPubKeyPaths {
		if _, err := os.Stat(keyPath); err == nil {
			sshPubKey, err = os.ReadFile(keyPath)
			if err == nil && len(sshPubKey) > 0 {
				foundKey = true
				break
			}
		}
	}

	if !foundKey {
		return "", errors.Errorf("no SSH public key found in ~/.ssh directory, please generate one with 'ssh-keygen'")
	}

	var kvmPackages, kvmCommands string
	if withKvm {
		kvmPackages = `
  - qemu-kvm
  - qemu-utils
  - libvirt-daemon-system
  - libvirt-clients
  - bridge-utils
  - virt-manager
`
		kvmCommands = `
  - systemctl enable libvirtd
  - systemctl start libvirtd
  - usermod -aG libvirt ubuntu
`
	}

	// Store SSH key in metadata for reference
	vm.MetaData["ssh_pubkey"] = strings.TrimSpace(string(sshPubKey))

	// Create cloud-init user-data with explicit SSH key configuration
	userData := fmt.Sprintf(`#cloud-config

# Ensure SSH key setup is handled properly
users:
  - name: ubuntu
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: users, admin, sudo
    shell: /bin/bash
    ssh_authorized_keys:
      - %s

# Disable password authentication
ssh_pwauth: false

# Disable password expiry
chpasswd:
  expire: false
  
# Always run ssh configuration module
cloud_init_modules:
 - migrator
 - bootcmd
 - write-files
 - growpart
 - resizefs
 - set_hostname
 - update_hostname
 - update_etc_hosts
 - users-groups
 - ssh

# Enable debugging for cloud-init
debug:
  verbose: true

# Write cloud-init logs to console for visibility
output: {all: '| tee -a /dev/console /var/log/cloud-init-verbose.log'}

# Make cloud-init logging more verbose
cloud_init:
  log_level: DEBUG
  log_file: /var/log/cloud-init-debug.log

package_update: true
package_upgrade: true
package_reboot_if_required: false

packages:
  - curl
  - ca-certificates
  - openssh-server
%s

write_files:
  - path: /etc/sysctl.d/50-vip-arp.conf
    content: |
      net.ipv4.conf.all.arp_announce = 2
      net.ipv4.conf.all.arp_ignore = 1
  - path: /etc/modules-load.d/cloud-init.conf
    content: |
      br_netfilter
  - path: /etc/ssh/sshd_config.d/99-cloudstack-mcp.conf
    content: |
      # Allow SSH key authentication
      PubkeyAuthentication yes
      AuthorizedKeysFile .ssh/authorized_keys
      PasswordAuthentication no
      ChallengeResponseAuthentication no
  - path: /etc/cloud/cloud.cfg.d/99_ssh.cfg
    content: |
      ssh_deletekeys: false
      ssh_genkeytypes: ['rsa', 'ecdsa', 'ed25519']
      ssh:
        emit_keys_to_console: true
  - path: /etc/cloud/cloud.cfg.d/05_logging.cfg
    content: |
      output: {all: '| tee -a /dev/console /var/log/cloud-init-verbose.log'}
  
runcmd:
  - sysctl -p /etc/sysctl.d/50-vip-arp.conf
  - modprobe br_netfilter
  - mkdir -p /home/ubuntu/.ssh
  - echo "%s" > /home/ubuntu/.ssh/authorized_keys
  - chmod 600 /home/ubuntu/.ssh/authorized_keys
  - chmod 700 /home/ubuntu/.ssh
  - chown -R ubuntu:ubuntu /home/ubuntu/.ssh
  - systemctl restart sshd
  - echo "SSH_DEBUG: Public key is: $(cat /home/ubuntu/.ssh/authorized_keys)" > /var/log/ssh_debug.log 
  - echo "SSH_DEBUG: User exists: $(getent passwd ubuntu)" >> /var/log/ssh_debug.log
  - ls -la /home/ubuntu/.ssh >> /var/log/ssh_debug.log
  - systemctl status sshd >> /var/log/ssh_debug.log
  - cp /var/log/cloud-init*.log /var/log/cloud-init-output.log /dev/console || true
  - journalctl -u cloud-init* > /var/log/cloud-init-journal.log || true
  - echo "CLOUD_INIT_DEBUG: Completed user-data execution" > /var/log/cloud-init-complete.log
%s
`, strings.TrimSpace(string(sshPubKey)), kvmPackages, strings.TrimSpace(string(sshPubKey)), kvmCommands)

	return userData, nil
}

// generateRandomPassword generates a random password for VM access
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}

// // generateControlPlaneUserData generates cloud-init user-data for Kubernetes control plane nodes
// func (vm *VM) generateControlPlaneUserData() (string, error) {
// 	baseUserData, err := vm.generateDefaultUserData()
// 	if err != nil {
// 		return "", err
// 	}

// 	// Add control plane specific configuration
// 	userData := fmt.Sprintf(`%s
//   - echo 'KUBELET_EXTRA_ARGS="--node-ip=$(hostname -I | awk "{print \\$1}")"' > /etc/default/kubelet
// `, baseUserData)

// 	return userData, nil
// }

// // generateWorkerUserData generates cloud-init user-data for Kubernetes worker nodes
// func (vm *VM) generateWorkerUserData() (string, error) {
// 	baseUserData, err := vm.generateDefaultUserData()
// 	if err != nil {
// 		return "", err
// 	}

// 	// Add worker specific configuration
// 	userData := fmt.Sprintf(`%s
//   - echo 'KUBELET_EXTRA_ARGS="--node-ip=$(hostname -I | awk "{print \\$1}")"' > /etc/default/kubelet
// `, baseUserData)

// 	return userData, nil
// }

// GenerateNetworkConfig generates the cloud-init network configuration for this VM
func (vm *VM) NetworkConfig() (string, error) {
	if vm.Config.Network.IPRange != "" && vm.Config.Network.Subnet != "" {
		// Static IP configuration
		parts := strings.Split(vm.Config.Network.IPRange, ",")
		if len(parts) > 0 {
			ipAddress := parts[0]
			// Extract gateway from IP by replacing last octet with 1
			ipParts := strings.Split(ipAddress, ".")
			if len(ipParts) == 4 {
				gateway := fmt.Sprintf("%s.%s.%s.1", ipParts[0], ipParts[1], ipParts[2])
				dns := "8.8.8.8,8.8.4.4" // Default to Google DNS

				return vm.generateStaticNetworkConfig(ipAddress, gateway, dns)
			}
		}
	}

	// Default to DHCP
	return vm.generateDefaultNetworkConfig()
}

// generateDefaultNetworkConfig generates a default cloud-init network configuration with DHCP
func (vm *VM) generateDefaultNetworkConfig() (string, error) {
	return `network:
  version: 2
  ethernets:
    eth0:
      match:
        name: en*
      dhcp4: true
`, nil
}

// generateStaticNetworkConfig generates a cloud-init network configuration with static IP
func (vm *VM) generateStaticNetworkConfig(ipAddress, gateway, dns string) (string, error) {
	return fmt.Sprintf(`network:
  version: 2
  ethernets:
    eth0:
      match:
        name: en*
      addresses: [%s]
      gateway4: %s
      nameservers:
        addresses: [%s]
`, ipAddress, gateway, dns), nil
}

// createCloudInitISO creates a cloud-init ISO from the provided data
func createCloudInitISO(vmConfig VMConfig, userData, metaData, networkConfig, mkisofsPath, vmDir string) error {
	// Create cloud-init files
	metaDataContent := metaData
	if metaDataContent == "" {
		// Generate default meta-data if not provided
		metaDataContent = fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n",
			vmConfig.Name,
			vmConfig.Network.Hostname)
	}

	userDataContent := userData
	if userDataContent == "" {
		// Warning: this would normally generate the default userData, but since the function requires a VM object,
		// we can't call it directly here. Callers should generate user data before calling this function.
		userDataContent = "#cloud-config\n"
	}

	networkConfigContent := networkConfig
	if networkConfigContent == "" {
		// Generate default network config
		networkConfigContent = `network:
  version: 2
  ethernets:
    eth0:
      match:
        name: en*
      dhcp4: true
`
	}

	// Write cloud-init files
	metaDataPath := filepath.Join(vmDir, "meta-data")
	if err := os.WriteFile(metaDataPath, []byte(metaDataContent), 0644); err != nil {
		return errors.Errorf("writing meta-data: %w", err)
	}

	userDataPath := filepath.Join(vmDir, "user-data")
	if err := os.WriteFile(userDataPath, []byte(userDataContent), 0644); err != nil {
		return errors.Errorf("writing user-data: %w", err)
	}

	networkConfigPath := filepath.Join(vmDir, "network-config")
	if err := os.WriteFile(networkConfigPath, []byte(networkConfigContent), 0644); err != nil {
		return errors.Errorf("writing network-config: %w", err)
	}

	// Create cloud-init ISO
	ciDataPath := filepath.Join(vmDir, "cidata.iso")
	mkisofsCmdArgs := []string{
		"-output", ciDataPath,
		"-volid", "cidata",
		"-joliet",
		"-rock",
		metaDataPath,
		userDataPath,
		networkConfigPath,
	}

	cmd := exec.Command(mkisofsPath, mkisofsCmdArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return errors.Errorf("creating cloud-init ISO: %s: %w", output, err)
	}

	return nil
}
