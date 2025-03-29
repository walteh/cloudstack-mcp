package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")

	rootCmd.PersistentPreRunE = setLoggingToContextInPreRun

	// Execute command
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		log.Fatal().Err(err).Msg("Error running command")
	}
}

func setLoggingToContextInPreRun(cmd *cobra.Command, args []string) error {
	level := zerolog.InfoLevel
	debug := cmd.Flag("debug")
	if debug.Changed && debug.Value.String() == "true" {
		level = zerolog.DebugLevel
	}

	ctx := zerolog.Ctx(cmd.Context()).With().Str("command", cmd.Name()).Logger().Level(level).WithContext(cmd.Context())
	cmd.SetContext(ctx)

	manager, err := vm.NewLocalManager()
	if err != nil {
		return errors.Errorf("creating VM manager: %w", err)
	}
	commands.Manager = manager

	return nil
}
