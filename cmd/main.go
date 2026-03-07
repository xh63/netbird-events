package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xh63/netbird-events/pkg/config"
	"github.com/xh63/netbird-events/pkg/election"
	"github.com/xh63/netbird-events/pkg/metrics"
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

	// Create logger factory; derive the local system logger for main's own messages.
	// Components that need additional log types call logFactory.New("<type>") directly.
	logFactory := cfg.NewLogFactory()
	logger := logFactory.New("system")
	logger.Info("Starting eventsproc", "version", version, "config_file", *configFile)

	// Start Prometheus metrics endpoint
	http.Handle("/metrics", promhttp.HandlerFor(metrics.MyRegistry, promhttp.HandlerOpts{}))
	go func() {
		logger.Info("Starting Prometheus metrics server", "port", cfg.MetricsPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.MetricsPort), nil); err != nil {
			logger.Error("Failed to start Prometheus metrics server", "error", err)
		}
	}()

	// Create processor
	proc, err := processor.NewProcessor(cfg, logFactory)
	if err != nil {
		logger.Error("Failed to create processor", "error", err)
		os.Exit(1)
	}
	defer func() { _ = proc.Close() }()

	// Setup context with cancellation for the full application lifetime.
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// Cancel appCtx on SIGTERM or SIGINT — this is the single shutdown trigger.
	// Both the elector (if enabled) and the processor observe appCtx.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Info("Received signal, shutting down gracefully", "signal", sig)
		appCancel()
	}()

	// Run the processor — directly (standalone) or via the Redis elector (HA mode).
	// errChan is buffered so the goroutine never blocks on write.
	errChan := make(chan error, 1)

	if cfg.Cluster.Enabled {
		hostname, _ := os.Hostname()
		lockKey := "eventsproc:leader"
		clusterLogger := logFactory.New("cluster")
		el, elErr := election.New(&election.ElectorConfig{
			RedisURL:      cfg.Cluster.RedisURL,
			LockKey:       lockKey,
			TTL:           time.Duration(cfg.Cluster.LockTTL) * time.Second,
			RetryInterval: time.Duration(cfg.Cluster.LockRetryInterval) * time.Second,
			NodeID:        hostname,
		}, clusterLogger)
		if elErr != nil {
			logger.Error("Failed to create leader elector", "error", elErr)
			os.Exit(1)
		}
		logger.Info("Cluster mode enabled",
			"node", hostname,
			"lock_key", lockKey,
			"lock_ttl_seconds", cfg.Cluster.LockTTL,
			"redis_url", cfg.Cluster.RedisURL,
		)
		go func() {
			errChan <- el.Run(appCtx, func(ctx context.Context) error {
				metrics.IsLeader.Set(1)
				defer metrics.IsLeader.Set(0)
				return proc.Run(ctx)
			})
		}()
	} else {
		logger.Info("Standalone mode (cluster disabled)")
		go func() {
			errChan <- proc.Run(appCtx)
		}()
	}

	// Block until the processor (or elector) exits.
	if err := <-errChan; err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Fatal error", "error", err)
		os.Exit(1)
	}
	logger.Info("Shutdown complete")
}
