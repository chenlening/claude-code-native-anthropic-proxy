package usage

import (
	"context"
	"time"
)

// Provider fetches usage/quota data from a backend API provider.
type Provider interface {
	Name() string
	FetchUsage(ctx context.Context, baseURL, apiKey string) (*UsageData, error)
}

// UsageData holds usage/quota information for a single endpoint.
type UsageData struct {
	Provider  string        `json:"provider"`
	Summary   string        `json:"summary"`
	PlanLevel string        `json:"plan_level,omitempty"`
	Details   []LimitDetail `json:"details,omitempty"`
	Error     string        `json:"error,omitempty"`
	FetchedAt time.Time     `json:"fetched_at"`
}

// LimitDetail describes a single usage limit or quota entry.
type LimitDetail struct {
	Type       string       `json:"type"`
	Window     string       `json:"window,omitempty"`
	Percentage float64      `json:"percentage,omitempty"`
	Used       int64        `json:"used,omitempty"`
	Total      int64        `json:"total,omitempty"`
	ResetTime  time.Time    `json:"reset_time,omitempty"`
	PerModel   []ModelUsage `json:"per_model,omitempty"`
	// DeepSeek-specific
	Balance     string `json:"balance,omitempty"`
	Currency    string `json:"currency,omitempty"`
	IsAvailable *bool  `json:"is_available,omitempty"`
}

// ModelUsage holds per-model usage breakdown.
type ModelUsage struct {
	Model string `json:"model"`
	Usage int64  `json:"usage"`
}

// IsStale returns true if the data is older than the given duration.
func (u *UsageData) IsStale(maxAge time.Duration) bool {
	return time.Since(u.FetchedAt) > maxAge
}
