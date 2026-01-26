// Package main provides the telephone reverse proxy sidecar application.
package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/verastack/telephone/pkg/config"
	"github.com/verastack/telephone/pkg/logger"
	"github.com/verastack/telephone/pkg/proxy"
)

var (
	envFile  = flag.String("env", ".env", "Path to .env file")
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	logFmt   = flag.String("log-format", "text", "Log format (text, json)")
	version  = "dev"
)

func main() {
	flag.Parse()

	// Initialize logger
	logger.Init(*logLevel, *logFmt)

	logger.Info("Telephone starting", "version", version)

	// Load .env file if it exists
	if _, err := os.Stat(*envFile); err == nil {
		logger.Info("Loading environment", "file", *envFile)

		if err := godotenv.Load(*envFile); err != nil {
			logger.Error("Failed to load .env file", "error", err)
			os.Exit(1)
		}
	}

	// Load configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		logger.Error("Configuration error", "error", err)
		os.Exit(1)
	}

	logger.Info("Configuration loaded",
		"plugboard_url", cfg.PlugboardURL,
		"backend", cfg.BackendURL(),
	)

	// Create Telephone instance
	tel, err := proxy.New(cfg)
	if err != nil {
		logger.Error("Failed to create Telephone instance", "error", err)
		os.Exit(1)
	}

	// Start the client
	if err := tel.Start(); err != nil {
		logger.Error("Failed to start Telephone", "error", err)
		os.Exit(1)
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("Telephone is running. Press Ctrl+C to stop.")

	// Wait for signal
	sig := <-sigChan
	logger.Info("Received signal, shutting down", "signal", sig.String())

	// Graceful shutdown
	if err := tel.Stop(); err != nil {
		logger.Error("Error during shutdown", "error", err)
	}

	logger.Info("Telephone shut down successfully")
}
