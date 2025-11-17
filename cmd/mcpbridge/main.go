package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
	"github.com/erauner12/toolbridge-api/internal/mcpserver/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	version = "0.1.0"
)

var (
	configPath = flag.String("config", "", "Path to configuration file (JSON)")
	showVersion = flag.Bool("version", false, "Show version information")
	devMode    = flag.Bool("dev", false, "Enable development mode (uses X-Debug-Sub header)")
	debug      = flag.Bool("debug", false, "Enable debug logging")
	logLevel   = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	// Show version and exit
	if *showVersion {
		fmt.Printf("mcpbridge version %s\n", version)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Setup logging
	setupLogging(cfg)

	log.Info().
		Str("version", version).
		Str("apiBaseUrl", cfg.APIBaseURL).
		Bool("devMode", cfg.DevMode).
		Bool("debug", cfg.Debug).
		Msg("Starting MCP Bridge Server")

	// Warn if dev mode is enabled
	if cfg.DevMode {
		log.Warn().Msg("Dev mode is enabled - Auth0 authentication will be bypassed!")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Create shutdown goroutine
	go func() {
		sig := <-sigChan
		log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
	}()

	// Run the MCP server
	if err := run(ctx, cfg); err != nil {
		log.Error().Err(err).Msg("MCP server failed")
		os.Exit(1)
	}

	log.Info().Msg("MCP Bridge Server stopped gracefully")
}

// loadConfig loads the configuration from file and environment
func loadConfig() (*config.Config, error) {
	var cfg *config.Config
	var err error

	if *configPath != "" {
		cfg, err = config.Load(*configPath)
	} else {
		// Try to load from environment only
		cfg, err = config.LoadFromEnvironment()
	}

	if err != nil {
		return nil, err
	}

	// Apply CLI flag overrides BEFORE validation
	// This allows --dev and --debug to work without requiring config/env setup
	if *devMode {
		cfg.DevMode = true
	}
	if *debug {
		cfg.Debug = true
		// Auto-set log level to debug when --debug flag is used
		// (unless user explicitly set a different level)
		if *logLevel == "info" {
			cfg.LogLevel = "debug"
		}
	}
	if *logLevel != "info" {
		cfg.LogLevel = *logLevel
	}

	// Validate configuration AFTER applying CLI overrides
	// This ensures --dev mode can bypass Auth0 validation
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// setupLogging configures the global logger
func setupLogging(cfg *config.Config) {
	// Parse log level
	level := parseLogLevel(cfg.LogLevel)
	zerolog.SetGlobalLevel(level)

	// Configure output format
	if cfg.Debug {
		// Pretty logging for development
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		})
	} else {
		// JSON logging for production
		log.Logger = zerolog.New(os.Stderr).
			With().
			Timestamp().
			Logger()
	}

	// Add caller information in debug mode
	if cfg.Debug {
		log.Logger = log.Logger.With().Caller().Logger()
	}
}

// parseLogLevel converts a string log level to zerolog.Level
func parseLogLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// run is the main application logic
func run(ctx context.Context, cfg *config.Config) error {
	// Phase 4: Start MCP server in Streamable HTTP mode
	mcpServer := server.NewMCPServer(cfg)

	// Warm up JWT validator (pre-fetch JWKS)
	// This makes /readyz return 200 faster
	if err := mcpServer.WarmUp(); err != nil {
		log.Warn().Err(err).Msg("Failed to warm up JWT validator (will retry on first request)")
	}

	// Start HTTP server in background
	serverErr := make(chan error, 1)
	go func() {
		addr := ":8082" // Different port from REST API (8081)
		if err := mcpServer.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("MCP server failed")
			serverErr <- err
		}
	}()

	log.Info().Msg("MCP server started in Streamable HTTP mode")

	// Wait for shutdown signal or server error
	select {
	case <-ctx.Done():
		log.Info().Msg("Shutting down MCP server...")
	case err := <-serverErr:
		return fmt.Errorf("MCP server error: %w", err)
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return mcpServer.Shutdown(shutdownCtx)
}
