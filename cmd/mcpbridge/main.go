package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/auth"
	"github.com/erauner12/toolbridge-api/internal/mcpserver/client"
	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
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
	var broker *auth.TokenBroker
	var httpClient *client.HTTPClient

	// Initialize Auth0 token broker (unless in dev mode)
	if !cfg.DevMode {
		log.Info().Msg("Initializing Auth0 token broker...")

		// Create device code flow delegate
		delegate := auth.NewDeviceDelegate()

		// Create token broker with caching
		var err error
		broker, err = auth.NewBroker(cfg.Auth0, delegate)
		if err != nil {
			return fmt.Errorf("failed to create auth broker: %w", err)
		}

		log.Info().
			Str("auth0Domain", cfg.Auth0.Domain).
			Int("clientCount", len(cfg.Auth0.Clients)).
			Strs("defaultScopes", cfg.Auth0.GetDefaultScopes()).
			Msg("Auth0 token broker initialized")

		// Optionally warm up the session in non-interactive mode
		// This will load cached refresh tokens if available
		if _, err := delegate.EnsureSession(ctx, false, cfg.Auth0.GetDefaultScopes()); err != nil {
			log.Warn().Err(err).Msg("failed to establish initial Auth0 session (will retry on first token request)")
		}

		// Phase 3: Initialize REST client with session management
		log.Info().Msg("Initializing REST client with session management...")

		audience := cfg.Auth0.SyncAPI.Audience
		sessionMgr := client.NewSessionManager(cfg.APIBaseURL, broker, audience)
		httpClient = client.NewHTTPClient(cfg.APIBaseURL, broker, sessionMgr, audience)

		log.Info().
			Str("apiBaseUrl", cfg.APIBaseURL).
			Str("audience", audience).
			Msg("REST client initialized")
	} else {
		log.Info().Msg("Running in dev mode - Auth0 authentication bypassed")

		// Phase 3: Initialize REST client in dev mode (no session management)
		// Uses X-Debug-Sub header instead of Bearer token
		debugSub := "mcp-bridge-dev-user"
		httpClient = client.NewHTTPClientDevMode(cfg.APIBaseURL, debugSub)

		log.Info().
			Str("apiBaseUrl", cfg.APIBaseURL).
			Str("debugSub", debugSub).
			Msg("REST client initialized (dev mode)")
	}

	// TODO: Phase 4 - Initialize MCP server with httpClient
	// Example entity clients that will be used in Phase 4:
	// - notesClient := client.NewEntityClient(httpClient, "notes")
	// - tasksClient := client.NewEntityClient(httpClient, "tasks")
	// - commentsClient := client.NewEntityClient(httpClient, "comments")
	// - chatsClient := client.NewEntityClient(httpClient, "chats")
	// - messagesClient := client.NewEntityClient(httpClient, "chat_messages")

	// httpClient will be used in Phase 4 for MCP server
	_ = httpClient

	log.Info().Msg("MCP Bridge Phase 3 complete - REST client ready (MCP server implementation in Phase 4)")

	// Log configuration summary
	log.Debug().
		Interface("config", cfg).
		Msg("Configuration loaded")

	// Wait for shutdown signal
	<-ctx.Done()

	log.Info().Msg("Shutting down MCP server...")

	// Cleanup
	if broker != nil {
		// Logout and clear cached tokens
		if err := broker.LogoutAll(ctx); err != nil {
			log.Warn().Err(err).Msg("error during logout")
		}
	}

	// TODO: Phase 4 - Graceful shutdown of MCP server
	// - Close stdio connections
	// - Flush pending operations
	// - Save state if needed

	return nil
}
