package usage

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// EPConfig holds the base URL and API key for an endpoint.
type EPConfig struct {
	BaseURL string
	APIKey  string
}

// Fetcher periodically fetches usage data from all providers and caches results.
type Fetcher struct {
	providers map[string]Provider
	configs   map[string]EPConfig
	cache     map[string]*UsageData
	mu        sync.RWMutex
	done      chan struct{}
}

// NewFetcher creates a Fetcher that will fetch usage for the given endpoints.
func NewFetcher(providers map[string]Provider, configs map[string]EPConfig) *Fetcher {
	return &Fetcher{
		providers: providers,
		configs:   configs,
		cache:     make(map[string]*UsageData),
		done:      make(chan struct{}),
	}
}

// GetAll returns cached usage data for all endpoints.
func (f *Fetcher) GetAll() map[string]*UsageData {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make(map[string]*UsageData, len(f.cache))
	for k, v := range f.cache {
		result[k] = v
	}
	return result
}

// Run starts the background fetch loop. Blocks until Stop is called.
func (f *Fetcher) Run(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	f.fetchAll(context.Background())

	for {
		select {
		case <-ticker.C:
			f.fetchAll(context.Background())
		case <-f.done:
			return
		}
	}
}

// Stop signals the background fetch loop to stop.
func (f *Fetcher) Stop() {
	close(f.done)
}

func (f *Fetcher) fetchAll(ctx context.Context) {
	for name, provider := range f.providers {
		cfg, ok := f.configs[name]
		if !ok {
			continue
		}

		data, err := provider.FetchUsage(ctx, cfg.BaseURL, cfg.APIKey)
		if err != nil {
			slog.Warn("usage fetch failed", "endpoint", name, "provider", provider.Name(), "error", err)
			if data == nil {
				data = &UsageData{Provider: provider.Name(), Error: err.Error(), FetchedAt: time.Now()}
			}
		} else {
			slog.Debug("usage fetched", "endpoint", name, "provider", provider.Name(), "summary", data.Summary)
		}

		f.mu.Lock()
		f.cache[name] = data
		f.mu.Unlock()
	}
}

// detectProvider returns a Provider for the given endpoint URL, or nil if unsupported.
func detectProvider(endpointURL, volcesAK, volcesSK string) Provider {
	switch {
	case strings.Contains(endpointURL, "open.bigmodel.cn"):
		return &ZhipuProvider{}
	case strings.Contains(endpointURL, "api.deepseek.com"):
		return &DeepSeekProvider{}
	case strings.Contains(endpointURL, "volces.com"):
		return &VolcesProvider{
			AK:     volcesAK,
			SK:     volcesSK,
			Region: "cn-beijing",
		}
	default:
		return nil
	}
}

// EPInput holds endpoint configuration for provider auto-detection.
type EPInput struct {
	URL      string
	APIKey   string
	VolcesAK string
	VolcesSK string
}

// NewFetcherFromConfig creates a Fetcher by auto-detecting providers from endpoint URLs.
func NewFetcherFromConfig(endpoints map[string]EPInput) *Fetcher {
	providers := make(map[string]Provider)
	configs := make(map[string]EPConfig)

	for name, ep := range endpoints {
		p := detectProvider(ep.URL, ep.VolcesAK, ep.VolcesSK)
		if p != nil {
			providers[name] = p
			configs[name] = EPConfig{BaseURL: ep.URL, APIKey: ep.APIKey}
		}
	}

	return NewFetcher(providers, configs)
}
