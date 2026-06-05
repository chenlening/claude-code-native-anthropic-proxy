package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ZhipuProvider fetches usage from open.bigmodel.cn.
type ZhipuProvider struct{}

func (p *ZhipuProvider) Name() string { return "zhipu" }

func (p *ZhipuProvider) FetchUsage(ctx context.Context, baseURL, apiKey string) (*UsageData, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("zhipu: parse base URL: %w", err)
	}
	apiURL := fmt.Sprintf("%s://%s/api/monitor/usage/quota/limit", u.Scheme, u.Host)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("zhipu: build request: %w", err)
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Accept-Language", "en-US,en")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &UsageData{Provider: "zhipu", Error: err.Error(), FetchedAt: time.Now()}, err
	}
	defer resp.Body.Close()

	var body struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Success bool   `json:"success"`
		Data    struct {
			Limits []struct {
				Type          string  `json:"type"`
				Unit          int     `json:"unit"`
				Number        int     `json:"number"`
				Percentage    float64 `json:"percentage"`
				Usage         int64   `json:"usage"`
				CurrentValue  int64   `json:"currentValue"`
				Remaining     int64   `json:"remaining"`
				NextResetTime int64   `json:"nextResetTime"`
				UsageDetails  []struct {
					ModelCode string `json:"modelCode"`
					Usage     int64  `json:"usage"`
				} `json:"usageDetails"`
			} `json:"limits"`
			Level string `json:"level"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return &UsageData{Provider: "zhipu", Error: fmt.Sprintf("decode error: %v", err), FetchedAt: time.Now()}, err
	}

	if !body.Success {
		err := fmt.Errorf("zhipu: %s (code %d)", body.Msg, body.Code)
		return &UsageData{Provider: "zhipu", Error: err.Error(), FetchedAt: time.Now()}, err
	}

	now := time.Now()
	data := &UsageData{
		Provider:  "zhipu",
		PlanLevel: body.Data.Level,
		FetchedAt: now,
	}

	// unit meanings: 3=hours, 5=monthly(time), 6=weekly(tokens)
	unitToWindow := map[int]string{3: "5h", 5: "monthly", 6: "weekly"}

	var monthlyPct float64
	for _, lim := range body.Data.Limits {
		window := unitToWindow[lim.Unit]
		if window == "" {
			window = fmt.Sprintf("unit%d", lim.Unit)
		}
		detail := LimitDetail{
			Type:       lim.Type,
			Window:     window,
			Percentage: lim.Percentage,
			Used:       lim.CurrentValue,
			Total:      lim.Usage,
			ResetTime:  time.UnixMilli(lim.NextResetTime),
		}
		for _, md := range lim.UsageDetails {
			detail.PerModel = append(detail.PerModel, ModelUsage{Model: md.ModelCode, Usage: md.Usage})
		}
		data.Details = append(data.Details, detail)

		if lim.Type == "TOKENS_LIMIT" && lim.Unit == 6 {
			monthlyPct = lim.Percentage
		}
	}

	if monthlyPct > 0 {
		data.Summary = fmt.Sprintf("%.0f%%", monthlyPct)
	} else if len(data.Details) > 0 {
		data.Summary = fmt.Sprintf("%.0f%%", data.Details[0].Percentage)
	}

	return data, nil
}
