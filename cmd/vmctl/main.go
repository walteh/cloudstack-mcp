package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/walteh/cloudstack-mcp/cmd/vmctl/commands"
	"github.com/walteh/cloudstack-mcp/pkg/vm"
	"gitlab.com/tozd/go/errors"
)

func main() {

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Println("Received signal", sig, "shutting down")
		cancel()
	}()

	rootCmd := commands.RootCmd()

	// hide usage on error
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")

	rootCmd.PersistentPreRunE = setLoggingToContextInPreRun

	// Execute command
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		level := getLevelFromFlag(rootCmd)
		if level == zerolog.DebugLevel {
			fmt.Fprintf(os.Stderr, "\nError running command: %+v\n\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "\nError running command: %v\n\n", err)
		}
		os.Exit(1)
	}
}

func getLevelFromFlag(cmd *cobra.Command) zerolog.Level {
	level := zerolog.InfoLevel
	debug := cmd.Flag("debug")
	if debug.Changed && debug.Value.String() == "true" {
		level = zerolog.DebugLevel
	}
	return level
}
func setLoggingToContextInPreRun(cmd *cobra.Command, args []string) error {
	level := getLevelFromFlag(cmd)

	wri := zerolog.New(zerolog.NewConsoleWriter())
	logger := wri.With().Str("command", cmd.Name()).Logger().Level(level)

	ctx := logger.WithContext(cmd.Context())
	cmd.SetContext(ctx)

	manager, err := vm.NewLocalManager()
	if err != nil {
		return errors.Errorf("creating VM manager: %w", err)
	}
	commands.Manager = manager

	return nil
}
