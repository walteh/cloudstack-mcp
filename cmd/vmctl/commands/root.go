package commands

import (
	"github.com/spf13/cobra"
	"github.com/walteh/cloudstack-mcp/pkg/vm"
)

//go:generate go run ./generate

// Execute is the main entry point for the vmctl command

var (
	// Manager is the global VM manager instance

	// Debug flag for verbose logging
	Debug bool
)

var (
	Manager *vm.LocalManager
)

// RootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "vmctl",
	Short: "VM control utility for CloudStack MCP",
	Long: `A command line utility for managing virtual machines
in the CloudStack MCP environment. This tool provides functionality
for creating, managing, and interacting with VMs.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.

// func init() {
// 	// Define persistent flags that apply to all commands
// 	rootCmd.PersistentFlags().BoolVarP(&Debug, "debug", "d", false, "Enable debug logging")

// 	// The following line is used by go generate to add commands

// }

func RootCmd() *cobra.Command {
	return rootCmd
}
