package usage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"time"
)

// VolcesProvider fetches usage from Volces Ark Coding Plan API using AK/SK signing.
// AK/SK are passed from config (volces_ak/volces_sk) or read from VOLCES_AK/VOLCES_SK env vars.
type VolcesProvider struct {
	AK     string
	SK     string
	Region string
}

func (p *VolcesProvider) Name() string { return "volces" }

func (p *VolcesProvider) FetchUsage(ctx context.Context, baseURL, apiKey string) (*UsageData, error) {
	if p.AK == "" {
		p.AK = os.Getenv("VOLCES_AK")
	}
	if p.SK == "" {
		p.SK = os.Getenv("VOLCES_SK")
	}
	if p.Region == "" {
		p.Region = "cn-beijing"
	}

	if p.AK == "" || p.SK == "" {
		err := fmt.Errorf("volces: AK/SK not set (set volces_ak/volces_sk in proxy.yaml or VOLCES_AK/VOLCES_SK env vars)")
		return &UsageData{Provider: "volces", Error: err.Error(), FetchedAt: time.Now()}, err
	}

	usage, err := p.getCodingPlanUsage(ctx)
	if err != nil {
		return &UsageData{Provider: "volces", Error: err.Error(), FetchedAt: time.Now()}, err
	}

	now := time.Now()
	data := &UsageData{
		Provider:  "volces",
		FetchedAt: now,
		Summary:   usage.Summary,
	}

	for _, qu := range usage.QuotaUsage {
		detail := LimitDetail{
			Type:       "TOKENS_LIMIT",
			Window:     qu.Level,
			Percentage: qu.Percent,
		}
		if qu.ResetTimestamp > 0 {
			detail.ResetTime = time.Unix(qu.ResetTimestamp, 0)
		}
		data.Details = append(data.Details, detail)
	}

	if usage.Summary == "" && len(usage.QuotaUsage) > 0 {
		// Use the highest percentage as summary
		var maxPct float64
		for _, qu := range usage.QuotaUsage {
			if qu.Percent > maxPct {
				maxPct = qu.Percent
			}
		}
		if maxPct > 0 {
			data.Summary = fmt.Sprintf("%.2f%%", maxPct)
		} else {
			data.Summary = "OK"
		}
	}

	return data, nil
}

type codingPlanUsage struct {
	Summary    string
	QuotaUsage []quotaUsageEntry
}

type quotaUsageEntry struct {
	Level          string  `json:"Level"`
	Percent        float64 `json:"Percent"`
	ResetTimestamp int64   `json:"ResetTimestamp"`
}

type codingPlanUsageResp struct {
	Result *struct {
		Status          string            `json:"Status"`
		UpdateTimestamp int64             `json:"UpdateTimestamp"`
		QuotaUsage      []quotaUsageEntry `json:"QuotaUsage"`
	} `json:"Result"`
}

func (p *VolcesProvider) getCodingPlanUsage(ctx context.Context) (*codingPlanUsage, error) {
	params := map[string]string{
		"Action":  "GetCodingPlanUsage",
		"Version": "2024-01-01",
	}
	respBody, err := p.callAPI(ctx, "GET", params, nil)
	if err != nil {
		return nil, fmt.Errorf("GetCodingPlanUsage: %w", err)
	}

	var resp codingPlanUsageResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode GetCodingPlanUsage: %w, body=%s", err, string(respBody[:min(len(respBody), 200)]))
	}

	if resp.Result == nil {
		return nil, fmt.Errorf("GetCodingPlanUsage: empty result")
	}

	cu := &codingPlanUsage{
		QuotaUsage: resp.Result.QuotaUsage,
	}

	// Compute summary from highest percentage
	var maxPct float64
	for _, qu := range resp.Result.QuotaUsage {
		if qu.Percent > maxPct {
			maxPct = qu.Percent
		}
	}
	if maxPct > 0 {
		cu.Summary = fmt.Sprintf("%.2f%%", maxPct)
	} else {
		cu.Summary = "OK"
	}

	return cu, nil
}

func (p *VolcesProvider) callAPI(ctx context.Context, method string, params map[string]string, jsonBody []byte) ([]byte, error) {
	region := p.Region
	if region == "" {
		region = "cn-beijing"
	}

	host := fmt.Sprintf("ark.%s.volcengineapi.com", region)

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	q := url.Values{}
	for _, k := range keys {
		q.Set(k, params[k])
	}
	query := q.Encode()

	apiURL := fmt.Sprintf("https://%s/?%s", host, query)

	req, err := http.NewRequestWithContext(ctx, method, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	p.signRequest(req, region, "ark", query, jsonBody)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var envelope struct {
		ResponseMetadata struct {
			Error *struct {
				CodeN   int    `json:"CodeN"`
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
		Result json.RawMessage `json:"Result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}

	if envelope.ResponseMetadata.Error != nil {
		return nil, fmt.Errorf("%s (%s)", envelope.ResponseMetadata.Error.Message, envelope.ResponseMetadata.Error.Code)
	}

	reWrapped, _ := json.Marshal(map[string]json.RawMessage{
		"Result": envelope.Result,
	})
	return reWrapped, nil
}

func (p *VolcesProvider) signRequest(req *http.Request, region, service, query string, payload []byte) {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	if payload == nil {
		payload = []byte{}
	}

	payloadHash := sha256Hex(payload)

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Date", amzDate)
	req.Header.Set("X-Content-Sha256", payloadHash)
	req.Header.Set("Content-Type", "application/json")

	canonicalURI := "/"
	signedHeaders := "host;x-content-sha256;x-date"
	canonicalHeaders := fmt.Sprintf("host:%s\nx-content-sha256:%s\nx-date:%s\n",
		req.URL.Host, payloadHash, amzDate)

	canonicalReq := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		query,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	)
	canonicalReqHash := sha256Hex([]byte(canonicalReq))

	credentialScope := fmt.Sprintf("%s/%s/%s/request", dateStamp, region, service)

	stringToSign := fmt.Sprintf("HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		credentialScope,
		canonicalReqHash,
	)

	kDate := hmacSHA256([]byte(p.SK), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "request")

	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	auth := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.AK, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
