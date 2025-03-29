package vm

import (
	"path/filepath"
	"strings"
)

// DetectArchitecture determines the architecture from the image name
func DetectArchitecture(imageName string) string {
	// Check if the image name contains arm64 or aarch64
	if strings.Contains(imageName, "arm64") || strings.Contains(imageName, "aarch64") {
		return "aarch64"
	}

	// Check if the image name contains amd64, x86_64, or x64
	if strings.Contains(imageName, "amd64") || strings.Contains(imageName, "x86_64") || strings.Contains(imageName, "x64") {
		return "x86_64"
	}

	// Default to the host architecture (assuming aarch64 for Mac M1/M2)
	return "aarch64"
}

// GetQEMUPath returns the appropriate QEMU binary path for the architecture
func GetQEMUPath(arch string) string {
	switch arch {
	case "aarch64":
		return "qemu-system-aarch64"
	case "x86_64":
		return "qemu-system-x86_64"
	default:
		return "qemu-system-aarch64" // Default to ARM64 on Macs
	}
}

// GetMachineSetting returns the appropriate machine settings for the architecture
func GetMachineSetting(arch string, isAppleSilicon bool) string {
	switch arch {
	case "aarch64":
		if isAppleSilicon {
			return "virt,accel=hvf,highmem=on"
		}
		return "virt,accel=kvm"
	case "x86_64":
		if isAppleSilicon {
			// On Apple Silicon M1/M2, don't use hardware acceleration for x86_64
			return "q35"
		}
		return "q35,accel=kvm"
	default:
		return "virt,accel=hvf,highmem=on" // Default for ARM64 on Macs
	}
}

// IsAppleSilicon checks if we're running on Apple Silicon
func IsAppleSilicon() bool {
	// Check for /opt/homebrew which is typical for M1/M2 Macs
	_, err := filepath.Glob("/opt/homebrew*")
	return err == nil
}

// GetEFIPath returns the appropriate EFI firmware path for the architecture
func GetEFIPath(arch string) string {
	switch arch {
	case "aarch64":
		// Check for Apple Silicon path first
		if IsAppleSilicon() {
			return "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"
		}
		return "/usr/share/qemu/edk2-aarch64-code.fd"
	case "x86_64":
		if IsAppleSilicon() {
			return "/opt/homebrew/share/qemu/edk2-x86_64-code.fd"
		}
		return "/usr/share/qemu/OVMF.fd"
	default:
		return "/opt/homebrew/share/qemu/edk2-aarch64-code.fd" // Default for ARM64
	}
}
