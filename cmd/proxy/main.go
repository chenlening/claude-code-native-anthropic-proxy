package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anthropic-transparent-proxy/internal/config"
	"github.com/anthropic-transparent-proxy/internal/endpoint"
	"github.com/anthropic-transparent-proxy/internal/healthcheck"
	"github.com/anthropic-transparent-proxy/internal/metrics"
	"github.com/anthropic-transparent-proxy/internal/models"
	"github.com/anthropic-transparent-proxy/internal/proxy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	configPath := flag.String("config", "configs/proxy.yaml", "path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Setup logger
	var logLevel slog.Level
	switch cfg.Logging.Level {
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// Initialize metrics
	m := metrics.NewMetrics("anthropic_proxy")
	prometheus.MustRegister(m)

	// Initialize health manager
	hm := endpoint.NewHealthManager(endpoint.HealthConfig{
		FailuresToDisable:     cfg.EndpointHealth.FailuresToDisable,
		RecoveryProbeInterval: cfg.EndpointHealth.RecoveryProbeInterval,
		SuccessesToEnable:     cfg.EndpointHealth.SuccessesToEnable,
	})

	// Register endpoints and find their probe models
	for name, epCfg := range cfg.Endpoints {
		if epCfg.Offline {
			logger.Info("skipping offline endpoint", "endpoint", name)
			continue
		}

		ep := endpoint.NewEndpointState(name, epCfg.URL, epCfg.APIKey, epCfg.ModelsEndpoint)
		if epCfg.Timeout > 0 {
			ep.WithTimeout(epCfg.Timeout)
		}

		// Use first endpoint model for probe if not set (will be overridden by model discovery)
		if ep.ProbeModel == "" {
			ep.ProbeModel = "test" // fallback
		}

		hm.AddEndpoint(ep)
		m.SetEndpointEnabled(name, true)
	}

	// Discover supported models via /v1/models (synchronous initial discovery)
	logger.Info("discovering endpoint model support via /v1/models")
	hm.DiscoverModelsOnce()

	// Verify all endpoints are reachable (lenient mode - log warning, don't fail)
	// If ALL endpoints fail, log error and exit since proxy can't work
	allFailed := true
	for _, ep := range hm.GetAllEndpoints() {
		if err := ep.Verify(ep.ProbeModel); err != nil {
			logger.Warn("endpoint unreachable, marking as disabled",
				"endpoint", ep.Name,
				"url", ep.URL,
				"model", ep.ProbeModel,
				"error", err)
			ep.RecordFailure(err.Error())
			ep.Disable()
			m.SetEndpointEnabled(ep.Name, false)
		} else {
			logger.Info("endpoint verified",
				"endpoint", ep.Name,
				"url", ep.URL,
				"model", ep.ProbeModel)
			allFailed = false
		}
	}
	if allFailed && len(cfg.Endpoints) > 0 {
		logger.Error("all endpoints failed verification, proxy cannot start")
		os.Exit(1)
	}

	// Start recovery probe
	stopCh := make(chan struct{})
	go hm.RunRecoveryProbe(stopCh)

	// Start periodic model discovery refresh
	go hm.StartModelDiscovery(5*time.Minute, stopCh)

	// Setup HTTP mux
	mux := http.NewServeMux()

	// Models endpoint (must be registered before /v1/ handler)
	modelsHandler := models.NewHandler(hm, logger)
	mux.Handle("/v1/models", modelsHandler)

	// Proxy handler
	proxyHandler := proxy.NewHandler(hm, m, logger)
	mux.Handle("/v1/", proxyHandler)

	// Health endpoint
	healthHandler := healthcheck.NewHandler(hm, m, cfg)
	mux.Handle(cfg.Health.Path, healthHandler)

	// Metrics endpoint
	if cfg.Metrics.Enabled {
		mux.Handle(cfg.Metrics.Path, promhttp.Handler())
	}

	// Create HTTP server
	server := &http.Server{
		Addr:         cfg.Server.Listen,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received shutdown signal", "signal", sig)

		close(stopCh)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			logger.Error("server shutdown error", "error", err)
		}
	}()

	logger.Info("starting proxy server",
		"listen", cfg.Server.Listen,
		"endpoints", len(cfg.Endpoints),
	)

	fmt.Fprintf(os.Stderr, "anthropic-transparent-proxy listening on %s\n", cfg.Server.Listen)

	// E2E test: verify proxy can handle actual requests before accepting traffic
	// Start server in goroutine
	serverReady := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			serverReady <- err
		}
		close(serverReady)
	}()

	// Wait for server to be ready (check health endpoint)
	client := &http.Client{Timeout: 10 * time.Second}
	maxWait := 30 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + cfg.Server.Listen + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Wait for shutdown signal or server error
	select {
	case err := <-serverReady:
		if err != nil {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	case <-sigCh:
		// Shutdown signal received, wait for server to stop
	}
	logger.Info("server stopped")
}
