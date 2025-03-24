package lmcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"gitlab.com/tozd/go/errors"
)

type LMCPOpts struct {
	HTTPMode       bool
	HTTPAddr       string
	DisableLogFile bool
	LogLevelStr    string
}

type ServerSetupFunc func(ctx context.Context) (*server.MCPServer, error)

func MyLogFileDir() (string, error) {
	cachedir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.Errorf("getting user cache directory: %w", err)
	}
	execr, err := os.Executable()
	if err != nil {
		return "", errors.Errorf("getting executable name: %w", err)
	}
	exename := filepath.Base(execr)
	return filepath.Join(cachedir, "lmcp", exename), nil
}

func WrapMCPServerWithLogging(ctx context.Context, opts LMCPOpts) (func(ctx context.Context, csrv ServerSetupFunc) error, error) {

	writers := []io.Writer{}

	event := map[string]string{
		"source": "application",
	}

	// Set log level
	level, err := zerolog.ParseLevel(opts.LogLevelStr)
	if err != nil {
		if opts.HTTPMode {
			fmt.Printf("Invalid log level '%s', using 'info'\n", opts.LogLevelStr)
		}
		level = zerolog.InfoLevel
	}
	// zerolog.SetGlobalLevel(level)

	// Configure logger to write to both console and file or just file based on mode
	if opts.HTTPMode {
		// In HTTP mode, write to both console and file
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		writers = append(writers, consoleWriter)

		event["mode"] = "http"
	} else {
		event["mode"] = "stdio"
	}

	logdir, err := MyLogFileDir()
	if err != nil {
		return nil, errors.Errorf("getting log file directory: %w", err)
	}

	var logFileCloser func() error

	// Set up logging
	if !opts.DisableLogFile {
		// Ensure directory exists for custom log path
		if _, err := os.Stat(logdir); os.IsNotExist(err) {
			os.MkdirAll(logdir, 0755)
		}

		logfileWithTime := filepath.Join(logdir, "lmcp."+time.Now().Format("2006-01-02_15-04-05")+".log")

		// Create log file
		logFile, err := os.Create(logfileWithTime)
		if err != nil {
			return nil, errors.Errorf("failed to create log file: %w", err)
		}
		logFileCloser = logFile.Close

		writers = append(writers, logFile)

		event["log_file"] = logfileWithTime
	} else {
		if !opts.HTTPMode {
			return nil, errors.New("log file cannot be disabled in stdio mode")
		}
	}

	if logFileCloser == nil {
		logFileCloser = func() error { return nil }
	}

	multi := zerolog.MultiLevelWriter(writers...)

	loggerpre := zerolog.New(multi).With().Timestamp().Caller()

	for k, v := range event {
		loggerpre = loggerpre.Str(k, v)
	}

	zlog.Logger = loggerpre.Logger().Level(level)

	logger := zlog.Logger

	// Store logger in context
	ctx = logger.WithContext(ctx)

	// Log startup
	logger.Info().Msg("Starting MCP server")

	// Start server based on mode
	if opts.HTTPMode {
		// HTTP mode with SSE
		logger.Info().Str("address", opts.HTTPAddr).Msg("Starting HTTP server")

		var sseContextFunc server.SSEContextFunc = func(ctx context.Context, r *http.Request) context.Context {
			ctx = logger.WithContext(ctx)

			logger := zerolog.Ctx(ctx)

			// logger.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("incoming SSE request")

			if logger.Trace().Enabled() {
				// copy the body of the request to a new buffer

				body, err := io.ReadAll(r.Body)
				if err != nil {
					logger.Error().Err(err).Msg("error reading request body")
				} else {
					logger.Info().RawJSON("body", body).Msg("Request body")
				}

				// reset the body of the request
				r.Body = io.NopCloser(bytes.NewBuffer(body))
			}

			return ctx
		}

		// Create a custom HTTP server with our logger middleware

		// Start the HTTP server
		logger.Info().Str("address", opts.HTTPAddr).Msg("Server is ready to accept connections")
		return func(ctx context.Context, csrv ServerSetupFunc) error {
			defer logFileCloser()

			ctx = logger.WithContext(ctx)

			logger.Info().Str("address", opts.HTTPAddr).Msg("Creating server")

			srv, err := csrv(ctx)
			if err != nil {
				return errors.Errorf("failed to create server: %w", err)
			}
			// Create SSE server
			sseServer := server.NewSSEServer(srv,
				// server.WithSSEEndpoint("/sse"),
				server.WithSSEContextFunc(sseContextFunc),
			)

			logger.Info().Str("address", opts.HTTPAddr).Msg("Server is ready to accept connections")

			httpServer := &http.Server{
				Addr:    opts.HTTPAddr,
				Handler: loggerMiddleware(sseServer, logger),
			}

			return httpServer.ListenAndServe()
		}, nil
	} else {
		// Stdio mode
		logger.Info().Msg("Starting stdio server")

		// We need to direct all errors to our log file only
		// Create a custom io.Writer that writes to our zerolog instance
		errorWriter := &logWriter{
			logger: logger.With().Str("source", "mcp_stdio_error_logs").Logger(),
		}

		// Create a standard library logger that writes to our custom writer
		stdLogger := log.New(errorWriter, "", 0)

		// Set up stdio server options
		stdioOpts := []server.StdioOption{
			server.WithErrorLogger(stdLogger),
		}

		// In stdio mode, make sure we restore stdout to normal before starting
		// This ensures proper stdio communication
		// stdoutBackup := os.Stdout
		// stderrBackup := os.Stderr

		// Start the stdio server - pass a descriptive server name

		return func(ctx context.Context, csrv ServerSetupFunc) error {
			defer logFileCloser()

			ctx = logger.WithContext(ctx)

			srv, err := csrv(ctx)
			if err != nil {
				return errors.Errorf("failed to create server: %w", err)
			}

			// defer func() {
			// 	os.Stdout = stdoutBackup
			// 	os.Stderr = stderrBackup
			// }()

			// logger.Info().Msg("Starting ServeStdio - no more logging to stdout after this point")
			logger.Info().Msg("Starting ServeStdio")

			// IMPORTANT: We need to fully restore stdout for proper stdio mode communication
			// // No writing to stdout from this point on except through the server itself
			// os.Stdout = stdoutBackup
			// os.Stderr = stderrBackup

			return server.ServeStdio(srv, stdioOpts...)
		}, nil
	}

}
