package commands

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/walteh/cloudstack-mcp/pkg/vm"
)

func init() {
	// Initialize the VM manager
	var err error
	Manager, err = vm.NewLocalManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing VM manager: %v\n", err)
		os.Exit(1)
	}

	// Set up logging with zerolog
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	// Add flags to commands
	startVMCmd.Flags().Bool("visible", false, "Start VM in a new terminal window to see logs in real-time")
	startVMCmd.Flags().Bool("logs", false, "Stream VM logs in the current terminal (default behavior)")
	startVMCmd.Flags().Bool("no-logs", false, "Do not show logs during VM start")

	// Set up mutually exclusive flags
	startVMCmd.MarkFlagsMutuallyExclusive("visible", "logs")
	startVMCmd.MarkFlagsMutuallyExclusive("visible", "no-logs")
	startVMCmd.MarkFlagsMutuallyExclusive("logs", "no-logs")

	// Add flags to create-vm command
	createVMCmd.Flags().Bool("visible", false, "Start VM in a new terminal window to see logs in real-time during creation")
	createVMCmd.Flags().Bool("logs", false, "Stream VM logs in the current terminal during creation (default behavior)")
	createVMCmd.Flags().Bool("no-logs", false, "Do not show logs during VM creation")

	// Set up mutually exclusive flags for create-vm
	createVMCmd.MarkFlagsMutuallyExclusive("visible", "logs")
	createVMCmd.MarkFlagsMutuallyExclusive("visible", "no-logs")
	createVMCmd.MarkFlagsMutuallyExclusive("logs", "no-logs")
}
