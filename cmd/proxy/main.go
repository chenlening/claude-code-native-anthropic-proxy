package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/anthropic-transparent-proxy/internal/proxy"
	"github.com/anthropic-transparent-proxy/internal/routing"
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
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
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
		ep := endpoint.NewEndpointState(name, epCfg.URL, epCfg.APIKey)
		if epCfg.Timeout > 0 {
			ep.WithTimeout(epCfg.Timeout)
		}

		// Find the first backend model that uses this endpoint
		for _, modelCfg := range cfg.Models {
			for _, backend := range modelCfg.Backends {
				if backend.Endpoint == ep.Name && ep.ProbeModel == "" {
					ep.ProbeModel = backend.Model
				}
			}
		}
		if ep.ProbeModel == "" {
			ep.ProbeModel = "test" // fallback
		}

		hm.AddEndpoint(ep)
		m.SetEndpointEnabled(name, true)
	}

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

	// Initialize router
	lb := routing.NewLeastConnectionsLoadBalancer()
	router := routing.NewModelRouter(cfg, hm, lb)

	// Start recovery probe
	stopCh := make(chan struct{})
	go hm.RunRecoveryProbe(stopCh)

	// Setup HTTP mux
	mux := http.NewServeMux()

	// Proxy handler
	proxyHandler := proxy.NewHandler(router, hm, m, logger)
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
		"models", len(cfg.Models),
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

	// Send E2E test request to verify proxy works end-to-end
	testRequest := map[string]interface{}{
		"model": "claude-haiku-3-5-20241022",
		"max_tokens": 5,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	testBody, _ := json.Marshal(testRequest)
	req, err := http.NewRequest("POST", "http://"+cfg.Server.Listen+"/v1/messages", bytes.NewReader(testBody))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", "test")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		resp, err := client.Do(req)
		if err != nil {
			logger.Error("E2E test failed: proxy request error", "error", err)
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			logger.Error("E2E test failed: proxy returned non-200 status", "status", resp.StatusCode)
			os.Exit(1)
		}
		logger.Info("E2E test passed: proxy verified working")
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
