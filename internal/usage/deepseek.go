package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DeepSeekProvider fetches balance from api.deepseek.com.
type DeepSeekProvider struct{}

func (p *DeepSeekProvider) Name() string { return "deepseek" }

func (p *DeepSeekProvider) FetchUsage(ctx context.Context, baseURL, apiKey string) (*UsageData, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("deepseek: parse base URL: %w", err)
	}
	apiURL := fmt.Sprintf("%s://%s/user/balance", u.Scheme, u.Host)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("deepseek: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &UsageData{Provider: "deepseek", Error: err.Error(), FetchedAt: time.Now()}, err
	}
	defer resp.Body.Close()

	var body struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency        string `json:"currency"`
			TotalBalance    string `json:"total_balance"`
			GrantedBalance  string `json:"granted_balance"`
			ToppedUpBalance string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return &UsageData{Provider: "deepseek", Error: fmt.Sprintf("decode error: %v", err), FetchedAt: time.Now()}, err
	}

	now := time.Now()
	data := &UsageData{
		Provider:  "deepseek",
		FetchedAt: now,
	}

	available := body.IsAvailable

	if !body.IsAvailable && len(body.BalanceInfos) == 0 {
		data.Summary = "unavailable"
		data.Details = append(data.Details, LimitDetail{
			Type:        "BALANCE",
			IsAvailable: &available,
		})
	}

	for _, bi := range body.BalanceInfos {
		detail := LimitDetail{
			Type:        "BALANCE",
			Balance:     bi.TotalBalance,
			Currency:    bi.Currency,
			IsAvailable: &available,
		}
		data.Details = append(data.Details, detail)

		if data.Summary == "" || bi.Currency == "CNY" {
			data.Summary = fmt.Sprintf("¥%s", bi.TotalBalance)
		}
	}

	if !body.IsAvailable && data.Summary == "" {
		data.Summary = "unavailable"
	}

	return data, nil
}
