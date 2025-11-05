package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/verastack/telephone/pkg/config"
	"github.com/verastack/telephone/pkg/proxy"
)

var (
	envFile = flag.String("env", ".env", "Path to .env file")
	version = "dev"
)

func main() {
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Telephone v%s starting...", version)

	// Load .env file if it exists
	if _, err := os.Stat(*envFile); err == nil {
		log.Printf("Loading environment from %s", *envFile)
		if err := godotenv.Load(*envFile); err != nil {
			log.Fatalf("Error loading .env file: %v", err)
		}
	}

	// Load configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	log.Printf("Configuration loaded:")
	log.Printf("  Plugboard URL: %s", cfg.PlugboardURL)
	log.Printf("  Backend: %s", cfg.BackendURL())

	// Create Telephone instance
	tel, err := proxy.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create Telephone: %v", err)
	}

	// Start the client
	if err := tel.Start(); err != nil {
		log.Fatalf("Failed to start Telephone: %v", err)
	}

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Telephone is running. Press Ctrl+C to stop.")

	// Wait for signal
	sig := <-sigChan
	log.Printf("Received signal: %v", sig)

	// Graceful shutdown
	if err := tel.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Telephone shut down successfully")
}
