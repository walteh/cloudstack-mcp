package qemu

import (
	"os"
	"runtime"
)

// isMacOS checks if the current OS is macOS
func isMacOS() bool {
	// Primary check using runtime
	if runtime.GOOS == "darwin" {
		return true
	}

	// Fallback checks using environment variables
	return os.Getenv("DARWIN_ORIGIN") != "" ||
		os.Getenv("OSTYPE") == "darwin" ||
		os.Getenv("TERM_PROGRAM") == "Apple_Terminal"
}
