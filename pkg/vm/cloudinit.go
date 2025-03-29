package vm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gitlab.com/tozd/go/errors"
)

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

	sshPubKeyPath := filepath.Join(homeDir, ".ssh", "walteh.git.pub")
	sshPubKey, err := os.ReadFile(sshPubKeyPath)
	if err != nil {
		return "", errors.Errorf("reading SSH public key: %w", err)
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

	// Create cloud-init user-data
	userData := fmt.Sprintf(`#cloud-config
ssh_authorized_keys:
  - %s

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

runcmd:
  - sysctl -p /etc/sysctl.d/50-vip-arp.conf
  - modprobe br_netfilter
%s
`, strings.TrimSpace(string(sshPubKey)), kvmPackages, kvmCommands)

	return userData, nil
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
