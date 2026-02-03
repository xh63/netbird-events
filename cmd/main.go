package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/xh63/netbird-events/pkg/config"
	"github.com/xh63/netbird-events/pkg/processor"
)

var version = "dev"

func main() {
	// Parse command line flags
	configFile := flag.String("config", "/etc/app/eventsproc/config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	// Show version if requested
	if *showVersion {
		fmt.Printf("eventsproc version %s\n", version)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Create logger
	logger := cfg.GetLogger()
	logger.Info("Starting eventsproc", "version", version, "config_file", *configFile)

	// Create processor
	proc, err := processor.NewProcessor(cfg, logger)
	if err != nil {
		logger.Error("Failed to create processor", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := proc.Close(); err != nil {
			logger.Error("Failed to close processor", "error", err)
		}
	}()

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run processor in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- proc.Run(ctx)
	}()

	// Wait for completion or signal
	select {
	case err := <-errChan:
		if err != nil {
			logger.Error("Processor error", "error", err)
			os.Exit(1)
		}
		logger.Info("Processor completed successfully")
	case sig := <-sigChan:
		logger.Info("Received signal, shutting down gracefully", "signal", sig)
		cancel()
		// Wait for processor to finish current batch
		if err := <-errChan; err != nil && err != context.Canceled {
			logger.Error("Error during shutdown", "error", err)
			os.Exit(1)
		}
		logger.Info("Shutdown complete")
	}
}
